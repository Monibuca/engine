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

func (conf *GlobalConfig) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Write([]byte("Monibuca API Server"))
}

func (conf *GlobalConfig) API_summary(rw http.ResponseWriter, r *http.Request) {
	util.ReturnJson(summary.collect, time.Second, rw, r)
}

func (conf *GlobalConfig) API_sysInfo(rw http.ResponseWriter, r *http.Request) {
	json.NewEncoder(rw).Encode(&struct {
		Version   string
		StartTime string
	}{Engine.Version, StartTime.Format("2006-01-02 15:04:05")})
}

func (conf *GlobalConfig) API_closeStream(w http.ResponseWriter, r *http.Request) {
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

// API_getConfig 获取指定的配置信息
func (conf *GlobalConfig) API_getConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			json.NewEncoder(w).Encode(c.RawConfig)
		} else {
			w.Write([]byte("no such config"))
		}
	} else {
		json.NewEncoder(w).Encode(Engine.RawConfig)
	}
}

// API_modifyConfig 修改并保存配置
func (conf *GlobalConfig) API_modifyConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			json.NewDecoder(r.Body).Decode(&c.Modified)
			c.Save()
			c.RawConfig.Assign(c.Modified)
		} else {
			w.Write([]byte("no such config"))
		}
	} else {
		json.NewDecoder(r.Body).Decode(&Engine.Modified)
		Engine.Save()
		Engine.RawConfig.Assign(Engine.Modified)
	}
}

// API_updateConfig 热更新配置
func (conf *GlobalConfig) API_updateConfig(w http.ResponseWriter, r *http.Request) {
	if configName := r.URL.Query().Get("name"); configName != "" {
		if c, ok := Plugins[configName]; ok {
			c.Update(c.Modified)
		} else {
			w.Write([]byte("no such config"))
		}
	} else {
		Engine.Update(Engine.Modified)
	}
}
