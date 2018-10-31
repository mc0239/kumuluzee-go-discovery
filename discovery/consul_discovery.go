package discovery

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/hashicorp/consul/api"
	"github.com/mc0239/kumuluzee-go-config/config"
	"github.com/mc0239/logm"
	"github.com/satori/go.uuid"
)

type consulDiscoverySource struct {
	client *api.Client

	startRetryDelay int64
	maxRetryDelay   int64
	protocol        string

	configOptions config.Options

	logger *logm.Logm

	registerableService *registerableService
}

type registerableService struct {
	isRegistered bool

	id         string
	name       string
	versionTag string

	singleton bool

	options *registerConfiguration
}

type registerConfiguration struct {
	Name   string
	Server struct {
		HTTP struct {
			Port    int
			Address string
		} `config:"http"`
	}
	Env struct {
		Name string
	}
	Version   string
	Discovery struct {
		TTL          int64 `config:"ttl"`
		PingInterval int64 `config:"ping-interval"`
	}
}

func newConsulDiscoverySource(options config.Options, logger *logm.Logm) discoverySource {
	var d consulDiscoverySource
	logger.Verbose("Initializing Consul discovery source")
	d.logger = logger

	d.configOptions = options
	conf := config.NewUtil(options)

	startRD, maxRD := getRetryDelays(conf)
	d.startRetryDelay = startRD
	d.maxRetryDelay = maxRD
	logger.Verbose("start-retry-delay-ms=%d, max-retry-delay-ms=%d", d.startRetryDelay, d.maxRetryDelay)

	var consulAddress string
	if addr, ok := conf.GetString("kumuluzee.discovery.consul.hosts"); ok {
		consulAddress = addr
	}
	if client, err := createConsulClient(consulAddress); err == nil {
		logger.Info("Consul client address set to %v", consulAddress)
		d.client = client
	} else {
		logger.Error("Failed to create Consul client: %s", err.Error())
	}

	if p, ok := conf.GetString("kumuluzee.discovery.consul.protocol"); ok {
		d.protocol = p
	} else {
		d.protocol = "http"
	}

	return d
}

func (d consulDiscoverySource) RegisterService(options RegisterOptions) (serviceID string, err error) {
	regconf := loadServiceRegisterConfiguration(d.configOptions, options)
	regService := registerableService{
		options:   &regconf,
		singleton: options.Singleton,
	}

	d.registerableService = &regService

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(err.Error())
	}

	regService.id = regService.options.Name + "-" + uuid4.String()
	regService.name = regService.options.Env.Name + "-" + regService.options.Name
	regService.versionTag = "version=" + regService.options.Version

	d.register(d.startRetryDelay)
	go d.ttlUpdate(d.startRetryDelay)

	return regService.id, nil
}

func (d consulDiscoverySource) DiscoverService(options DiscoverOptions) (Service, error) {

	// TODO: ACCESSTYPE?
	queryServiceName := options.Environment + "-" + options.Value
	serviceEntries, _, err := d.client.Health().Service(queryServiceName, "", true, nil)
	if err != nil {
		d.logger.Error("Service discovery failed: %s", err.Error())
		return Service{}, fmt.Errorf("Service discovery failed: %s", err.Error())
	}

	d.logger.Verbose("Services %s-%s available: %d", options.Environment, options.Value, len(serviceEntries))

	versionRange, err := parseVersion(options.Version)
	if err != nil {
		return Service{}, fmt.Errorf("wantVersion parse error: %s", err.Error())
	}

	return d.extractService(serviceEntries, versionRange)
}

// functions that aren't configSource methods

func (d consulDiscoverySource) isServiceRegistered() bool {
	reg := d.registerableService
	serviceEntries, _, err := d.client.Health().Service(reg.id, reg.versionTag, true, nil)

	if err != nil {
		d.logger.Error(err.Error())
		return false
	}

	for _, service := range serviceEntries {
		for _, tag := range service.Service.Tags {
			if tag == reg.versionTag {
				return true
			}
		}
	}

	return false
}

func (d consulDiscoverySource) register(retryDelay int64) {
	reg := d.registerableService
	if isRegistered := d.isServiceRegistered(); isRegistered && reg.singleton {
		d.logger.Error("Service is already registered, not registering with options.singleton set to true")
	} else {
		d.logger.Info("Registering service: id=%s address=%s port=%d", reg.id, reg.options.Server.HTTP.Address, reg.options.Server.HTTP.Port)

		agentRegistration := api.AgentServiceRegistration{
			Port: reg.options.Server.HTTP.Port,
			ID:   reg.id,
			Name: reg.name,
			Tags: []string{d.protocol, reg.versionTag},
			Check: &api.AgentServiceCheck{
				CheckID: "check-" + reg.id,
				TTL:     strconv.FormatInt(reg.options.Discovery.TTL, 10) + "s",
				DeregisterCriticalServiceAfter: strconv.FormatInt(10, 10) + "s",
			},
		}

		if reg.options.Server.HTTP.Address != "" {
			agentRegistration.Address = reg.options.Server.HTTP.Address
		}

		err := d.client.Agent().ServiceRegister(&agentRegistration)

		if err != nil {
			d.logger.Error(fmt.Sprintf("Service registration failed: %s", err.Error()))
			// sleep for current delay
			time.Sleep(time.Duration(retryDelay) * time.Millisecond)

			// exponentially extend retry delay, but keep it at most maxRetryDelay
			newRetryDelay := retryDelay * 2
			if newRetryDelay > d.maxRetryDelay {
				newRetryDelay = d.maxRetryDelay
			}
			d.register(newRetryDelay)
			return
		}

		d.logger.Info("Service registered, id=%s", reg.id)
		reg.isRegistered = true
	}
}

func (d consulDiscoverySource) ttlUpdate(retryDelay int64) {
	reg := d.registerableService
	d.logger.Verbose("Updating TTL for service %s", reg.id)

	err := d.client.Agent().UpdateTTL("check-"+reg.id, "serviceid="+reg.id+" time="+time.Now().Format("2006-01-02 15:04:05"), "passing")
	if err != nil {
		d.logger.Error("Updating TTL failed for service %s, error: %s, retry delay: %d ms", reg.id, err.Error(), retryDelay)

		// sleep for current delay
		time.Sleep(time.Duration(retryDelay) * time.Millisecond)

		// exponentially extend retry delay, but keep it at most maxRetryDelay
		newRetryDelay := retryDelay * 2
		if newRetryDelay > d.maxRetryDelay {
			newRetryDelay = d.maxRetryDelay
		}
		d.ttlUpdate(newRetryDelay)
		return
	}

	time.Sleep(time.Duration(reg.options.Discovery.PingInterval) * time.Second)
	d.ttlUpdate(d.startRetryDelay)
	return
}

func (d consulDiscoverySource) extractService(serviceEntries []*api.ServiceEntry, wantVersion semver.Range) (Service, error) {
	var foundServiceIndexes []int
	for index, serviceEntry := range serviceEntries {
		for _, tag := range serviceEntry.Service.Tags {
			if strings.HasPrefix(tag, "version") {
				t := strings.Split(tag, "=")

				gotVersion, err := semver.ParseTolerant(t[1])
				if err == nil {
					// check if gotVersion is in wantVersion's range
					if wantVersion(gotVersion) {
						foundServiceIndexes = append(foundServiceIndexes, index)
					}
				} else {
					d.logger.Warning("semver parsing failed for: %s, error: %s", t[1], err.Error())
				}
			}
		}
	}

	if len(foundServiceIndexes) > 0 {
		var addr string
		var port int

		randomIndex := rand.Intn(len(foundServiceIndexes))

		service := serviceEntries[foundServiceIndexes[randomIndex]]

		addr = service.Service.Address
		// if address is not set, it's equal to node's address
		if addr == "" {
			addr = service.Node.Address
		}
		port = service.Service.Port

		d.logger.Verbose("Found service, address=%s port=%d", addr, port)

		return Service{
			Address: addr,
			Port:    strconv.Itoa(port),
		}, nil
	}

	return Service{}, fmt.Errorf("Service discovery failed: No services for given query")
}

// functions that aren't configSource methods or consulCondigSource methods

func createConsulClient(address string) (*api.Client, error) {
	clientConfig := api.DefaultConfig()
	clientConfig.Address = address

	client, err := api.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
}
