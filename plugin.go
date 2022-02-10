package engine

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// InstallPlugin 安装插件，传入插件配置生成插件信息对象
func InstallPlugin(config config.Plugin) *Plugin {
	t := reflect.TypeOf(config).Elem()
	name := strings.TrimSuffix(t.Name(), "Config")
	plugin := &Plugin{
		Name:   name,
		Config: config,
	}
	_, pluginFilePath, _, _ := runtime.Caller(1)
	configDir := filepath.Dir(pluginFilePath)
	if parts := strings.Split(configDir, "@"); len(parts) > 1 {
		plugin.Version = parts[len(parts)-1]
	}
	if _, ok := Plugins[name]; ok {
		return nil
	}
	if config != EngineConfig {
		plugin.Entry = log.WithField("plugin", name)
		Plugins[name] = plugin
		plugin.Infoln(Green("install"), BrightBlue(plugin.Version))
	}
	return plugin
}

// Plugin 插件信息
type Plugin struct {
	context.Context    `json:"-"`
	context.CancelFunc `json:"-"`
	Name               string        //插件名称
	Config             config.Plugin //插件配置
	Version            string        //插件版本
	RawConfig          config.Config //配置的map形式方便查询
	Modified           config.Config //修改过的配置项
	*log.Entry
}

type PullPlugin interface {
	PullStream(string, Puller) bool
}

func (opt *Plugin) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if opt == nil {
		return
	}
	var cors bool
	if v, ok := opt.RawConfig["cors"]; ok {
		cors = v.(bool)
	} else if EngineConfig.CORS {
		cors = true
	}
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	if opt != Engine {
		pattern = "/" + strings.ToLower(opt.Name) + pattern
	}
	opt.Infoln("http handle added:", pattern)
	EngineConfig.HandleFunc(pattern, func(rw http.ResponseWriter, r *http.Request) {
		if cors {
			util.CORS(rw, r)
		}
		opt.Debugln(r.RemoteAddr, " -> ", pattern)
		handler(rw, r)
	})
}

// 读取独立配置合并入总配置中
func (opt *Plugin) assign() {
	f, err := os.Open(opt.settingPath())
	if err == nil {
		if err = yaml.NewDecoder(f).Decode(&opt.Modified); err == nil {
			if opt.RawConfig == nil {
				opt.RawConfig = opt.Modified
			} else {
				opt.RawConfig.Assign(opt.Modified)
			}
		}
	}
	t := reflect.TypeOf(opt.Config).Elem()
	// 用全局配置覆盖没有设置的配置
	for _, fname := range MergeConfigs {
		if _, ok := t.FieldByName(fname); ok {
			if Engine.RawConfig.Has(fname) {
				if !opt.RawConfig.Has(fname) {
					opt.RawConfig.Set(fname, Engine.RawConfig[fname])
				} else if opt.RawConfig.HasChild(fname) {
					opt.RawConfig.GetChild(fname).Merge(Engine.RawConfig.GetChild(fname))
				}
			}
		}
	}
	opt.registerHandler()
	opt.Update()
}

func (opt *Plugin) Update() {
	if opt.CancelFunc != nil {
		opt.CancelFunc()
	}
	opt.Context, opt.CancelFunc = context.WithCancel(Engine)
	opt.RawConfig.Unmarshal(opt.Config)
	opt.autoPull()
	go opt.Config.Update(opt.RawConfig)
}

func (opt *Plugin) autoPull() {
	t := reflect.TypeOf(opt.Config).Elem()
	v := reflect.ValueOf(opt.Config).Elem()
	for i, j := 0, t.NumField(); i < j; i++ {
		if t.Field(i).Name == "Pull" {
			var pullConfig config.Pull
			reflect.ValueOf(&pullConfig).Elem().Set(v.Field(i))
			for streamPath, url := range pullConfig.AutoPullList {
				puller := Puller{RemoteURL: url, Config: &pullConfig}
				if pullConfig.PullOnStart {
					opt.Config.(PullPlugin).PullStream(streamPath, puller)
				} else if pullConfig.PullOnSubscribe {
					PullOnSubscribeList[streamPath] = PullOnSubscribe{opt.Config.(PullPlugin), puller}
				}
			}
		}
	}
}
func (opt *Plugin) registerHandler() {
	t := reflect.TypeOf(opt.Config)
	v := reflect.ValueOf(opt.Config)
	// 注册http响应
	for i, j := 0, t.NumMethod(); i < j; i++ {
		mt := t.Method(i)
		mv := v.Method(i)
		if mv.CanConvert(handlerFuncType) {
			patten := "/"
			if mt.Name != "ServeHTTP" {
				patten = strings.ToLower(strings.ReplaceAll(mt.Name, "_", "/"))
			}
			reflect.ValueOf(opt.HandleFunc).Call([]reflect.Value{reflect.ValueOf(patten), mv})
		}
	}
}

func (opt *Plugin) settingPath() string {
	return filepath.Join(settingDir, strings.ToLower(opt.Name)+".yaml")
}

func (opt *Plugin) Save() error {
	file, err := os.OpenFile(opt.settingPath(), os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer file.Close()
		err = yaml.NewEncoder(file).Encode(opt.Modified)
	}
	if err == nil {
		opt.Info("config saved")
	}
	return err
}
