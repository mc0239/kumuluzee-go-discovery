package discovery

import (
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/mc0239/kumuluzee-go-config/config"
	"github.com/mc0239/logm"
	"github.com/satori/go.uuid"
)

type consulDiscoverySource struct {
	client               *api.Client
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
	clientConfig := api.DefaultConfig()

	if addr, ok := config.GetString("kumuluzee.config.consul.hosts"); ok {
		clientConfig.Address = addr
	}

	client, err := api.NewClient(clientConfig)
	if err != nil {
		logger.Error("Failed to create Consul client: %s", err.Error())
	}

	logger.Info("Consul client address set to %s", clientConfig.Address)

	d := consulDiscoverySource{
		client: client,
		logger: logger,
	}
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

	if ver, ok := conf.GetString("kumuluzee.version"); ok {
		regService.options.Version = ver
	}

	if ttl, ok := conf.GetInt("kumuluzee.discovery.ttl"); ok {
		regService.options.TTL = int64(ttl)
	}
	if regService.options.TTL == 0 {
		regService.options.TTL = 30
	}

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(fmt.Sprintf(err.Error()))
	}

	regService.id = regService.options.Value + "-" + uuid4.String()
	regService.name = regService.options.Environment + "-" + regService.options.Value
	regService.versionTag = "version=" + regService.options.Version

	if isRegistered := d.isServiceRegistered(&regService); isRegistered && regService.options.Singleton {
		d.logger.Error("Service is already registered, not registering with options.singleton set to true")
	} else {
		d.logger.Info("Registering service: id=%s address=%s port=%d", regService.id, regService.address, regService.port)

		agentRegistration := api.AgentServiceRegistration{
			Port: regService.port,
			ID:   regService.id,
			Name: regService.name,
			Tags: []string{"<service protocol>", regService.versionTag},
			Check: &api.AgentServiceCheck{
				CheckID: "check-" + regService.id,
				TTL:     strconv.FormatInt(regService.options.TTL, 10) + "s",
				DeregisterCriticalServiceAfter: strconv.FormatInt(10, 10) + "s",
			},
		}

		if regService.address != "" {
			agentRegistration.Address = regService.address
		}

		err = d.client.Agent().ServiceRegister(&agentRegistration)

		if err != nil {
			d.logger.Error(fmt.Sprintf("Service registration failed: %s", err.Error()))
			// retry delay stuff somehow
		}

		d.logger.Info("Service registered, id=%s", regService.id)
		regService.isRegistered = true
	}

	go d.checkIn(&regService)

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

func (d consulDiscoverySource) checkIn(reg *registerableService) {
	d.logger.Verbose("Updating TTL for service %s", reg.id)

	err := d.client.Agent().UpdateTTL("check-"+reg.id, "serviceid="+reg.id+" time="+time.Now().Format("2006-01-02 15:04:05"), "passing")
	if err != nil {
		d.logger.Error("Updating TTL failed for service %s, error: %s", reg.id, err.Error())
	}

	time.Sleep(time.Duration(reg.options.PingInterval) * time.Second)
	d.checkIn(reg)
}

func (d consulDiscoverySource) DiscoverService(options DiscoverOptions) (Service, error) {

	// TODO: ENV? ACCESSTYPE?
	serviceEntries, _, err := d.client.Health().Service(options.Value, "version="+options.Version, true, nil)
	if err != nil {
		d.logger.Error("Service discovery failed: %s", err.Error())
		return Service{}, fmt.Errorf("Service discovery failed: %s", err.Error())
	}

	var addr string
	var port int

	for _, serviceEntry := range serviceEntries {
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

	return Service{}, fmt.Errorf("Service discovery failed: No services for given query") // TODO: check if really
}
