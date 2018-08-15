package discovery

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/hashicorp/consul/api"
	"github.com/mc0239/kumuluzee-go-config/config"
)

type consulDiscoverySource struct {
	client *api.Client
}

func initConsulDiscoverySource(config config.ConfigUtil) discoverySource {
	clientConfig := api.DefaultConfig()

	if addr, ok := config.GetString("kumuluzee.config.consul.hosts"); ok {
		clientConfig.Address = addr
	}

	client, err := api.NewClient(clientConfig)
	if err != nil {
		lgr.logE(fmt.Sprintf("Failed to create Consul client: %s", err.Error()))
	}

	lgr.logI(fmt.Sprintf("Consul client address set to %v", clientConfig.Address))

	d := consulDiscoverySource{
		client: client,
	}
	return d
}

func (d consulDiscoverySource) RegisterService() (serviceID string, err error) {
	envName, ok := conf.GetString("kumuluzee.env.name")
	if !ok {
		envName = "dev"
	}

	serName, ok := conf.GetString("kumuluzee.name")
	if !ok {
		serName = "unnamed"
	}

	version, ok := conf.GetString("kumuluzee.version")
	if !ok {
		version = "1.0.0"
	}

	/*var deregTime int
	deregTime, ok = conf.GetInt("kumuluzee.config.consul.deregister-critical-service-after-s")
	if !ok {
		deregTime = 60
	}*/

	reg := api.AgentServiceRegistration{
		//ID:   "kek2",
		Name:    envName + "-" + serName,
		Tags:    []string{"version=" + version},
		Port:    9000,
		Address: "192.168.2.50",
		Checks:  api.AgentServiceChecks{
			/*&api.AgentServiceCheck{
				Name:     "health failure",
				Interval: "10s",
				HTTP:     "http://127.0.0.1:9000/health?s=fail",
				DeregisterCriticalServiceAfter: strconv.Itoa(deregTime) + "s",
			},*/
			/*&api.AgentServiceCheck{
				Name:     "health alright",
				Interval: "30s",
				HTTP:     "http://127.0.0.1:9000/health?s=ok",
				DeregisterCriticalServiceAfter: strconv.Itoa(deregTime) + "s",
			},*/
			/*&api.AgentServiceCheck{
				Name:     "health warning",
				Interval: "10s",
				HTTP:     "http://127.0.0.1:9000/health?s=warn",
				DeregisterCriticalServiceAfter: "20s",
			},*/
		},
	}

	err = d.client.Agent().ServiceRegister(&reg)
	if err != nil {
		return "", err
	}

	return reg.Name, nil // since id is not specified it equals the name of the service
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
