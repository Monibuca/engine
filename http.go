package engine

import (
	"encoding/json"
	"net/http"
	"time"

	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

type GlobalConfig struct {
	*config.Engine
}

func (config *GlobalConfig) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Write([]byte("Monibuca API Server"))
}

func (config *GlobalConfig) API_summary(rw http.ResponseWriter, r *http.Request) {
	util.ReturnJson(summary.collect, time.Second, rw, r)
}

func (config *GlobalConfig) API_sysInfo(rw http.ResponseWriter, r *http.Request) {
	json.NewEncoder(rw).Encode(&struct {
		Version   string
		StartTime string
	}{Engine.Version, StartTime.Format("2006-01-02 15:04:05")})
}

func (config *GlobalConfig) API_closeStream(w http.ResponseWriter, r *http.Request) {
	if streamPath := r.URL.Query().Get("streamPath"); streamPath != "" {
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
