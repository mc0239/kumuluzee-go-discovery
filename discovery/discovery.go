package discovery

import (
	"errors"

	"github.com/mc0239/kumuluzee-go-config/config"
)

var conf config.ConfigUtil

type DiscoveryUtil struct {
	discoverySources []discoverySource
}

type Service struct {
	Address string
	Port    int
}

type discoverySource interface {
	RegisterService() (serviceID string, err error)
	DiscoverService(name string, tag string, passing bool) (Service, error)
}

func Initialize(ext string, configPath string) DiscoveryUtil {

	conf = config.Initialize("", "")

	var src discoverySource

	if ext == "consul" {
		src = initConsulDiscoverySource(conf) // TODO should actually pass file source
	} else if ext == "etcd" {
		// TODO:
	} else {
		// TODO: invalid ext
	}

	k := DiscoveryUtil{[]discoverySource{src}}

	return k
}

func (d DiscoveryUtil) RegisterService() (string, error) {

	for _, ds := range d.discoverySources {
		return ds.RegisterService()
	}

	return "", errors.New("Service registration failed with all clients")
}

func (d DiscoveryUtil) DiscoverService(name string, tag string, passing bool) (Service, error) {

	for _, ds := range d.discoverySources {
		return ds.DiscoverService(name, tag, passing)
	}

	return Service{}, errors.New("Service discovery failed with all clients")
}
