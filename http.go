package engine

import (
	"encoding/json"
	"net/http"
	"github.com/Monibuca/engine/v4/log"
	"github.com/Monibuca/engine/v4/config"
	. "github.com/logrusorgru/aurora"
)

type GlobalConfig struct {
	*http.ServeMux
	*config.Engine
}

func (cfg *GlobalConfig) Update(override config.Config) {
	// 使得RawConfig具备全量配置信息，用于合并到插件配置中
	Engine.RawConfig = config.Struct2Config(cfg.Engine)
	log.Info(Green("api server start at"), BrightBlue(cfg.ListenAddr), BrightBlue(cfg.ListenAddrTLS))
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
