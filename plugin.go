package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mcuadros/go-defaults"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

// InstallPlugin 安装插件，传入插件配置生成插件信息对象
func InstallPlugin(config config.Plugin) *Plugin {
	defaults.SetDefaults(config)
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
	} else {
		plugin.Version = pluginFilePath
	}
	if _, ok := Plugins[name]; ok {
		return nil
	}
	if config != EngineConfig {
		plugin.Logger = log.With(zap.String("plugin", name))
		Plugins[name] = plugin
		plugins = append(plugins, plugin)
		plugin.Info("install", zap.String("version", plugin.Version))
	}
	return plugin
}

type FirstConfig config.Config
type DefaultYaml string

// Plugin 插件信息
type Plugin struct {
	context.Context    `json:"-"`
	context.CancelFunc `json:"-"`
	Name               string        //插件名称
	Config             config.Plugin `json:"-"` //类型化的插件配置
	Version            string        //插件版本
	Yaml               string        //配置文件中的配置项
	modifiedYaml       string        //修改过的配置的yaml文件内容
	RawConfig          config.Config //最终合并后的配置的map形式方便查询
	Modified           config.Config //修改过的配置项
	*zap.Logger        `json:"-"`
	saveTimer          *time.Timer //用于保存的时候的延迟，防抖
}

func (opt *Plugin) logHandler(pattern string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		opt.Debug("visit", zap.String("path", r.URL.String()), zap.String("remote", r.RemoteAddr))
		handler.ServeHTTP(rw, r)
	})
}
func (opt *Plugin) handle(pattern string, handler http.Handler) {
	if opt == nil {
		return
	}
	conf, ok := opt.Config.(config.HTTPConfig)
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	if ok {
		opt.Debug("http handle added:" + pattern)
		conf.Handle(pattern, opt.logHandler(pattern, handler))
	}
	if opt != Engine {
		pattern = "/" + strings.ToLower(opt.Name) + pattern
		opt.Debug("http handle added to engine:" + pattern)
		EngineConfig.Handle(pattern, opt.logHandler(pattern, handler))
	}
	apiList = append(apiList, pattern)
}

// 读取独立配置合并入总配置中
func (opt *Plugin) assign() {
	f, err := os.Open(opt.settingPath())
	defer f.Close()
	if err == nil {
		var b []byte
		b, err = io.ReadAll(f)
		if err == nil {
			opt.modifiedYaml = string(b)
			if err = yaml.Unmarshal(b, &opt.Modified); err == nil {
				err = yaml.Unmarshal(b, &opt.RawConfig)
			}
		}
		if err != nil {
			opt.Warn("assign config failed", zap.Error(err))
		}
	}
	if opt == Engine {
		opt.registerHandler()
		return
	}
	if opt.RawConfig == nil {
		opt.RawConfig = config.Config{}
	} else if opt.RawConfig["enable"] == false {
		opt.Warn("disabled")
		return
	} else if opt.RawConfig["enable"] == true {
		//移除这个属性防止反序列化报错
		delete(opt.RawConfig, "enable")
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
	opt.Config.OnEvent(FirstConfig(opt.RawConfig))
	var buffer bytes.Buffer
	err := yaml.NewEncoder(&buffer).Encode(opt.Config)
	if err != nil {
		panic(err)
	}
	err = yaml.NewDecoder(&buffer).Decode(&opt.RawConfig)
	if err != nil {
		panic(err)
	}
	delete(opt.RawConfig, "defaultyaml")
	opt.Debug("config", zap.Any("config", opt.Config))
	// opt.RawConfig = config.Struct2Config(opt.Config)
	if conf, ok := opt.Config.(config.HTTPConfig); ok {
		go conf.Listen(opt)
	}
}

// Update 热更新配置
func (opt *Plugin) Update(conf config.Config) {
	conf.Unmarshal(&opt.Config)
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
			opt.handle(patten, http.HandlerFunc(handler))
		}
	}
}

func (opt *Plugin) settingPath() string {
	return filepath.Join(settingDir, strings.ToLower(opt.Name)+".yaml")
}

func (opt *Plugin) Save() error {
	if opt.saveTimer == nil {
		var lock sync.Mutex
		opt.saveTimer = time.AfterFunc(time.Second, func() {
			lock.Lock()
			defer lock.Unlock()
			file, err := os.OpenFile(opt.settingPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
			if err == nil {
				defer file.Close()
				err = yaml.NewEncoder(file).Encode(opt.Modified)
			}
			if err == nil {
				opt.Info("config saved")
			}
		})
	} else {
		opt.saveTimer.Reset(time.Second)
	}
	return nil
}

func (opt *Plugin) Publish(streamPath string, pub IPublisher) error {
	opt.Info("publish", zap.String("path", streamPath))
	conf, ok := opt.Config.(config.PublishConfig)
	if !ok {
		conf = EngineConfig
	}
	pub.GetPublisher().Config = conf.GetPublishConfig()
	return pub.receive(streamPath, pub)
}

var ErrStreamNotExist = errors.New("stream not exist")

// SubscribeExist 订阅已经存在的流
func (opt *Plugin) SubscribeExist(streamPath string, sub ISubscriber) error {
	opt.Info("subscribe exsit", zap.String("path", streamPath))
	path, _, _ := strings.Cut(streamPath, "?")
	if !Streams.Has(path) {
		opt.Warn("stream not exist", zap.String("path", streamPath))
		return ErrStreamNotExist
	}
	conf, ok := opt.Config.(config.SubscribeConfig)
	if !ok {
		conf = EngineConfig
	}
	sub.GetSubscriber().Config = conf.GetSubscribeConfig()
	return sub.receive(streamPath, sub)
}

// Subscribe 订阅一个流，如果流不存在则创建一个等待流
func (opt *Plugin) Subscribe(streamPath string, sub ISubscriber) error {
	opt.Info("subscribe", zap.String("path", streamPath))
	conf, ok := opt.Config.(config.SubscribeConfig)
	if !ok {
		conf = EngineConfig
	}
	sub.GetSubscriber().Config = conf.GetSubscribeConfig()
	return sub.receive(streamPath, sub)
}

// SubscribeBlock 阻塞订阅一个流，直到订阅结束
func (opt *Plugin) SubscribeBlock(streamPath string, sub ISubscriber, t byte) (err error) {
	if err = opt.Subscribe(streamPath, sub); err == nil {
		sub.PlayBlock(t)
	}
	return
}

var ErrNoPullConfig = errors.New("no pull config")
var Pullers sync.Map

func (opt *Plugin) Pull(streamPath string, url string, puller IPuller, save int) (err error) {
	zurl := zap.String("url", url)
	zpath := zap.String("path", streamPath)
	opt.Info("pull", zpath, zurl)
	defer func() {
		if err != nil {
			opt.Error("pull failed", zurl, zap.Error(err))
		}
	}()
	conf, ok := opt.Config.(config.PullConfig)
	if !ok {
		return ErrNoPullConfig
	}
	pullConf := conf.GetPullConfig()

	puller.init(streamPath, url, pullConf)
	puller.SetLogger(opt.Logger.With(zpath, zurl))
	go func() {
		Pullers.Store(puller, url)
		defer Pullers.Delete(puller)
		for opt.Info("start pull", zurl); puller.Reconnect(); opt.Warn("restart pull", zurl) {
			if err = puller.Connect(); err != nil {
				if err == io.EOF {
					puller.GetPublisher().Stream.Close()
					opt.Info("pull complete", zurl)
					return
				}
				opt.Error("pull connect", zurl, zap.Error(err))
				time.Sleep(time.Second * 5)
			} else {
				if err = opt.Publish(streamPath, puller); err != nil {
					if stream := Streams.Get(streamPath); stream != nil {
						if stream.Publisher != puller && stream.Publisher != nil {
							io := stream.Publisher.GetPublisher()
							opt.Error("puller is not publisher", zap.String("ID", io.ID), zap.String("Type", io.Type), zap.Error(err))
							return
						} else {
							opt.Warn("pull publish", zurl, zap.Error(err))
						}
					} else {
						opt.Error("pull publish", zurl, zap.Error(err))
						return
					}
				}
				if err = puller.Pull(); err != nil && !puller.IsShutdown() {
					opt.Error("pull", zurl, zap.Error(err))
				}
			}
			if puller.IsShutdown() {
				opt.Info("stop pull shutdown", zurl)
				return
			}
		}
		opt.Warn("stop pull stop reconnect", zurl)
	}()
	switch save {
	case 1:
		pullConf.AddPullOnStart(streamPath, url)
	case 2:
		pullConf.AddPullOnSub(streamPath, url)
	}
	if save > 0 {
		if opt.Modified == nil {
			opt.Modified = make(config.Config)
		}
		opt.Modified["pull"] = config.Struct2Config(pullConf)
		if err = opt.Save(); err != nil {
			opt.Error("save faild", zap.Error(err))
		}
	}
	return
}

var ErrNoPushConfig = errors.New("no push config")
var Pushers sync.Map

func (opt *Plugin) Push(streamPath string, url string, pusher IPusher, save bool) (err error) {
	zp, zu := zap.String("path", streamPath), zap.String("url", url)
	opt.Info("push", zp, zu)
	defer func() {
		if err != nil {
			opt.Error("push faild", zap.Error(err))
		}
	}()
	conf, ok := opt.Config.(config.PushConfig)
	if !ok {
		return ErrNoPushConfig
	}
	pushConfig := conf.GetPushConfig()

	pusher.init(streamPath, url, pushConfig)

	go func() {
		Pushers.Store(url, pusher)
		defer Pushers.Delete(url)
		for opt.Info("start push", zp, zu); pusher.Reconnect(); opt.Warn("restart push", zp, zu) {
			if err = opt.Subscribe(streamPath, pusher); err != nil {
				opt.Error("push subscribe", zp, zu, zap.Error(err))
				time.Sleep(time.Second * 5)
			} else {
				stream := pusher.GetSubscriber().Stream
				if err = pusher.Connect(); err != nil {
					if err == io.EOF {
						opt.Info("push complete", zp, zu)
						return
					}
					opt.Error("push connect", zp, zu, zap.Error(err))
					time.Sleep(time.Second * 5)
					stream.Receive(pusher) // 通知stream移除订阅者
				} else if err = pusher.Push(); err != nil && !stream.IsClosed() {
					opt.Error("push", zp, zu, zap.Error(err))
					pusher.Stop()
				}
				if stream.IsClosed() {
					opt.Info("stop push closed", zp, zu)
					return
				}
			}
		}
		opt.Warn("stop push stop reconnect", zp, zu)
	}()

	if save {
		pushConfig.AddPush(url, streamPath)
		if opt.Modified == nil {
			opt.Modified = make(config.Config)
		}
		opt.Modified["push"] = config.Struct2Config(pushConfig)
		if err = opt.Save(); err != nil {
			opt.Error("save faild", zap.Error(err))
		}
	}
	return
}
