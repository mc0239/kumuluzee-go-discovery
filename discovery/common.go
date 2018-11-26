package discovery

import (
	"fmt"
	"math/rand"
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

type discoveredService struct {
	version    semver.Version
	id         string
	directURL  string
	gatewayURL string
	// TODO: containerURL ?
}

//

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

func fillDefaultDiscoverOptions(options *DiscoverOptions) {
	// Load default values
	if options.Environment == "" {
		options.Environment = "dev"
	}
	if options.Version == "" {
		options.Version = ">=0.0.0" // discover ANY version
	}
	if options.AccessType == "" {
		options.AccessType = AccessTypeGateway
	}
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

func extractServicesWithVersion(services []discoveredService, wantVersion semver.Range) []discoveredService {
	var matchingServices []discoveredService
	// first, get all services that are within range, and store the latest version found
	// then, return services that match only the latest version

	var latestVersion semver.Version
	for _, s := range services {
		// if service version is in range of wantVersion
		if wantVersion(s.version) {
			// store latest version
			if s.version.GTE(latestVersion) {
				latestVersion = s.version
			}
		}
	}

	for _, s := range services {
		// if service is of latestVersion
		if s.version.EQ(latestVersion) {
			matchingServices = append(matchingServices, s)
		}
	}

	return matchingServices
}

// returns a randomly picked instace from discovered services.
// Note that function can return both a valid, non-empty service string and an error, which means
// that no proper service could be found and the lastKnownService string is being returned
func pickRandomServiceInstance(discoveredInstances []discoveredService, options DiscoverOptions, lastKnownService string) (service string, err error) {
	wantVersion, err := parseVersion(options.Version)
	if err != nil {
		if lastKnownService != "" {
			return lastKnownService, fmt.Errorf("wantVersion parse error: %s", err.Error())
		}
		return "", fmt.Errorf("wantVersion parse error: %s", err.Error())
	}

	// pick a random service instance from registered instances that match version
	instances := extractServicesWithVersion(discoveredInstances, wantVersion)
	if len(instances) == 0 {
		if lastKnownService != "" {
			return lastKnownService, fmt.Errorf("No service found (no matching version)")
		}
		return "", fmt.Errorf("No service found (no matching version)")
	}

	randomInstance := instances[rand.Intn(len(instances))]
	if options.AccessType == AccessTypeGateway && randomInstance.gatewayURL != "" {
		return randomInstance.gatewayURL, nil
	} else if randomInstance.directURL != "" {
		return randomInstance.directURL, nil
	} else {
		if lastKnownService != "" {
			return lastKnownService, fmt.Errorf("No service found (no service with URL)")
		}
		return "", fmt.Errorf("No service found (no service with URL)")
	}
}
