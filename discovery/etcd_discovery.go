package discovery

import (
	"context"
	"fmt"
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

	// TODO ACCESSTYPE?
	kvClient := client.NewKeysAPI(*d.client)
	kvDir, err := kvClient.Get(context.Background(), "environments/dev/services/customer-service/1.0.0/instances/", nil)
	if err != nil {
		return Service{}, err
	}
	for _, n := range kvDir.Node.Nodes {
		inst, err := kvClient.Get(context.Background(), n.Key, nil)
		if err != nil {
			return Service{}, err
		}
		for _, n2 := range inst.Node.Nodes {
			fmt.Printf("key=%v value=%v", n2.Key, n2.Value)
			if strings.HasSuffix(n2.Key, "url") {
				surl, err := url.Parse(n2.Value)
				if err != nil {
					return Service{}, err
				}
				print("addr=%s port=%s", surl.Hostname(), surl.Port())
				return Service{
					Address: surl.Hostname(),
					Port:    surl.Port(),
				}, nil
			}
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
