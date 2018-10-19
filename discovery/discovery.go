package discovery

import (
	"github.com/mc0239/kumuluzee-go-config/config"
	"github.com/mc0239/logm"
)

// Options struct is used when instantiating a new Util.
type Options struct {
	// Additional configuration source to connect to. Possible values are: "consul"
	Extension string
	// ConfigPath is a path to configuration file, including the configuration file name.
	// Passing an empty string will default to config/config.yaml
	ConfigPath string
	// LogLevel can be used to limit the amount of logging output. Default log level is 0. Level 4
	// will only output Warnings and Errors, and level 5 will only output errors.
	// See package github.com/mc0239/logm for more details on logging and log levels.
	LogLevel int
}

// RegisterOptions is used when registering a service
type RegisterOptions struct {
	// Service name to register the service by.
	// Can be overridden with configuration key kumuluzee.name
	Value string
	// Time to live of a registration key in the store (in seconds).
	// Default value is 30.
	// Can be overridden with configuration key kumuluzee.discovery.ttl
	TTL int64
	// Interval in which service updates registration key value in store.
	// Default value is 20.
	// Can be overridden with configuration key kumuluzee.discovery.ping-interval
	PingInterval int64
	// Environment in which the service is registered.
	// Default value is "dev".
	// Can be overridden with configuration key kumuluzee.env.name
	Environment string
	// Version of the service to be registered.
	// Default value is "1.0.0".
	// Can be overridden with configuration key kumuluzee.version
	Version string
	// If set to true, only once instance of service with the same name, version and environment is registered.
	// Default value is false.
	Singleton bool
}

// DiscoverOptions is used when discovering services
type DiscoverOptions struct {
	// Name of the service to discover.
	Value string
	// Environment of the service to discover.
	// If value is not provided, it uses value from configuration with key kumuluzee.env.name
	// If value is not specified and key in configuration does not exists, value defaults to 'dev'.
	Environment string
	// Version of the service to discover.
	// Uses semantic versioning?
	// Default value is "*", which resolves to highest deployed version.
	Version string
	// TODO
	AccessType string
}

// Util is used for registering and discovering services from a service discovery source.
// Util should be initialized with discovery.New() function
type Util struct {
	discoverySource discoverySource
	Logger          logm.Logm
}

type Service struct {
	Address string
	Port    string
}

type discoverySource interface {
	RegisterService(options RegisterOptions) (serviceID string, err error)
	DiscoverService(options DiscoverOptions) (Service, error)
}

// New instantiates Util struct with initialized service discovery
func New(options Options) Util {

	lgr := logm.New("Kumuluz-discovery")

	var src discoverySource

	if options.Extension == "consul" {
		src = newConsulDiscoverySource(config.Options{
			ConfigPath: options.ConfigPath,
			LogLevel:   logm.LvlWarning,
		}, &lgr)
	} else if options.Extension == "etcd" {
		src = newEtcdDiscoverySource(config.Options{
			ConfigPath: options.ConfigPath,
			LogLevel:   logm.LvlWarning,
		}, &lgr)
	} else {
		lgr.Error("Specified discovery source extension is invalid.")
	}

	k := Util{
		src,
		lgr,
	}

	return k
}

// RegisterService registers service using service discovery client with given RegisterOptions
func (d Util) RegisterService(options RegisterOptions) (string, error) {
	return d.discoverySource.RegisterService(options)
}

// DiscoverService discovery services using service discovery client with given RegisterOptions
func (d Util) DiscoverService(options DiscoverOptions) (Service, error) {
	return d.discoverySource.DiscoverService(options)
}
