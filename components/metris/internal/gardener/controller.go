package gardener

import (
	"context"
	"strings"
	"sync"
	"time"

	ginformers "github.com/gardener/gardener/pkg/client/core/informers/externalversions"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

const (
	labelAccountID       = "account"
	labelSubAccountID    = "subaccount"
	labelHyperscalerType = "hyperscalerType"

	fieldSecretBindingName = "spec.secretBindingName"
	fieldCloudProfileName  = "spec.cloudProfileName"

	defaultResyncPeriod = time.Second * 30
)

// NewController return a new controller for watching shoots and secrets.
func NewController(client *Client, provider string, clusterChannel chan<- *Cluster, logger *zap.SugaredLogger) (*Controller, error) {
	gardenerInformerFactory := ginformers.NewSharedInformerFactoryWithOptions(
		client.GClientset,
		defaultResyncPeriod,
		ginformers.WithNamespace(client.Namespace),
		ginformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.SelectorFromSet(fields.Set{fieldCloudProfileName: provider}).String()
		}),
	)

	hyperscalertype := provider
	if hyperscalertype == "az" {
		hyperscalertype = "azure"
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		client.KClientset,
		defaultResyncPeriod,
		kubeinformers.WithNamespace(client.Namespace),
		kubeinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = labels.SelectorFromSet(labels.Set{labelHyperscalerType: hyperscalertype}).String()
		}),
	)

	shootInformer := gardenerInformerFactory.Core().V1beta1().Shoots()
	secretInformer := kubeInformerFactory.Core().V1().Secrets()

	controller := &Controller{
		providertype:            strings.ToLower(provider),
		client:                  client,
		gardenerInformerFactory: gardenerInformerFactory,
		kubeInformerFactory:     kubeInformerFactory,
		shootInformer:           shootInformer,
		secretInformer:          secretInformer,
		clusterChannel:          clusterChannel,
		logger:                  logger,
	}

	// Set up event handlers for Shoot resources
	shootInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.shootAddHandlerFunc,
		UpdateFunc: controller.shootUpdateHandlerFunc,
		DeleteFunc: controller.shootDeleteHandlerFunc,
	})

	// Set up event handler for Secret resources
	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.secretUpdateHandlerFunc,
	})

	return controller, nil
}

// Run will set up the event handlers for secrets and shoots, as well as syncing informer caches.
func (c *Controller) Run(parentCtx context.Context, parentwg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	parentwg.Add(1)
	defer parentwg.Done()

	c.logger.Info("starting controller")

	// Start the informer factories to begin populating the informer caches
	c.gardenerInformerFactory.Start(ctx.Done())
	c.kubeInformerFactory.Start(ctx.Done())

	c.logger.Debug("waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(ctx.Done(), c.shootInformer.Informer().HasSynced, c.secretInformer.Informer().HasSynced); !ok {
		c.logger.Errorf("failed to wait for caches to sync")
		return
	}

	c.logger.Debug("informer caches sync completed")

	<-ctx.Done()

	c.logger.Info("stopping controller")
}
