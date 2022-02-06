package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time" // colorable

	"github.com/google/uuid"

	"github.com/Monibuca/engine/v4/util"

	"github.com/BurntSushi/toml"
	. "github.com/logrusorgru/aurora"
)

var Version = "4.0.0"

type Second int

func (s Second) Duration() time.Duration {
	return time.Duration(s) * time.Second
}

// StreamConfig 流的三级覆盖配置（全局，插件，流）

type PublishConfig struct {
	EnableAudio      bool
	EnableVideo      bool
	KillExit         bool   // 是否踢掉已经存在的发布者
	AutoReconnect    bool   // 自动重连
	PullOnStart      bool   // 启动时拉流
	PullOnSubscribe  bool   // 订阅时自动拉流
	PublishTimeout   Second // 发布无数据超时
	WaitCloseTimeout Second // 延迟自动关闭（无订阅时）
}

type SubscribeConfig struct {
	EnableAudio bool
	EnableVideo bool
	IFrameOnly  bool   // 只要关键帧
	WaitTimeout Second // 等待流超时
}

var (
	DefaultPublishConfig = PublishConfig{
		true, true, false, true, true, true, 10, 10,
	}
	DefaultSubscribeConfig = SubscribeConfig{
		true, true, false, 10,
	}
	config = &struct {
		Publish    PublishConfig
		Subscribe  SubscribeConfig
		RTPReorder bool
	}{DefaultPublishConfig, DefaultSubscribeConfig, false}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw  []byte
	StartTime  time.Time                  //启动时间
	Plugins    = make(map[string]*Plugin) // Plugins 所有的插件配置
	Ctx        context.Context
	settingDir string
)

type PluginConfig interface {
	Update(map[string]any)
}

func InstallPlugin(config PluginConfig) *Plugin {
	name := strings.TrimSuffix(reflect.TypeOf(config).Elem().Name(), "Config")
	plugin := &Plugin{
		Name:     name,
		Config:   config,
		Modified: make(map[string]any),
	}
	_, pluginFilePath, _, _ := runtime.Caller(1)
	configDir := filepath.Dir(pluginFilePath)
	if parts := strings.Split(configDir, "@"); len(parts) > 1 {
		plugin.Version = parts[len(parts)-1]
	}
	if _, ok := Plugins[name]; ok {
		return nil
	}
	Plugins[name] = plugin
	log.Print(Green("install plugin"), BrightCyan(name), BrightBlue(plugin.Version))
	return plugin
}

// Plugin 插件配置定义
type Plugin struct {
	Name     string         //插件名称
	Config   PluginConfig   //插件配置
	Version  string         //插件版本
	Modified map[string]any //修改过的配置项
}

func init() {
	if parts := strings.Split(util.CurrentDir(), "@"); len(parts) > 1 {
		Version = parts[len(parts)-1]
	}
}

// Run 启动Monibuca引擎
func Run(ctx context.Context, configFile string) (err error) {
	Ctx = ctx
	if err := util.CreateShutdownScript(); err != nil {
		log.Print(Red("create shutdown script error:"), err)
	}
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		log.Print(Red("read config file error:"), err)
		return
	}
	settingDir = filepath.Join(filepath.Dir(configFile), ".m7s")
	if err = os.MkdirAll(settingDir, 0755); err != nil {
		log.Print(Red("create dir .m7s error:"), err)
		return
	}
	util.Print(BgGreen(Black("Ⓜ starting m7s ")), BrightBlue(Version))
	var cg map[string]any
	if _, err = toml.Decode(string(ConfigRaw), &cg); err == nil {
		if cfg, ok := cg["Engine"]; ok {
			b, _ := json.Marshal(cfg)
			if err = json.Unmarshal(b, config); err != nil {
				log.Println(err)
			}
		}
	}
	for name, config := range Plugins {
		var cfg map[string]any
		if v, ok := cg[name]; ok {
			cfg = v.(map[string]any)
		}
		config.Update(cfg)
	}
	UUID := uuid.NewString()
	reportTimer := time.NewTimer(time.Minute)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://monibuca.com:2022/report/engine", nil)
	req.Header.Set("os", runtime.GOOS)
	req.Header.Set("version", Version)
	req.Header.Set("uuid", UUID)
	var c http.Client
	for {
		req.Header.Set("streams", fmt.Sprintf("%d", Streams.Len()))
		c.Do(req)
		select {
		case <-ctx.Done():
			return
		case <-reportTimer.C:
		}
	}
}
func objectAssign(target, source map[string]any) {
	for k, v := range source {
		if _, ok := target[k]; !ok {
			target[k] = v
		} else {
			switch v := v.(type) {
			case map[string]any:
				objectAssign(target[k].(map[string]any), v)
			default:
				target[k] = v
			}
		}
	}
}

// Update 更新配置
func (opt *Plugin) Update(cfg map[string]any) {
	if setting, err := ioutil.ReadFile(opt.settingPath()); err == nil {
		var cg map[string]interface{}
		if _, err = toml.Decode(string(setting), &cg); err == nil {
			if cfg == nil {
				cfg = cg
			} else {
				objectAssign(cfg, cg)
			}
		}
	}
	// TODO: map转成struct优化
	if cfg != nil {
		b, _ := json.Marshal(cfg)
		for k, v := range cfg {
			opt.Modified[k] = v
		}
		if err := json.Unmarshal(b, opt.Config); err != nil {
			log.Println(err)
		}
	}
	go opt.Config.Update(cfg)
}
func (opt *Plugin) settingPath() string {
	return filepath.Join(settingDir, opt.Name+".toml")
}

func (opt *Plugin) Save() error {
	file, err := os.OpenFile(opt.settingPath(), os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer file.Close()
		err = toml.NewEncoder(file).Encode(opt.Modified)
	}
	return err
}
