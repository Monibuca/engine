package config

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/logrusorgru/aurora/v4"
	"golang.org/x/sync/errgroup"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ HTTPConfig = (*HTTP)(nil)

type Middleware func(string, http.Handler) http.Handler
type HTTP struct {
	ListenAddr    string
	ListenAddrTLS string
	CertFile      string
	KeyFile       string
	CORS          bool `default:"true"` //是否自动添加CORS头
	UserName      string
	Password      string
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	IdleTimeout   time.Duration
	mux           *http.ServeMux
	middlewares   []Middleware
}
type HTTPConfig interface {
	GetHTTPConfig() *HTTP
	Listen(ctx context.Context) error
	Handle(string, http.Handler)
	Handler(*http.Request) (http.Handler, string)
	AddMiddleware(Middleware)
}

func (config *HTTP) AddMiddleware(middleware Middleware) {
	config.middlewares = append(config.middlewares, middleware)
}

func (config *HTTP) Handle(path string, f http.Handler) {
	if config.mux == nil {
		config.mux = http.NewServeMux()
	}
	if config.CORS {
		f = util.CORS(f)
	}
	if config.UserName != "" && config.Password != "" {
		f = util.BasicAuth(config.UserName, config.Password, f)
	}
	for _, middleware := range config.middlewares {
		f = middleware(path, f)
	}
	config.mux.Handle(path, f)
}

func (config *HTTP) GetHTTPConfig() *HTTP {
	return config
}

func (config *HTTP) Handler(r *http.Request) (h http.Handler, pattern string) {
	return config.mux.Handler(r)
}

// ListenAddrs Listen http and https
func (config *HTTP) Listen(ctx context.Context) error {
	if config.mux == nil {
		return nil
	}
	var g errgroup.Group
	if config.ListenAddrTLS != "" && (config == &Global.HTTP || config.ListenAddrTLS != Global.ListenAddrTLS) {
		g.Go(func() error {
			if Global.LogLang == "zh" {
				log.Info("🌐 https 监听在 ", aurora.Blink(config.ListenAddrTLS))
			} else {
				log.Info("🌐 https listen at ", aurora.Blink(config.ListenAddrTLS))
			}
			cer, _ := tls.X509KeyPair(LocalCert, LocalKey)
			var server = http.Server{
				Addr:         config.ListenAddrTLS,
				ReadTimeout:  config.ReadTimeout,
				WriteTimeout: config.WriteTimeout,
				IdleTimeout:  config.IdleTimeout,
				Handler:      config.mux,
				TLSConfig: &tls.Config{
					Certificates: []tls.Certificate{cer},
					CipherSuites: []uint16{
						tls.TLS_AES_128_GCM_SHA256,
						tls.TLS_CHACHA20_POLY1305_SHA256,
						tls.TLS_AES_256_GCM_SHA384,
						//tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						//tls.TLS_RSA_WITH_AES_256_CBC_SHA,
						//tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
						//tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
						tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
					},
				},
			}
			return server.ListenAndServeTLS(config.CertFile, config.KeyFile)
		})
	}
	if config.ListenAddr != "" && (config == &Global.HTTP || config.ListenAddr != Global.ListenAddr) {
		g.Go(func() error {
			if Global.LogLang == "zh" {
				log.Info("🌐 http 监听在 ", aurora.Blink(config.ListenAddr))
			} else {
				log.Info("🌐 http listen at ", aurora.Blink(config.ListenAddr))
			}
			var server = http.Server{
				Addr:         config.ListenAddr,
				ReadTimeout:  config.ReadTimeout,
				WriteTimeout: config.WriteTimeout,
				IdleTimeout:  config.IdleTimeout,
				Handler:      config.mux,
			}
			return server.ListenAndServe()
		})
	}
	g.Go(func() error {
		<-ctx.Done()
		return ctx.Err()
	})
	return g.Wait()
}
