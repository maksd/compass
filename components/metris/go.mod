module github.com/kyma-incubator/compass/components/metris

go 1.14

require (
	github.com/Azure/azure-sdk-for-go v43.2.0+incompatible
	github.com/Azure/azure-sdk-for-go/sdk/to v0.1.0
	github.com/Azure/go-autorest/autorest v0.10.2
	github.com/Azure/go-autorest/autorest/adal v0.8.3 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.2
	github.com/Azure/go-autorest/autorest/date v0.2.0
	github.com/alecthomas/kong v0.2.9
	github.com/gardener/gardener v1.6.3
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/mapstructure v1.3.2
	github.com/prometheus/client_golang v1.7.0
	github.com/stretchr/testify v1.6.1
	go.uber.org/zap v1.15.0
	golang.org/x/crypto v0.0.0-20200406173513-056763e48d71 // indirect
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/klog v1.0.0
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace (
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.6
	k8s.io/client-go => k8s.io/client-go v0.17.6
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.17.6
)
