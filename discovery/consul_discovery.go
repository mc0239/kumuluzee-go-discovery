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

	configOptions config.Options

	logger               *logm.Logm
	registerableServices []registerableService
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

func initConsulDiscoverySource(options config.Options, logger *logm.Logm) discoverySource {
	logger.Verbose("Initializing ConsulDiscoverySource")
	var d consulDiscoverySource

	d.logger = logger
	d.configOptions = options

	consulClientConfig := api.DefaultConfig()

	conf := config.NewUtil(options)

	if addr, ok := conf.GetString("kumuluzee.config.consul.hosts"); ok {
		consulClientConfig.Address = addr
	}

	startRetryDelay, ok := conf.Get("kumuluzee.config.start-retry-delay-ms").(float64)
	if !ok {
		logger.Warning("Failed to assert value kumuluzee.config.start-retry-delay-ms as float64. Using default value 500.")
		startRetryDelay = 500
	}
	d.startRetryDelay = int64(startRetryDelay)

	maxRetryDelay, ok := conf.Get("kumuluzee.config.max-retry-delay-ms").(float64)
	if !ok {
		logger.Warning("Failed to assert value kumuluzee.config.max-retry-delay-ms as float64. Using default value 900000.")
		maxRetryDelay = 900000
	}
	d.maxRetryDelay = int64(maxRetryDelay)

	client, err := api.NewClient(consulClientConfig)
	if err != nil {
		logger.Error("Failed to create Consul client: %s", err.Error())
	}

	logger.Info("Consul client address set to %s", consulClientConfig.Address)

	d.client = client

	return d
}

func (d consulDiscoverySource) RegisterService(options RegisterOptions) (serviceID string, err error) {

	// Load default values
	regconf := registerConfiguration{}
	regconf.Server.HTTP.Port = 80 // TODO: default port to register?
	regconf.Env.Name = "dev"
	regconf.Version = "1.0.0"
	regconf.Discovery.TTL = 30
	regconf.Discovery.PingInterval = 20

	// Load from configuration file, overriding defaults
	config.NewBundle("kumuluzee", &regconf, d.configOptions)

	// Load from RegisterOptions, override file configuration
	if options.Value != "" {
		regconf.Name = options.Value
	}
	if options.Environment != "" {
		regconf.Env.Name = options.Environment
	}
	if options.Version != "" {
		regconf.Version = options.Version
	}
	if options.TTL != 0 {
		regconf.Discovery.TTL = options.TTL
	}
	if options.PingInterval != 0 {
		regconf.Discovery.PingInterval = options.PingInterval
	}

	//d.logger.Info("after bundling: %v", regconf)

	regService := registerableService{
		options:   &regconf,
		singleton: options.Singleton,
	}

	// TODO: at some point, unregistered services should be removed from array!
	d.registerableServices = append(d.registerableServices, regService)

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(fmt.Sprintf(err.Error()))
	}

	regService.id = regService.options.Name + "-" + uuid4.String()
	regService.name = regService.options.Env.Name + "-" + regService.options.Name
	regService.versionTag = "version=" + regService.options.Version

	d.register(&regService, d.startRetryDelay)
	go d.checkIn(&regService, d.startRetryDelay)

	return regService.id, nil
}

func (d consulDiscoverySource) isServiceRegistered(reg *registerableService) bool {
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

func (d consulDiscoverySource) register(reg *registerableService, retryDelay int64) {
	if isRegistered := d.isServiceRegistered(reg); isRegistered && reg.singleton {
		d.logger.Error("Service is already registered, not registering with options.singleton set to true")
	} else {
		d.logger.Info("Registering service: id=%s address=%s port=%d", reg.id, reg.options.Server.HTTP.Address, reg.options.Server.HTTP.Port)

		agentRegistration := api.AgentServiceRegistration{
			Port: reg.options.Server.HTTP.Port,
			ID:   reg.id,
			Name: reg.name,
			Tags: []string{"<service protocol>", reg.versionTag},
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
			d.register(reg, newRetryDelay)
			return
		}

		d.logger.Info("Service registered, id=%s", reg.id)
		reg.isRegistered = true
	}
}

func (d consulDiscoverySource) checkIn(reg *registerableService, retryDelay int64) {
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
		d.checkIn(reg, newRetryDelay)
		return
	}

	time.Sleep(time.Duration(reg.options.Discovery.PingInterval) * time.Second)
	d.checkIn(reg, d.startRetryDelay)
	return
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

	versionRange, err := d.parseVersion(options.Version)
	if err != nil {
		return Service{}, fmt.Errorf("wantVersion parse error: %s", err.Error())
	}

	return d.extractService(serviceEntries, versionRange)
}

func (d consulDiscoverySource) parseVersion(version string) (semver.Range, error) {
	version = strings.Replace(version, "*", "x", -1)

	if strings.HasPrefix(version, "^") {
		ver, err := semver.ParseTolerant(version[1:])
		if err == nil {
			var verNext = ver
			verNext.Major++
			return semver.ParseRange(">=" + ver.String() + " <" + verNext.String())
		}
		return nil, err
	} else if strings.HasPrefix(version, "~") {
		ver, err := semver.ParseTolerant(version[1:])
		if err == nil {
			var verNext = ver
			verNext.Minor++
			return semver.ParseRange(">=" + ver.String() + " <" + verNext.String())
		}
		return nil, err
	} else {
		return semver.ParseRange(version)
	}
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
			Port:    port,
		}, nil
	} else {
		return Service{}, fmt.Errorf("Service discovery failed: No services for given query")
	}
}
