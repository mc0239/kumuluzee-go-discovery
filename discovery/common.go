package discovery

import "github.com/mc0239/kumuluzee-go-config/config"

func getRetryDelays(conf config.Util) (startRD, maxRD int64) {
	if sdl, ok := conf.GetInt("kumuluzee.config.start-retry-delay-ms"); ok {
		startRD = int64(sdl)
	} else {
		startRD = 500
	}

	if mdl, ok := conf.GetInt("kumuluzee.config.max-retry-delay-ms"); ok {
		maxRD = int64(mdl)
	} else {
		maxRD = 900000
	}

	return
}

func loadServiceRegisterConfiguration(confOptions config.Options, regOptions RegisterOptions) (regconf registerConfiguration) {
	// Load default values
	regconf = registerConfiguration{}
	regconf.Server.HTTP.Port = 80 // TODO: default port to register?
	regconf.Env.Name = "dev"
	regconf.Version = "1.0.0"
	regconf.Discovery.TTL = 30
	regconf.Discovery.PingInterval = 20

	// Load from configuration file, overriding defaults
	config.NewBundle("kumuluzee", &regconf, confOptions)

	// Load from RegisterOptions, override file configuration
	if regOptions.Value != "" {
		regconf.Name = regOptions.Value
	}
	if regOptions.Environment != "" {
		regconf.Env.Name = regOptions.Environment
	}
	if regOptions.Version != "" {
		regconf.Version = regOptions.Version
	}
	if regOptions.TTL != 0 {
		regconf.Discovery.TTL = regOptions.TTL
	}
	if regOptions.PingInterval != 0 {
		regconf.Discovery.PingInterval = regOptions.PingInterval
	}

	return
}
