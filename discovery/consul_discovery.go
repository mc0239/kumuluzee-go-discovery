package discovery

import (
	"errors"
	"fmt"
	"strconv"

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

	id         string
	port       int
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
		logger.Error(fmt.Sprintf("Failed to create Consul client: %s", err.Error()))
	}

	logger.Info(fmt.Sprintf("Consul client address set to %v", clientConfig.Address))

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

	if port, ok := conf.GetInt("kumuluzee.server.http.port"); ok {
		regService.port = port
	} else {
		regService.port = 80 // TODO: default port to register?
	}

	if env, ok := conf.GetString("kumuluzee.env.name"); ok {
		regService.options.Environment = env
	}

	if ver, ok := conf.GetString("kumuluzee.version"); ok {
		regService.options.Version = ver
	}

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(fmt.Sprintf(err.Error()))
	}

	regService.id = regService.options.Value + "-" + uuid4.String()
	regService.name = regService.options.Environment + "-" + regService.options.Value
	regService.versionTag = "version=" + regService.options.Version

	if isRegistered := d.isServiceRegistered(regService); isRegistered && regService.options.Singleton {
		d.logger.Info("Service already registered, not registering with songleton option true ...")
	} else {
		d.logger.Info("Registering service ...")

		regErr := d.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
			Port: regService.port,
			ID:   regService.id,
			Name: regService.name,
			Tags: []string{"<service protocol>", regService.versionTag},
			Check: &api.AgentServiceCheck{
				TTL: strconv.FormatInt(regService.options.TTL, 10) + "s",
				DeregisterCriticalServiceAfter: strconv.FormatInt(10, 10) + "s",
			},
		})

		if regErr != nil {
			d.logger.Error("Service register fail ...")
			// retry delay stuff somehow
		}

		regService.isRegistered = true
	}

	return regService.id, nil // since id is not specified it equals the name of the service
}

func (d consulDiscoverySource) isServiceRegistered(reg registerableService) bool {
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

func (d consulDiscoverySource) sendHeartbeat(reg registerableService) {
	d.logger.Verbose("Sending heartbeat....")

	checks, err := d.client.Agent().Checks()
	if err != nil {
		d.logger.Error("asdasdasdsd.......")
	}

	var serviceOk bool
	for _, check := range checks {
		if check.ServiceID == reg.id && check.Status == "passing" {
			serviceOk = true
		}
	}

	if !serviceOk {
		reg.isRegistered = false
		d.logger.Error("SERVCE not registered when tried hearbeating ...")
		d.RegisterService(*reg.options)
	}

}

func (d consulDiscoverySource) DiscoverService(name string, tag string, passing bool) (Service, error) {

	serviceEntries, _, err := d.client.Health().Service(name, tag, passing, nil)
	if err != nil {
		panic(err)
	}

	var addr string
	var port int

	for _, serviceEntry := range serviceEntries {
		addr = serviceEntry.Service.Address
		if addr == "" {
			addr = serviceEntry.Node.Address
		}
		port = serviceEntry.Service.Port
		fmt.Printf("Service entry: %v:%v\n", addr, strconv.Itoa(port))
		return Service{
			Address: addr,
			Port:    port,
		}, nil
	}

	return Service{}, errors.New("No services discovered") // TODO: check if really
}
