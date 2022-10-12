package config

import (
	"context"
	"net/http"

	. "github.com/logrusorgru/aurora"
	"golang.org/x/sync/errgroup"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var _ HTTPConfig = (*HTTP)(nil)

type HTTP struct {
	ListenAddr    string
	ListenAddrTLS string
	CertFile      string
	KeyFile       string
	CORS          bool //是否自动添加CORS头
	UserName      string
	Password      string
	mux           *http.ServeMux
}
type HTTPConfig interface {
	GetHTTPConfig() *HTTP
	Listen(ctx context.Context) error
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}

func (config *HTTP) HandleFunc(path string, f func(http.ResponseWriter, *http.Request)) {
	if config.mux == nil {
		config.mux = http.NewServeMux()
	}
	if config.CORS {
		f = util.CORS(f)
	}
	if config.UserName != "" && config.Password != "" {
		f = util.BasicAuth(config.UserName, config.Password, f)
	}
	config.mux.HandleFunc(path, f)
}

func (config *HTTP) GetHTTPConfig() *HTTP {
	return config
}

// ListenAddrs Listen http and https
func (config *HTTP) Listen(ctx context.Context) error {
	if config.mux == nil {
		return nil
	}
	var g errgroup.Group
	if config.ListenAddrTLS != "" && (config == &Global.HTTP || config.ListenAddrTLS != Global.ListenAddrTLS) {
		g.Go(func() error {
			log.Info("🌐 https listen at ", Blink(config.ListenAddrTLS))
			return http.ListenAndServeTLS(config.ListenAddrTLS, config.CertFile, config.KeyFile, config.mux)
		})
	}
	if config.ListenAddr != "" && (config == &Global.HTTP || config.ListenAddr != Global.ListenAddr) {
		g.Go(func() error {
			log.Info("🌐 http listen at ", Blink(config.ListenAddr))
			return http.ListenAndServe(config.ListenAddr, config.mux)
		})
	}
	g.Go(func() error {
		<-ctx.Done()
		return ctx.Err()
	})
	return g.Wait()
}
