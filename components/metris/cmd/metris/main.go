package main

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/alecthomas/kong"
	"github.com/kyma-incubator/compass/components/metris/internal/edp"
	"github.com/kyma-incubator/compass/components/metris/internal/gardener"
	"github.com/kyma-incubator/compass/components/metris/internal/provider"
	"github.com/kyma-incubator/compass/components/metris/internal/server"
	"github.com/kyma-incubator/compass/components/metris/internal/utils"
	"github.com/mitchellh/go-homedir"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog"

	// import to initialize prometheus registry.
	_ "github.com/kyma-incubator/compass/components/metris/internal/metrics"

	// import to initialize provider.
	_ "github.com/kyma-incubator/compass/components/metris/internal/provider/azure"
)

var (
	// version is the current version, set by the go linker's -X flag at build time.
	version = "dev"
)

type app struct {
	EDPConfig      edp.Config       `kong:"embed=true,prefix='edp-'"`
	ProviderConfig provider.Config  `kong:"embed=true,prefix='provider-'"`
	ServerConfig   server.Config    `kong:"embed=true"`
	ConfigFile     kong.ConfigFlag  `kong:"help='Location of the config file.',type='path'"`
	Kubeconfig     string           `kong:"help='Path to the Gardener kubeconfig file.',required=true,default='${kubeconfig}',env='METRIS_KUBECONFIG'"`
	LogLevel       zap.AtomicLevel  `kong:"help='Logging level. (${loglevels})',default='info',env='METRIS_LOGLEVEL'"`
	Version        kong.VersionFlag `kong:"help='Print version information and quit.'"`
}

func main() {
	var (
		err        error
		homefld    string
		kubeconfig string
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	homefld, err = homedir.Dir()
	if err == nil {
		kubeconfig = filepath.Join(homefld, ".kube", "config")
	}

	cli := app{}
	clictx := kong.Parse(&cli,
		kong.Name("metris"),
		kong.Description("Metris is a metering component that collects data and sends them to EDP."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: false}),
		kong.Vars{
			"version":    fmt.Sprintf("Version: %s\n", version),
			"kubeconfig": kubeconfig,
			"loglevels":  "debug,info,warn,error",
		},
		kong.TypeMapper(reflect.TypeOf(zap.AtomicLevel{}), utils.LogLevelDecoder()),
		kong.Configuration(kong.JSON, ""),
	)

	var slogger *zap.SugaredLogger
	{
		var cfg zap.Config
		if version == "dev" {
			cfg = zap.NewDevelopmentConfig()
		} else {
			cfg = zap.NewProductionConfig()
			cfg.DisableCaller = true
		}
		cfg.Level = cli.LogLevel
		logger, logerr := cfg.Build()
		if logerr != nil {
			panic(logerr)
		}

		// capture standard golang "log" package output and force it through this logger
		_ = zap.RedirectStdLog(logger)

		// capture klog logs and write them at debug level because some dependencies are using it
		if loggerwriter, logerr := zap.NewStdLogAt(logger, zapcore.DebugLevel); logerr == nil {
			klog.SetOutput(loggerwriter.Writer())
		}

		slogger = logger.Sugar()
	}

	clusterChannel := make(chan *gardener.Cluster, cli.ProviderConfig.Buffer)
	eventChannel := make(chan *edp.Event, cli.EDPConfig.Buffer)
	wg := sync.WaitGroup{}

	edpclient := edp.NewClient(&cli.EDPConfig, nil, eventChannel, slogger.Named("edp"))

	go edpclient.Run(ctx, &wg)

	// start provider to fetch metrics from the clusters
	cli.ProviderConfig.ClusterChannel = clusterChannel
	cli.ProviderConfig.EventsChannel = eventChannel
	cli.ProviderConfig.Logger = slogger.Named(cli.ProviderConfig.Type)
	prov, err := provider.GetProvider(&cli.ProviderConfig)
	clictx.FatalIfErrorf(err)

	go prov.Run(ctx, &wg)

	// start gardener controller to sync clusters with provider
	gclient, err := gardener.NewClient(cli.Kubeconfig)
	clictx.FatalIfErrorf(err)

	ctrl, err := gardener.NewController(gclient, cli.ProviderConfig.Type, clusterChannel, slogger.Named("gardener"))
	clictx.FatalIfErrorf(err)

	go ctrl.Run(ctx, &wg)

	// start web server for metris metrics and profiling
	if len(cli.ServerConfig.ListenAddr) > 0 {
		s, err := server.NewServer(cli.ServerConfig, slogger.Named("metris"), cli.LogLevel.ServeHTTP)

		clictx.FatalIfErrorf(err)

		go s.Start(ctx, &wg)
	}

	utils.SetupSignalHandler(func() {
		cancel()
	})

	wg.Wait()

	slogger.Info("metris stopped")

	if loggererr := slogger.Sync(); loggererr != nil {
		fmt.Println(loggererr)
	}
}
