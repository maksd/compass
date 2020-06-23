package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	serverReadTimeout  = 5 * time.Second
	serverWriteTimeout = 10 * time.Second
	serverIdleTimeout  = 15 * time.Second
	serverStopTimeout  = 30 * time.Second

	sessionCacheCapacity = 128
)

// List of default ciphers to use.
// https://golang.org/pkg/crypto/tls/#pkg-constants
var defaultCiphers = []uint16{
	tls.TLS_FALLBACK_SCSV, // TLS_FALLBACK_SCSV should always be first, see https://tools.ietf.org/html/rfc7507#section-6
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

// List of default curves to use.
var defaultCurves = []tls.CurveID{
	tls.X25519,
	tls.CurveP256,
}

type Config struct {
	ListenAddr   string `kong:"help='Address and port for the server to listen on.',env='METRIS_LISTENADDR',required=true"`
	TLSCertFile  string `kong:"help='Path to TLS certificate file.',type='path',env='METRIS_TLSCERTFILE'"`
	TLSKeyFile   string `kong:"help='Path to TLS key file.',type='path',env='METRIS_TLSKEYFILE'"`
	ProfilerPort int    `kong:"help='Port to expose debugging information.',optional=true,env='METRIS_PROFILER_PORT'"`
}

// Server represents an HTTP server
type Server struct {
	Server          *http.Server
	ProfServer      *http.Server
	Healthy         int32
	useTLS          bool
	logger          *zap.SugaredLogger
	loglevelHandler func(w http.ResponseWriter, r *http.Request)
}

func NewServer(c Config, logger *zap.SugaredLogger, loglevelHandler func(w http.ResponseWriter, r *http.Request)) (*Server, error) {
	s := &Server{
		logger:          logger,
		ProfServer:      &http.Server{},
		loglevelHandler: loglevelHandler,
	}

	tlsConfig := &tls.Config{}

	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		logger.Debug("TLS Configuration found")

		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, err
		}

		tlsConfig = &tls.Config{
			Certificates:             []tls.Certificate{cert},
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			CurvePreferences:         defaultCurves,
			PreferServerCipherSuites: true,
			CipherSuites:             defaultCiphers,
			ClientSessionCache:       tls.NewLRUClientSessionCache(sessionCacheCapacity),
		}
		s.useTLS = true
	}

	router := http.NewServeMux()
	router.Handle("/healthz", s.HealthHandler())
	router.Handle("/metrics", promhttp.Handler())

	if loglevelHandler != nil {
		router.HandleFunc("/~/loglevel", s.loglevelHandler)
	}

	// adding go profiling tools
	if c.ProfilerPort > 0 {
		profRouter := http.NewServeMux()
		profRouter.HandleFunc("/debug/pprof/", pprof.Index)
		profRouter.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		profRouter.HandleFunc("/debug/pprof/profile", pprof.Profile)
		profRouter.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		profRouter.HandleFunc("/debug/pprof/trace", pprof.Trace)

		// for security reason only listen on localhost
		s.ProfServer = &http.Server{
			Addr:    fmt.Sprintf("localhost:%d", c.ProfilerPort),
			Handler: profRouter,
		}
	}

	s.Server = &http.Server{
		Addr:      c.ListenAddr,
		TLSConfig: tlsConfig,
		Handler:   router,

		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}

	return s, nil
}

func (s *Server) Start(parentctx context.Context, parentwg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(parentctx)
	defer cancel()

	parentwg.Add(1)
	defer parentwg.Done()

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	atomic.StoreInt32(&s.Healthy, 1)

	if s.ProfServer.Addr != "" {
		s.logger.Infof("listening (http/pprof) on %s", s.ProfServer.Addr)

		go func() {
			if err := s.ProfServer.ListenAndServe(); err != http.ErrServerClosed {
				s.logger.Fatalf("listening on %s failed: %v", s.ProfServer.Addr, err)
			}
		}()
	}

	if s.useTLS {
		s.logger.Infof("listening (https) on %s", s.Server.Addr)

		if err := s.Server.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			s.logger.Fatalf("listening on %s failed: %v", s.Server.Addr, err)
		}
	} else {
		s.logger.Infof("listening (http) on %s", s.Server.Addr)

		if err := s.Server.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Fatalf("listening on %s failed: %v", s.Server.Addr, err)
		}
	}
}

func (s *Server) Stop() {
	atomic.StoreInt32(&s.Healthy, 0)

	ctx, cancel := context.WithTimeout(context.Background(), serverStopTimeout)
	defer cancel()

	go func(ctx context.Context) {
		<-ctx.Done()

		if ctx.Err() == context.Canceled {
			return
		} else if ctx.Err() == context.DeadlineExceeded {
			s.logger.Panic("Timeout while stopping the server, killing instance!")
		}
	}(ctx)

	if s.ProfServer.Addr != "" {
		s.ProfServer.SetKeepAlivesEnabled(false)

		if err := s.ProfServer.Shutdown(ctx); err != nil {
			s.logger.Errorf("Could not gracefully shutdown the pprof: %v\n", err)
		}
	}

	s.Server.SetKeepAlivesEnabled(false)

	if err := s.Server.Shutdown(ctx); err != nil {
		s.logger.Fatalf("Could not gracefully shutdown the server: %v\n", err)
	}
}

func (s *Server) HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuscode := http.StatusServiceUnavailable
		if atomic.LoadInt32(&s.Healthy) == 1 {
			statuscode = http.StatusOK
		}
		w.WriteHeader(statuscode)
	})
}
