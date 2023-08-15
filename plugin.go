package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

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
	switch v := config.(type) {
	case *GlobalConfig:
		v.InitDefaultHttp()
	default:
		Plugins[name] = plugin
		plugins = append(plugins, plugin)
	}
	return plugin
}

type FirstConfig config.Config
type DefaultYaml string

// Plugin 插件信息
type Plugin struct {
	context.Context    `json:"-" yaml:"-"`
	context.CancelFunc `json:"-" yaml:"-"`
	Name               string        //插件名称
	Config             config.Plugin `json:"-" yaml:"-"` //类型化的插件配置
	Version            string        //插件版本
	Yaml               string        //配置文件中的配置项
	modifiedYaml       string        //修改过的配置的yaml文件内容
	RawConfig          config.Config //最终合并后的配置的map形式方便查询
	Modified           config.Config //修改过的配置项
	*log.Logger        `json:"-" yaml:"-"`
	saveTimer          *time.Timer //用于保存的时候的延迟，防抖
	Disabled           bool
}

func (opt *Plugin) logHandler(pattern string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		opt.Debug("visit", zap.String("path", r.URL.String()), zap.String("remote", r.RemoteAddr))
		name := strings.ToLower(opt.Name)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+name)
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
		opt.Debug("http handle added", zap.String("pattern", pattern))
		conf.Handle(pattern, opt.logHandler(pattern, handler))
	}
	if opt != Engine {
		pattern = "/" + strings.ToLower(opt.Name) + pattern
		opt.Debug("http handle added to engine", zap.String("pattern", pattern))
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
	if EngineConfig.DisableAll {
		opt.Disabled = true
	}
	if opt.RawConfig == nil {
		opt.RawConfig = config.Config{}
	} else if opt.RawConfig["enable"] == false {
		opt.Disabled = true
	} else if opt.RawConfig["enable"] == true {
		opt.Disabled = false
		//移除这个属性防止反序列化报错
		delete(opt.RawConfig, "enable")
	}
	if opt.Disabled {
		opt.Warn("plugin disabled")
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
	opt.RawConfig = config.Struct2Config(opt.Config, strings.ToUpper(opt.Name))
	// var buffer bytes.Buffer
	// err := yaml.NewEncoder(&buffer).Encode(opt.Config)
	// if err != nil {
	// 	panic(err)
	// }
	// err = yaml.NewDecoder(&buffer).Decode(&opt.RawConfig)
	// if err != nil {
	// 	panic(err)
	// }
	opt.Config.OnEvent(FirstConfig(opt.RawConfig))
	delete(opt.RawConfig, "defaultyaml")
	opt.Debug("config", zap.Any("config", opt.Config))
	// opt.RawConfig = config.Struct2Config(opt.Config)
	if conf, ok := opt.Config.(config.HTTPConfig); ok {
		go conf.Listen(opt)
	}
	if conf, ok := opt.Config.(config.TCPConfig); ok {
		go conf.ListenTCP(opt, opt.Config.(config.TCPPlugin))
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
		patten := "/"
		if name != "ServeHTTP" {
			patten = strings.ToLower(strings.ReplaceAll(name, "_", "/"))
		}
		switch handler := v.Method(i).Interface().(type) {
		case func(http.ResponseWriter, *http.Request):
			opt.handle(patten, http.HandlerFunc(handler))
			// case func(*http.Request) (int, string, any):
			// 	opt.handle(patten, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			// 		code, msg, data := handler(r)
			// 		switch returnMode {
			// 		case "json":
			// 			rw.Header().Set("Content-Type", "application/json")
			// 			rw.WriteHeader(http.StatusOK)
			// 			json.NewEncoder(rw).Encode(map[string]interface{}{
			// 				"code": code,
			// 				"msg":  msg,
			// 				"data": data,
			// 			})
			// 		default:
			// 			http.Error(rw, msg, code)
			// 		}
			// 	}))
		}
	}
}

func (opt *Plugin) settingPath() string {
	return filepath.Join(SettingDir, strings.ToLower(opt.Name)+".yaml")
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

func (opt *Plugin) AssignPubConfig(puber *Publisher) {
	if puber.Config == nil {
		conf, ok := opt.Config.(config.PublishConfig)
		if !ok {
			conf = EngineConfig
		}
		copyConfig := conf.GetPublishConfig()
		puber.Config = &copyConfig
	}
}

func (opt *Plugin) Publish(streamPath string, pub IPublisher) error {
	puber := pub.GetPublisher()
	if puber == nil {
		if EngineConfig.LogLang == "zh" {
			return errors.New("不是发布者")
		} else {
			return errors.New("not publisher")
		}
	}
	opt.AssignPubConfig(puber)
	return pub.Publish(streamPath, pub)
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
	return opt.Subscribe(streamPath, sub)
}
func (opt *Plugin) AssignSubConfig(suber  *Subscriber) {
	if suber.Config == nil {
		conf, ok := opt.Config.(config.SubscribeConfig)
		if !ok {
			conf = EngineConfig
		}
		copyConfig := *conf.GetSubscribeConfig()
		suber.Config = &copyConfig
	}
	if suber.ID == "" {
		suber.ID = fmt.Sprintf("%d", uintptr(unsafe.Pointer(suber)))
	}
}
// Subscribe 订阅一个流，如果流不存在则创建一个等待流
func (opt *Plugin) Subscribe(streamPath string, sub ISubscriber) error {
	suber := sub.GetSubscriber()
	if suber == nil {
		if EngineConfig.LogLang == "zh" {
			return errors.New("不是订阅者")
		} else {
			return errors.New("not subscriber")
		}
	}
	opt.AssignSubConfig(suber)
	return sub.Subscribe(streamPath, sub)
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
	conf, ok := opt.Config.(config.PullConfig)
	if !ok {
		return ErrNoPullConfig
	}
	pullConf := conf.GetPullConfig()
	if save < 2 {
		zurl := zap.String("url", url)
		zpath := zap.String("path", streamPath)
		opt.Info("pull", zpath, zurl)
		puller.init(streamPath, url, pullConf)
		opt.AssignPubConfig(puller.GetPublisher())
		puller.SetLogger(opt.Logger.With(zpath, zurl))
		go puller.startPull(puller)
	}
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
	pusher.SetLogger(opt.Logger.With(zp, zu))
	opt.AssignSubConfig(pusher.GetSubscriber())
	go pusher.startPush(pusher)
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
