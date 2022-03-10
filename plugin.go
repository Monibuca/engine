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

func (opt *Plugin) logHandler(pattern string, handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		opt.Debug("visit", zap.String("path", r.URL.String()), zap.String("remote", r.RemoteAddr))
		handler(rw, r)
	}
}
func (opt *Plugin) handleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if opt == nil {
		return
	}
	conf, ok := opt.Config.(config.HTTPConfig)
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	if ok {
		opt.Info("http handle added:" + pattern)
		conf.HandleFunc(pattern, opt.logHandler(pattern, handler))
	}
	if opt != Engine {
		pattern = "/" + strings.ToLower(opt.Name) + pattern
		opt.Info("http handle added to engine:" + pattern)
		EngineConfig.HandleFunc(pattern, opt.logHandler(pattern, handler))
	}
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
	if conf, ok := opt.Config.(config.HTTPConfig); ok {
		httpConf := conf.GetHTTPConfig()
		httpConf.InitMux()
	}
	opt.registerHandler()
	opt.run()
}

func (opt *Plugin) run() {
	opt.Context, opt.CancelFunc = context.WithCancel(Engine)
	opt.RawConfig.Unmarshal(opt.Config)
	opt.Debug("config", zap.Any("config", opt.Config))
	opt.Config.OnEvent(FirstConfig(opt.RawConfig))
	if conf, ok := opt.Config.(config.HTTPConfig); ok {
		httpconf := conf.GetHTTPConfig()
		if httpconf.ListenAddr != "" && httpconf.ListenAddr != EngineConfig.ListenAddr {
			go conf.Listen(opt)
		}
	}
}

// Update 热更新配置
func (opt *Plugin) Update(conf config.Config) {
	conf.Unmarshal(opt.Config)
	opt.Config.OnEvent(conf)
}

func (opt *Plugin) registerHandler() {
	t := reflect.TypeOf(opt.Config)
	v := reflect.ValueOf(opt.Config)
	// 注册http响应
	for i, j := 0, t.NumMethod(); i < j; i++ {
		name := t.Method(i).Name
		if handler, ok := v.Method(i).Interface().(func(http.ResponseWriter, *http.Request)); ok {
			patten := "/"
			if name != "ServeHTTP" {
				patten = strings.ToLower(strings.ReplaceAll(name, "_", "/"))
			}
			opt.handleFunc(patten, handler)
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

func (opt *Plugin) Pull(streamPath string, url string, puller IPuller, save bool) (err error) {
	conf, ok := opt.Config.(config.PullConfig)
	if !ok {
		return NoPullConfigErr
	}
	pullConf := conf.GetPullConfig()

	puller.init(streamPath, url, pullConf)

	if err = puller.Connect(); err != nil {
		return
	}

	if err = opt.Publish(streamPath, puller); err != nil {
		return
	}

	go func() {
		defer opt.Info("stop pull", zap.String("remoteURL", url))
		for puller.Reconnect() {
			if puller.Pull(); !puller.IsClosed() {
				if err = puller.Connect(); err != nil {
					return
				}
				if err = opt.Publish(streamPath, puller); err != nil {
					if Streams.Get(streamPath).Publisher != puller {
						return
					}
				}
			} else {
				return
			}
		}
	}()

	if save {
		pullConf.AddPull(streamPath, url)
		opt.Modified["pull"] = config.Struct2Config(pullConf)
		if err = opt.Save(); err != nil {
			opt.Error("save faild", zap.Error(err))
		}
	}
	return
}
var NoPushConfigErr = errors.New("no push config")
func (opt *Plugin) Push(streamPath string, url string, pusher IPusher, save bool) (err error) {
	conf, ok := opt.Config.(config.PushConfig)
	if !ok {
		return NoPushConfigErr
	}
	pushConfig := conf.GetPushConfig()

	pusher.init(streamPath, url, pushConfig)

	if err = pusher.Connect(); err != nil {
		return
	}

	if err = opt.Subscribe(streamPath, pusher); err != nil {
		return
	}

	go func() {
		defer opt.Info("stop push", zap.String("remoteURL", url))
		for pusher.Reconnect() {
			if pusher.Push(); !pusher.IsClosed() {
				if err = pusher.Connect(); err != nil {
					return
				}
				if err = opt.Subscribe(streamPath, pusher); err != nil {
					return
				}
			} else {
				return
			}
		}
	}()

	if save {
		pushConfig.AddPush(streamPath, url)
		opt.Modified["push"] = config.Struct2Config(pushConfig)
		if err = opt.Save(); err != nil {
			opt.Error("save faild", zap.Error(err))
		}
	}
	return
}
