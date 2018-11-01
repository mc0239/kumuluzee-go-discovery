package discovery

import (
	"strings"

	"github.com/blang/semver"
	"github.com/mc0239/kumuluzee-go-config/config"
)

// configuration bundle for usage with kumuluzee config bundle
type registerConfiguration struct {
	Name   string
	Server struct {
		BaseURL string `config:"base-url"`
		HTTP    struct {
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
	regconf.Server.HTTP.Port = 9000
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

func parseVersion(version string) (semver.Range, error) {
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
