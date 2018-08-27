package discovery

import (
	"fmt"
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

	logger               *logm.Logm
	registerableServices []registerableService
}

type registerableService struct {
	isRegistered bool

	port    int
	address string

	id         string
	name       string
	versionTag string

	options *RegisterOptions
}

func initConsulDiscoverySource(config config.Util, logger *logm.Logm) discoverySource {
	logger.Verbose("Initializing ConsulDiscoverySource")
	var d consulDiscoverySource
	d.logger = logger

	clientConfig := api.DefaultConfig()

	if addr, ok := config.GetString("kumuluzee.config.consul.hosts"); ok {
		clientConfig.Address = addr
	}

	startRetryDelay, ok := config.Get("kumuluzee.config.start-retry-delay-ms").(float64)
	if !ok {
		logger.Warning("Failed to assert value kumuluzee.config.start-retry-delay-ms as float64. Using default value 500.")
		startRetryDelay = 500
	}
	d.startRetryDelay = int64(startRetryDelay)

	maxRetryDelay, ok := config.Get("kumuluzee.config.max-retry-delay-ms").(float64)
	if !ok {
		logger.Warning("Failed to assert value kumuluzee.config.max-retry-delay-ms as float64. Using default value 900000.")
		maxRetryDelay = 900000
	}
	d.maxRetryDelay = int64(maxRetryDelay)

	client, err := api.NewClient(clientConfig)
	if err != nil {
		logger.Error("Failed to create Consul client: %s", err.Error())
	}

	logger.Info("Consul client address set to %s", clientConfig.Address)

	d.client = client

	return d
}

func (d consulDiscoverySource) RegisterService(options RegisterOptions) (serviceID string, err error) {

	regService := registerableService{
		options: &options,
	}

	// TODO: at some point, unregistered services should be removed from array!
	d.registerableServices = append(d.registerableServices, regService)

	// if * exists in config, override value in options
	if name, ok := conf.GetString("kumuluzee.name"); ok {
		regService.options.Value = name
	}

	if port, ok := conf.GetFloat("kumuluzee.server.http.port"); ok {
		regService.port = int(port)
	} else {
		regService.port = 80 // TODO: default port to register?
	}

	if addr, ok := conf.GetString("kumuluzee.server.http.address"); ok {
		regService.address = addr
	}

	if env, ok := conf.GetString("kumuluzee.env.name"); ok {
		regService.options.Environment = env
	}
	if regService.options.Environment == "" {
		regService.options.Environment = "dev"
	}

	if ver, ok := conf.GetString("kumuluzee.version"); ok {
		regService.options.Version = ver
	}
	if regService.options.Version == "" {
		regService.options.Version = "1.0.0"
	}

	if ttl, ok := conf.GetInt("kumuluzee.discovery.ttl"); ok {
		regService.options.TTL = int64(ttl)
	}
	if regService.options.TTL == 0 {
		regService.options.TTL = 30
	}

	if pingInterval, ok := conf.GetInt("kumuluzee.discovery.ping-interval"); ok {
		regService.options.PingInterval = int64(pingInterval)
	}
	if regService.options.PingInterval == 0 {
		regService.options.PingInterval = 20
	}

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(fmt.Sprintf(err.Error()))
	}

	regService.id = regService.options.Value + "-" + uuid4.String()
	regService.name = regService.options.Environment + "-" + regService.options.Value
	regService.versionTag = "version=" + regService.options.Version

	d.register(&regService, d.startRetryDelay)

	go d.checkIn(&regService, d.startRetryDelay)

	return regService.id, nil
}

func (d consulDiscoverySource) isServiceRegistered(reg *registerableService) bool {
	serviceEntries, _, err := d.client.Health().Service(reg.id, reg.versionTag, true, nil)

	if err != nil {
		d.logger.Error(fmt.Sprintf("%s", err.Error()))
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
	if isRegistered := d.isServiceRegistered(reg); isRegistered && reg.options.Singleton {
		d.logger.Error("Service is already registered, not registering with options.singleton set to true")
	} else {
		d.logger.Info("Registering service: id=%s address=%s port=%d", reg.id, reg.address, reg.port)

		agentRegistration := api.AgentServiceRegistration{
			Port: reg.port,
			ID:   reg.id,
			Name: reg.name,
			Tags: []string{"<service protocol>", reg.versionTag},
			Check: &api.AgentServiceCheck{
				CheckID: "check-" + reg.id,
				TTL:     strconv.FormatInt(reg.options.TTL, 10) + "s",
				DeregisterCriticalServiceAfter: strconv.FormatInt(10, 10) + "s",
			},
		}

		if reg.address != "" {
			agentRegistration.Address = reg.address
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

	time.Sleep(time.Duration(reg.options.PingInterval) * time.Second)
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

	var addr string
	var port int

	d.logger.Verbose("Services %s-%s available: %d", options.Environment, options.Value, len(serviceEntries))

	for _, serviceEntry := range serviceEntries {

		var wantVersion semver.Range
		var err error

		options.Version = strings.Replace(options.Version, "*", "x", -1)

		if strings.HasPrefix(options.Version, "^") {
			ver, err := semver.ParseTolerant(options.Version[1:])
			if err == nil {
				var verNext = ver
				verNext.Major++
				wantVersion, err = semver.ParseRange(">=" + ver.String() + " <" + verNext.String())
			} else {
				return Service{}, err
			}
		} else if strings.HasPrefix(options.Version, "~") {
			ver, err := semver.ParseTolerant(options.Version[1:])
			if err == nil {
				var verNext = ver
				verNext.Minor++
				wantVersion, err = semver.ParseRange(">=" + ver.String() + " <" + verNext.String())
			} else {
				return Service{}, err
			}
		} else {
			wantVersion, err = semver.ParseRange(options.Version)
		}

		if wantVersion == nil || err != nil {
			return Service{}, fmt.Errorf("wantVersion parse error: %s", err.Error())
		}

		var foundService = false
		for _, tag := range serviceEntry.Service.Tags {
			if strings.HasPrefix(tag, "version") {
				t := strings.Split(tag, "=")

				gotVersion, err := semver.ParseTolerant(t[1])
				if err == nil {
					// check if gotVersion is in wantVersion's range
					if wantVersion(gotVersion) {
						foundService = true
						break
					}
				} else {
					d.logger.Warning("Semantic version parsing failed for: %s, error: %s", t[1], err.Error())
				}
			}
		}

		if foundService {
			addr = serviceEntry.Service.Address
			if addr == "" {
				addr = serviceEntry.Node.Address
			}
			port = serviceEntry.Service.Port

			d.logger.Verbose("Found service, address=%s port=%d", addr, port)

			return Service{
				Address: addr,
				Port:    port,
			}, nil
		}
	}

	return Service{}, fmt.Errorf("Service discovery failed: No services for given query") // TODO: check if really
}
