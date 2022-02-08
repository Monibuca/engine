package config

import (
	"context"
	"net/http"

	"golang.org/x/sync/errgroup"
)

type HTTP struct {
	ListenAddr    string
	ListenAddrTLS string
	CertFile      string
	KeyFile       string
	CORS          bool //是否自动添加CORS头
}

// ListenAddrs Listen http and https
func (config *HTTP) Listen(ctx context.Context, plugin HTTPPlugin) error {
	var g errgroup.Group
	if config.ListenAddrTLS != "" {
		g.Go(func() error {
			return http.ListenAndServeTLS(config.ListenAddrTLS, config.CertFile, config.KeyFile, plugin)
		})
	}
	if config.ListenAddr != "" {
		g.Go(func() error { return http.ListenAndServe(config.ListenAddr, plugin) })
	}
	g.Go(func() error {
		<-ctx.Done()
		return ctx.Err()
	})
	return g.Wait()
}
