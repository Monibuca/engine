package engine

import (
	"encoding/json"
	"net/http"
	log "github.com/sirupsen/logrus"
	"github.com/Monibuca/engine/v4/config"
	. "github.com/logrusorgru/aurora"
)

type GlobalConfig struct {
	*http.ServeMux
	*config.Engine
}

func (cfg *GlobalConfig) Update(override config.Config) {
	Engine.RawConfig = config.Struct2Config(cfg.Engine)
	log.Infoln(Green("api server start at"), BrightBlue(cfg.ListenAddr), BrightBlue(cfg.ListenAddrTLS))
	cfg.Listen(Engine, cfg)
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
