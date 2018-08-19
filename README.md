# KumuluzEE Go Discovery

Note: crossed content is work in progress.

KumuluzEE Go Discovery is a service discovery library for the KumuluzEE microservice framework.  It is a Go  package based on a [KumuluzEE Discovery](https://github.com/kumuluz/kumuluzee-discovery), service discovery extension for microservices written in Java programming language. It provides support for service registration, service discovery and client side load balancing.

KumuluzEE Go Discovery provides full support for microservices packed as Docker containers. It also provides full support for executing microservices in clusters and cloud-native platforms with full support for Kubernetes.

## Install

You can `go get` this package:

```
$ go get github.com/mc0239/kumuluzee-go-discovery
```

Or you can get it via a package manager, for example `dep`:

```
$ dep ensure -add github.com/mc0239/kumuluzee-go-discovery
```

## Setup

Before you can start using this library you should configure properties in order to successfully connect to desired discovery framework. If you wish to connect to Consul check section [Configuring Consul](https://github.com/kumuluz/kumuluzee-discovery#configuring-consul) <s>or [Configuring etcd](https://github.com/kumuluz/kumuluzee-discovery#configuring-etcd) to connect to etcd</s>.

Library also supports retry delays on watch connection errors. For more information check [Retry delays](https://github.com/kumuluz/kumuluzee-discovery#retry-delays).

## Usage

### discovery.Util

*discovery.New(options)*

Connect to a given discovery source. Function accepts `discovery.Options` struct with following fields:
* **Extension** (string): name of service discovery source, possible values are "consul" 
* **FilePath** (string): path to configuration source file, defaults to "config/config.yaml"

Firstly we need to import KumuluzEE Discovery client.

```go
// import package
import "github.com/mc0239/kumuluzee-go-discovery/discovery"

// usage
var disc discovery.Util

disc = discovery.New(discovery.Options(
    Extension: "consul",
})
```

***.registerService(options)***

Registers service to specified discovery source with given options.

Function accepts `discovery.RegisterOptions` struct with following fields:
* **Value** (String): service name of a registered service. Service name can be overridden with configuration key  `kumuluzee.name`,
* **TTL** (Integer, optional): seconds to live of a registration key in the store. Default value is `30`. TTL can be overridden with configuration key `kumuluzee.discovery.ttl`,
* **PingInterval** (Integer, optional): an interval in which service updates registration key value in the store. Default value is `20` seconds. Ping interval can be overridden with configuration key  `kumuluzee.discovery.ping-interval`,
* **Environment** (String, optional): environment in which service is registered. Default value is `'dev'`. Environment can be overridden with configuration key  `kumuluzee.env.name`,
* **Version** (String, optional): version of service to be registered. Default value is `'1.0.0'`. Version can be overridden with configuration key  `kumuluzee.version`,
* **Singleton** (Boolean, optional): if true ensures, that only one instance of service with the same name, version and environment is registered. Default value is `false`.

Example of service registration:

```go
disc.RegisterService({
    Value: "my-service",
    TTL: 40,
    PingInterval: 20,
    Environment: "test",
    Version: "1.1.0",
    Singleton: false,
})
```

<s>To register a service with etcd, service URL has to be provided with the configuration key `kumuluzee.server.base-url` in the following format:`http://localhost:8080`.</s> Consul implementation uses agent's IP address for the URL of registered services, so this key is not used.

***.discoverService(options)***

Discovers service on specified discovery source.

Function takes four parameters:

* **value** (String): name of the service we want to discover,
* **environment** (String, optional): service environment, e.g. prod, dev, test. If value is not provided, environment is set to the value defined with the configuration key  `kumuluzee.env.name`. If the configuration key is not present, value is set to  `'dev'`,
* **version** (String, optional): service version or NPM version range. Default value is `'*'`, which resolves to the highest deployed version,
* **accessType** (String, optional): defines, which URL is returned. Supported values are  `'GATEWAY'`  and  `'DIRECT'`. Default is  `'GATEWAY'`.


Example of service discovery:
```javascript
const serviceUrl = await KumuluzeeDiscovery.discoverService({
    value: 'customer-service',
    version: '^1.1.0',
    environment: 'dev',
    accessType: 'GATEWAY',
})
```
If no service is found, `null` is returned.


**Access types**

Service discovery supports two access types:

*   `GATEWAY`  returns gateway URL, if it is present. If not, behavior is the same as with  `DIRECT`,
*   `DIRECT`  always returns base URL or container URL.

If etcd implementation is used, gateway URL is read from etcd key-value store used for service discovery. It is stored in key  `/environments/'environment'/services/'serviceName'/'serviceVersion'/gatewayUrl`  and is automatically updated, if value changes.

If Consul implementation is used, gateway URL is read from Consul key-value store. It is stored in key`/environments/'environment'/services/'serviceName'/'serviceVersion'/gatewayUrl`  and is automatically updated on changes, similar as in etcd implementation.

**NPM-like versioning**

Service discovery support NPM-like versioning. If service is registered with version in NPM format, it can be discovered using a NPM range. Some examples:

-   `'*'` would discover the latest version in NPM format, registered with etcd
-   `'^1.0.4'` would discover the latest minor version in NPM format, registered with etcd
-   `'~1.0.4'` would discover the latest patch version in NPM format, registered with etcd

For more information see  [NPM semver documentation](http://docs.npmjs.com/misc/semver).

### Using the last-known service

Etcd implementation improves resilience by saving the information of the last present service, before it gets deleted. This means, that etcd discovery extension will return the URL of the last-known service, if no services are present in the registry. When discovering the last-known service a warning is logged.

### Executing service discovery only when needed

When discovering service with `discoverService` function the service is discovered every time the function is called. While in a run time service is listening for changes so the value of discovered service is changed in a background. Every time a change of discovered service happens info about the change is logged. So in order to access the new discovered service value you need to call discovering function again.

### Cluster, cloud-native platforms and Kubernetes
KumuluzEE Node.js Discovery is also fully compatible with clusters and cloud-native platforms. For more information check [Cluster, cloud-native platforms and Kubernetes](https://github.com/kumuluz/kumuluzee-discovery#cluster-cloud-native-platforms-and-kubernetes).

## Changelog

Recent changes can be viewed on Github on the  [Releases Page](https://github.com/kumuluz/kumuluzee/releases)

## Contribute

See the  [contributing docs](https://github.com/kumuluz/kumuluzee-nodejs-discovery/blob/master/CONTRIBUTING.md)

When submitting an issue, please follow the  [guidelines](https://github.com/kumuluz/kumuluzee-nodejs-discovery/blob/master/CONTRIBUTING.md#bugs).

When submitting a bugfix, write a test that exposes the bug and fails before applying your fix. Submit the test alongside the fix.

When submitting a new feature, add tests that cover the feature.

## License

MIT
