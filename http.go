package engine

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/util"
)

const (
	NO_SUCH_CONIFG = "no such config"
	NO_SUCH_STREAM = "no such stream"
)

type GlobalConfig struct {
	*config.Engine
}

func (conf *GlobalConfig) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Write([]byte("Monibuca API Server\n"))
	for _, api := range apiList {
		rw.Write([]byte(api + "\n"))
	}
}

func (conf *GlobalConfig) API_summary(rw http.ResponseWriter, r *http.Request) {
	util.ReturnJson(summary.collect, time.Second, rw, r)
}

func (conf *GlobalConfig) API_plugins(rw http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(rw).Encode(Plugins); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}

func (conf *GlobalConfig) API_stream(rw http.ResponseWriter, r *http.Request) {
	if streamPath := r.URL.Query().Get("streamPath"); streamPath != "" {
		if s := Streams.Get(streamPath); s != nil {
			if err := json.NewEncoder(rw).Encode(s); err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(rw, NO_SUCH_STREAM, http.StatusNotFound)
		}
	} else {
		http.Error(rw, "no streamPath", http.StatusBadRequest)
	}
}

func (conf *GlobalConfig) API_sysInfo(rw http.ResponseWriter, r *http.Request) {
	var IP []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					IP = append(IP, ipnet.IP.String())
				}
			}
		}
	}

	if err := json.NewEncoder(rw).Encode(&struct {
		Version   string
		StartTime string
		IP        []string
	}{Engine.Version, StartTime.Format("2006-01-02 15:04:05"), IP}); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}

func (conf *GlobalConfig) API_closeStream(w http.ResponseWriter, r *http.Request) {
	if streamPath := r.URL.Query().Get("streamPath"); streamPath != "" {
		if s := Streams.Get(streamPath); s != nil {
			s.Close()
			w.Write([]byte("ok"))
		} else {
			http.Error(w, NO_SUCH_STREAM, http.StatusNotFound)
		}
	} else {
		http.Error(w, "no streamPath", http.StatusBadRequest)
	}
}

// API_getConfig 获取指定的配置信息
func (conf *GlobalConfig) API_getConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			if err := json.NewEncoder(w).Encode(c.RawConfig); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			http.Error(w, NO_SUCH_CONIFG, http.StatusNotFound)
		}
	} else if err := json.NewEncoder(w).Encode(Engine.RawConfig); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// API_modifyConfig 修改并保存配置
func (conf *GlobalConfig) API_modifyConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			if err := json.NewDecoder(r.Body).Decode(&c.Modified); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				c.Save()
				c.RawConfig.Assign(c.Modified)
				w.Write([]byte("ok"))
			}
		} else {
			http.Error(w, NO_SUCH_CONIFG, http.StatusNotFound)
		}
	} else if err := json.NewDecoder(r.Body).Decode(&Engine.Modified); err == nil {
		Engine.Save()
		Engine.RawConfig.Assign(Engine.Modified)
		w.Write([]byte("ok"))
	} else {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// API_updateConfig 热更新配置
func (conf *GlobalConfig) API_updateConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			c.Update(c.Modified)
			w.Write([]byte("ok"))
		} else {
			http.Error(w, NO_SUCH_CONIFG, http.StatusNotFound)
		}
	} else {
		Engine.Update(Engine.Modified)
		w.Write([]byte("ok"))
	}
}
