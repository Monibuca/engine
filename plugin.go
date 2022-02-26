package engine

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
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
		plugin.Version = util.LastElement(parts)
	}
	if _, ok := Plugins[name]; ok {
		return nil
	}
	if config != EngineConfig {
		plugin.Logger = log.With(zap.String("plugin", name))
		Plugins[name] = plugin
		plugin.Info("install", zap.String("version", plugin.Version))
	}
	return plugin
}

type FirstConfig config.Config

// Plugin 插件信息
type Plugin struct {
	context.Context    `json:"-"`
	context.CancelFunc `json:"-"`
	Name               string        //插件名称
	Config             config.Plugin //插件配置
	Version            string        //插件版本
	RawConfig          config.Config //配置的map形式方便查询
	Modified           config.Config //修改过的配置项
	*zap.Logger
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
	opt.Info("http handle added:" + pattern)
	EngineConfig.HandleFunc(pattern, func(rw http.ResponseWriter, r *http.Request) {
		if cors {
			util.CORS(rw, r)
		}
		opt.Debug("visit", zap.String("path", pattern), zap.String("remote", r.RemoteAddr))
		handler(rw, r)
	})
}

// 读取独立配置合并入总配置中
// TODO: 覆盖逻辑有待商榷
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
	if opt.RawConfig["enable"] == false {
		opt.Warn("disabled")
		return
	}
	t := reflect.TypeOf(opt.Config).Elem()
	// 用全局配置覆盖没有设置的配置
	for _, fname := range MergeConfigs {
		if _, ok := t.FieldByName(fname); ok {
			if v, ok := Engine.RawConfig[strings.ToLower(fname)]; ok {
				if !opt.RawConfig.Has(fname) {
					opt.RawConfig.Set(fname, v)
				} else if opt.RawConfig.HasChild(fname) {
					opt.RawConfig.GetChild(fname).Merge(Engine.RawConfig.GetChild(fname))
				}
			}
		}
	}
	opt.registerHandler()
	opt.run()
}

func (opt *Plugin) run() {
	opt.Context, opt.CancelFunc = context.WithCancel(Engine)
	opt.RawConfig.Unmarshal(opt.Config)
	opt.Debug("config", zap.Any("config", opt.Config))
	opt.Config.OnEvent(FirstConfig(opt.RawConfig))
	opt.autoPull()
}

// Update 热更新配置
func (opt *Plugin) Update(conf config.Config) {
	conf.Unmarshal(opt.Config)
	opt.Config.OnEvent(conf)
}

func (opt *Plugin) autoPull() {
	t := reflect.TypeOf(opt.Config).Elem()
	v := reflect.ValueOf(opt.Config).Elem()
	for i, j := 0, t.NumField(); i < j; i++ {
		switch t.Field(i).Name {
		case "Pull":
			var pullConfig config.Pull
			reflect.ValueOf(&pullConfig).Elem().Set(v.Field(i))
			for streamPath, url := range pullConfig.PullList {
				if pullConfig.PullOnStart {
					opt.Pull(streamPath, url, false)
				} else if pullConfig.PullOnSubscribe {
					PullOnSubscribeList[streamPath] = PullOnSubscribe{opt, streamPath, url}
				}
			}
		case "Push":
			var pushConfig config.Push
			reflect.ValueOf(&pushConfig).Elem().Set(v.Field(i))
			for streamPath, url := range pushConfig.PushList {
				PushOnPublishList[streamPath] = append(PushOnPublishList[streamPath], PushOnPublish{opt.Config, Pusher{Client[config.Push]{&pushConfig, streamPath, url, 0}}})
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
			} else if opt == Engine {
				continue
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

func (opt *Plugin) Publish(streamPath string, pub IPublisher) error {
	conf, ok := opt.Config.(config.PublishConfig)
	if !ok {
		conf = EngineConfig
	}
	return pub.receive(streamPath, pub, conf.GetPublishConfig())
}

func (opt *Plugin) Subscribe(streamPath string, sub ISubscriber) error {
	conf, ok := opt.Config.(config.SubscribeConfig)
	if !ok {
		conf = EngineConfig
	}
	return sub.receive(streamPath, sub, conf.GetSubscribeConfig())
}

var NoPullConfigErr = errors.New("no pull config")

type PullerPromise struct {
	*util.Promise[Puller, struct{}]
}

func (opt *Plugin) Pull(streamPath string, url string, save bool) error {
	conf, ok := opt.Config.(config.PullConfig)
	if !ok {
		return NoPullConfigErr
	}
	var puller Puller
	puller.StreamPath = streamPath
	puller.RemoteURL = url
	puller.Config = conf.GetPullConfig()
	promise := util.NewPromise[Puller, struct{}](puller)
	opt.Config.OnEvent(PullerPromise{promise})
	_, err := promise.AWait()
	if err == nil && save {
		puller.Config.AddPull(streamPath, url)
		opt.Modified["pull"] = config.Struct2Config(puller.Config)
		if err := opt.Save(); err != nil {
			opt.Error("save faild", zap.Error(err))
		}
	}
	return err
}
