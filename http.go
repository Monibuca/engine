package engine

import (
	"encoding/json"
	"net/http"

	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
)

func (config *EngineConfig) Update(override Config) {
	override.Unmarshal(config)
	if config.Context == nil {
		config.Context = Ctx
		handleFunc("/sysInfo", sysInfo)
		handleFunc("/closeStream", closeStream)
		util.Print(Green("api server start at "), BrightBlue(config.HTTP.ListenAddr), BrightBlue(config.HTTP.ListenAddrTLS))
		config.HTTP.Listen(config)
	}
}

func handleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	config.HandleFunc("/api"+pattern, func(rw http.ResponseWriter, r *http.Request) {
		if config.HTTP.CORS {
			util.CORS(rw, r)
		}
		handler(rw, r)
	})
}

func closeStream(w http.ResponseWriter, r *http.Request) {
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
func sysInfo(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(&struct {
		Version   string
		StartTime string
	}{Version, StartTime.Format("2006-01-02 15:04:05")})
}
