package engine

import (
	"encoding/json"
	"net/http"
	"net/http/pprof"

	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/log"
	. "github.com/logrusorgru/aurora"
)

type GlobalConfig struct {
	*http.ServeMux
	*config.Engine
}

func (cfg *GlobalConfig) OnEvent(event any) {
	switch event.(type) {
	case FirstConfig:
		if cfg.EnablePProf {
			cfg.HandleFunc("/debug/pprof/", pprof.Index)
			cfg.HandleFunc("/debug/pprof/profile",pprof.Profile)
			cfg.HandleFunc("/debug/pprof/trace", pprof.Trace)
			// cfg.HandleFunc("/debug/pprof/profile", pprof.Profile)
			// cfg.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
			// cfg.Handle("/debug/pprof/block", pprof.Handler("block"))
			// cfg.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
			// cfg.Handle("/debug/pprof/heap", pprof.Handler("heap"))
			// cfg.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
			// cfg.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
		}
		log.Info(Green("api server start at"), BrightBlue(cfg.ListenAddr), BrightBlue(cfg.ListenAddrTLS))
		// cfg.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		// 	w.Write([]byte("Monibuca API Server"))
		// })
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
