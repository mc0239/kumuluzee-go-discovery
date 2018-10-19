package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"strings"

	"github.com/mc0239/kumuluzee-go-config/config"
	"github.com/mc0239/logm"
	"go.etcd.io/etcd/client"
)

type etcdDiscoveryService struct {
	client *client.Client

	startRetryDelay int64
	maxRetryDelay   int64

	configOptions config.Options

	logger *logm.Logm

	registerableService registerableService
}

func newEtcdDiscoverySource(options config.Options, logger *logm.Logm) discoverySource {
	var d etcdDiscoveryService
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

func (d etcdDiscoveryService) RegisterService(options RegisterOptions) (serviceID string, err error) {
	// TODO
	return "", nil
}

func (d etcdDiscoveryService) DiscoverService(options DiscoverOptions) (Service, error) {

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
