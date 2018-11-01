package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mc0239/kumuluzee-go-config/config"
	"github.com/mc0239/logm"
	uuid "github.com/satori/go.uuid"
	"go.etcd.io/etcd/client"
)

type etcdDiscoverySource struct {
	client *client.Client

	startRetryDelay int64
	maxRetryDelay   int64

	configOptions config.Options

	logger *logm.Logm

	serviceInstance *etcdServiceInstance
}

type etcdServiceInstance struct {
	isRegistered bool

	id         string
	name       string
	versionTag string

	singleton bool

	options *registerConfiguration
}

func newEtcdDiscoverySource(options config.Options, logger *logm.Logm) discoverySource {
	var d etcdDiscoverySource
	logger.Verbose("Initializing etcd discovery source")
	d.logger = logger

	d.configOptions = options
	conf := config.NewUtil(options)

	startRD, maxRD := getRetryDelays(conf)
	d.startRetryDelay = startRD
	d.maxRetryDelay = maxRD
	logger.Verbose("start-retry-delay-ms=%d, max-retry-delay-ms=%d", d.startRetryDelay, d.maxRetryDelay)

	var etcdAddress string
	if addr, ok := conf.GetString("kumuluzee.discovery.etcd.hosts"); ok {
		etcdAddress = addr
	}
	if client, err := createEtcdClient(etcdAddress); err == nil {
		logger.Info("etcd client address set to: %v", etcdAddress)
		d.client = client
	} else {
		logger.Error("Failed to create etcd client: %s", err.Error())
	}

	return d
}

func (d etcdDiscoverySource) RegisterService(options RegisterOptions) (serviceID string, err error) {
	regconf := loadServiceRegisterConfiguration(d.configOptions, options)

	regService := etcdServiceInstance{
		options:   &regconf,
		singleton: options.Singleton,
	}

	d.serviceInstance = &regService

	uuid4, err := uuid.NewV4()
	if err != nil {
		d.logger.Error(err.Error())
	}

	regService.id = uuid4.String()
	regService.name = regService.options.Env.Name + "-" + regService.options.Name
	regService.versionTag = "version=" + regService.options.Version

	d.register(d.startRetryDelay)
	go d.ttlUpdate(d.startRetryDelay)

	return regService.id, nil
}

func (d etcdDiscoverySource) DiscoverService(options DiscoverOptions) (Service, error) {

	kvClient := client.NewKeysAPI(*d.client)

	kvPath := fmt.Sprintf("environments/%s/services/%s/%s/instances/", options.Environment, options.Value, options.Version)
	kvDir, err := kvClient.Get(context.Background(), kvPath, nil)
	if err != nil {
		return Service{}, err
	}

	randomIndex := rand.Intn(kvDir.Node.Nodes.Len())
	randomNode := kvDir.Node.Nodes[randomIndex]

	instance, err := kvClient.Get(context.Background(), randomNode.Key, nil)
	if err != nil {
		return Service{}, err
	}

	for _, node := range instance.Node.Nodes {
		var keySuffix string
		switch options.AccessType {
		case "gateway":
			keySuffix = "gatewayUrl"
			break
		case "direct":
			keySuffix = "url"
			break
		default:
			keySuffix = "gatewayUrl"
			d.logger.Warning("Invalid AccessType specified, using gateway")
			break
		}

		// fmt.Printf("key=%v value=%v", node.Key, node.Value)
		if strings.HasSuffix(node.Key, keySuffix) {
			serviceURL, err := url.Parse(node.Value)
			if err != nil {
				return Service{}, err
			}
			// print("addr=%s port=%s", serviceURL.Hostname(), serviceURL.Port())
			return Service{
				Address: serviceURL.Hostname(),
				Port:    serviceURL.Port(),
			}, nil
		}
	}

	return Service{}, fmt.Errorf("No service found")
}

// functions that aren't configSource methods

func (d etcdDiscoverySource) isServiceRegistered() bool {
	// TODO
	return false
}

func (d etcdDiscoverySource) register(retryDelay int64) {
	reg := d.serviceInstance
	if isRegistered := d.isServiceRegistered(); isRegistered && reg.singleton {
		d.logger.Error("Service is already registered, not registering with options.singleton set to true")
	} else {
		d.logger.Info("Registering service: id=%s address=%s port=%d", reg.id, reg.options.Server.HTTP.Address, reg.options.Server.HTTP.Port)

		regkvPath := fmt.Sprintf("/environments/%s/services/%s/%s/instances/%s/url",
			reg.options.Env.Name, reg.options.Name, reg.options.Version, reg.id)

		// TODO where is service address assumed from ?
		serviceURL := reg.options.Server.HTTP.Address + ":" + string(reg.options.Server.HTTP.Port)

		ttlDuration, err := time.ParseDuration(strconv.FormatInt(reg.options.Discovery.TTL, 10) + "s")
		if err != nil {
			d.logger.Warning("Failed to parse TTL duration, using default: 10 seconds")
			ttlDuration = 10 * time.Second
		}

		kvClient := client.NewKeysAPI(*d.client)
		resp, err := kvClient.Set(context.Background(), regkvPath, serviceURL, &client.SetOptions{
			TTL: ttlDuration,
		})

		d.logger.Info("resp?=%v", resp)

		if err != nil {
			d.logger.Error(fmt.Sprintf("Service registration failed: %s", err.Error()))
			// sleep for current delay
			time.Sleep(time.Duration(retryDelay) * time.Millisecond)

			// exponentially extend retry delay, but keep it at most maxRetryDelay
			newRetryDelay := retryDelay * 2
			if newRetryDelay > d.maxRetryDelay {
				newRetryDelay = d.maxRetryDelay
			}
			d.register(newRetryDelay)
			return
		}

		d.logger.Info("Service registered, id=%s", reg.id)
		reg.isRegistered = true
	}
}

func (d etcdDiscoverySource) ttlUpdate(retryDelay int64) {
	reg := d.serviceInstance
	d.logger.Verbose("Updating TTL for service %s", reg.id)

	regkvPath := fmt.Sprintf("/environments/%s/services/%s/%s/instances/%s/url",
		reg.options.Env.Name, reg.options.Name, reg.options.Version, reg.id)

	ttlDuration, err := time.ParseDuration(strconv.FormatInt(reg.options.Discovery.TTL, 10) + "s")
	if err != nil {
		d.logger.Warning("Failed to parse TTL duration, using default: 10 seconds")
		ttlDuration = 10 * time.Second
	}

	kvClient := client.NewKeysAPI(*d.client)
	_, err = kvClient.Set(context.Background(), regkvPath, "", &client.SetOptions{
		TTL:     ttlDuration,
		Refresh: true,
	})

	if err != nil {
		d.logger.Error("Updating TTL failed for service %s, error: %s, retry delay: %d ms", reg.id, err.Error(), retryDelay)

		// sleep for current delay
		time.Sleep(time.Duration(retryDelay) * time.Millisecond)

		// exponentially extend retry delay, but keep it at most maxRetryDelay
		newRetryDelay := retryDelay * 2
		if newRetryDelay > d.maxRetryDelay {
			newRetryDelay = d.maxRetryDelay
		}
		d.ttlUpdate(newRetryDelay)
		return
	}

	time.Sleep(time.Duration(reg.options.Discovery.PingInterval) * time.Second)
	d.ttlUpdate(d.startRetryDelay)
	return
}

// functions that aren't configSource methods or etcdConfigSource methods

func createEtcdClient(address string) (*client.Client, error) {
	clientConfig := client.Config{
		Endpoints: []string{address}, // TODO: split string in case of multiple hosts?
	}

	client, err := client.New(clientConfig)
	if err != nil {
		return nil, err
	}
	return &client, nil
}
