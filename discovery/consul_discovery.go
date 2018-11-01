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

// holds consul client instance and configuration
type consulDiscoverySource struct {
	client *api.Client

	startRetryDelay int64
	maxRetryDelay   int64
	protocol        string

	configOptions   config.Options         // passed when calling new...()
	options         *registerConfiguration // loaded as config bundle
	serviceInstance *consulServiceInstance

	logger *logm.Logm
}

// holds service instance configuration and state
type consulServiceInstance struct {
	isRegistered bool

	id         string
	name       string
	versionTag string

	singleton bool
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
	d.options = &regconf

	d.serviceInstance = &consulServiceInstance{
		singleton: options.Singleton,
	}

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(err.Error())
	}

	d.serviceInstance.id = d.options.Name + "-" + uuid4.String()
	d.serviceInstance.name = d.options.Env.Name + "-" + d.options.Name
	d.serviceInstance.versionTag = "version=" + d.options.Version

	go d.run(d.startRetryDelay)

	return d.serviceInstance.id, nil
}

func (d consulDiscoverySource) DiscoverService(options DiscoverOptions) (string, error) {

	// TODO: ACCESSTYPE?
	queryServiceName := options.Environment + "-" + options.Value
	serviceEntries, _, err := d.client.Health().Service(queryServiceName, "", true, nil)
	if err != nil {
		d.logger.Error("Service discovery failed: %s", err.Error())
		return "", fmt.Errorf("Service discovery failed: %s", err.Error())
	}

	d.logger.Verbose("Services %s-%s available: %d", options.Environment, options.Value, len(serviceEntries))

	versionRange, err := parseVersion(options.Version)
	if err != nil {
		return "", fmt.Errorf("wantVersion parse error: %s", err.Error())
	}

	return d.extractService(serviceEntries, versionRange)
}

// functions that aren't discoverySource methods

// if service is not registered, performs registration. Otherwise perform ttl update
func (d consulDiscoverySource) run(retryDelay int64) {

	var ok bool
	if !d.serviceInstance.isRegistered {
		ok = d.register(retryDelay)
		if ok {
			d.serviceInstance.isRegistered = true
		}
	} else {
		ok = d.ttlUpdate(retryDelay)
		if !ok {
			d.serviceInstance.isRegistered = false
		}
	}

	if !ok {
		// Something went wrong with either registration or TTL update :(

		// sleep for current delay
		time.Sleep(time.Duration(retryDelay) * time.Millisecond)
		// exponentially extend retry delay, but keep it at most maxRetryDelay
		newRetryDelay := retryDelay * 2
		if newRetryDelay > d.maxRetryDelay {
			newRetryDelay = d.maxRetryDelay
		}
		d.run(newRetryDelay)
	} else {
		// Everything is alright, either registration or TTL update was successful :)

		time.Sleep(time.Duration(d.options.Discovery.PingInterval) * time.Second)
		d.run(d.startRetryDelay)
	}

}

func (d consulDiscoverySource) register(retryDelay int64) bool {
	inst := d.serviceInstance

	if d.isServiceRegistered() && inst.singleton {
		d.logger.Error("Service of this kind is already registered, not registering with options.singleton set to true")
		return false
	}

	d.logger.Info("Registering service: id=%s address=%s port=%d", inst.id, d.options.Server.HTTP.Address, d.options.Server.HTTP.Port)

	agentRegistration := api.AgentServiceRegistration{
		Port: d.options.Server.HTTP.Port,
		ID:   inst.id,
		Name: inst.name,
		Tags: []string{d.protocol, inst.versionTag},
		Check: &api.AgentServiceCheck{
			CheckID: "check-" + inst.id,
			TTL:     strconv.FormatInt(d.options.Discovery.TTL, 10) + "s",
			DeregisterCriticalServiceAfter: strconv.FormatInt(10, 10) + "s",
		},
	}

	if d.options.Server.HTTP.Address != "" {
		agentRegistration.Address = d.options.Server.HTTP.Address
	}

	err := d.client.Agent().ServiceRegister(&agentRegistration)

	if err != nil {
		d.logger.Error(fmt.Sprintf("Service registration failed: %s", err.Error()))
		return false
	}

	d.logger.Info("Service registered, id=%s", inst.id)
	return true
}

func (d consulDiscoverySource) ttlUpdate(retryDelay int64) bool {
	inst := d.serviceInstance
	//d.logger.Verbose("Updating TTL for service %s", inst.id)

	err := d.client.Agent().UpdateTTL(
		"check-"+inst.id,
		"serviceid="+inst.id+" time="+time.Now().Format("2006-01-02 15:04:05"),
		"passing")

	if err != nil {
		d.logger.Error("TTL update failed, error: %s, retry delay: %d ms", inst.id, err.Error(), retryDelay)
		return false
	}

	d.logger.Verbose("TTL update for service %s", inst.id)
	return true
}

// returns true if there are any services of this kind (env+name) registered
func (d consulDiscoverySource) isServiceRegistered() bool {
	reg := d.serviceInstance
	serviceEntries, _, err := d.client.Health().Service(reg.id, "", true, nil)

	if err != nil {
		d.logger.Warning("isServiceRegistered() failed: %s", err.Error())
		return false
	}

	return len(serviceEntries) > 0
}

func (d consulDiscoverySource) extractService(serviceEntries []*api.ServiceEntry, wantVersion semver.Range) (string, error) {
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

		// TODO check if ok
		return fmt.Sprintf("%s:%d", addr, port), nil
	}

	return "", fmt.Errorf("Service discovery failed: No services for given query")
}

// functions that aren't discoverySource methods or consulDiscoverySource methods

func createConsulClient(address string) (*api.Client, error) {
	clientConfig := api.DefaultConfig()
	clientConfig.Address = address

	client, err := api.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
}
