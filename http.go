package engine

import (
	"encoding/json"
	"net/http"

	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	"v4.m7s.live/engine/config"
	"v4.m7s.live/engine/log"
)

type GlobalConfig struct {
	*http.ServeMux
	*config.Engine
}

func (cfg *GlobalConfig) OnEvent(event any) {
	switch event.(type) {
	case FirstConfig:
		log.Info(Green("api server start at"), BrightBlue(cfg.ListenAddr), BrightBlue(cfg.ListenAddrTLS))
		cfg.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Debug("visit", zap.String("path", "/"), zap.String("remote", r.RemoteAddr))
			w.Write([]byte("Monibuca API Server"))
		})
		go cfg.Listen(Engine, cfg)
	}
}

func (config *GlobalConfig) API_sysInfo(rw http.ResponseWriter, r *http.Request) {
	json.NewEncoder(rw).Encode(&struct {
		Version   string
		StartTime string
	}{Engine.Version, StartTime.Format("2006-01-02 15:04:05")})
}

func (config *GlobalConfig) API_closeStream(w http.ResponseWriter, r *http.Request) {
	if streamPath := r.URL.Query().Get("stream"); streamPath != "" {
		if s := Streams.Get(streamPath); s != nil {
			s.Close()
			w.Write([]byte("success"))
		} else {
			w.Write([]byte("no such stream"))
		}
	} else {
		w.Write([]byte("no query stream"))
	}
}
