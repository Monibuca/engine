package engine

import (
	"context"
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

	"github.com/Monibuca/engine/v4/util"
	"github.com/google/uuid"

	. "github.com/logrusorgru/aurora"
	"gopkg.in/yaml.v3"
)

var Version = "4.0.0"

var (
	DefaultPublishConfig = PublishConfig{
		true, true, false, 10, 10,
	}
	DefaultSubscribeConfig = SubscribeConfig{
		true, true, false, 10,
	}
	config = &EngineConfig{
		http.NewServeMux(),
		Ctx,
		DefaultPublishConfig,
		DefaultSubscribeConfig,
		HTTPConfig{ListenAddr: ":8080", CORS: true},
		false, true, true, true,
	}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw  []byte
	StartTime  time.Time                  //启动时间
	Plugins    = make(map[string]*Plugin) // Plugins 所有的插件配置
	Ctx        context.Context
	settingDir string
)

func InstallPlugin(config PluginConfig) *Plugin {
	name := strings.TrimSuffix(reflect.TypeOf(config).Elem().Name(), "Config")
	plugin := &Plugin{
		Name:     name,
		Config:   config,
		Modified: make(Config),
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
	Name      string       //插件名称
	Config    PluginConfig //插件配置
	Version   string       //插件版本
	RawConfig Config       //配置的map形式方便查询
	Modified  Config       //修改过的配置项
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
	}
	settingDir = filepath.Join(filepath.Dir(configFile), ".m7s")
	if err = os.MkdirAll(settingDir, 0755); err != nil {
		log.Print(Red("create dir .m7s error:"), err)
		return
	}
	util.Print(BgGreen(Black("Ⓜ starting m7s ")), BrightBlue(Version))
	var cg Config
	var engineCg Config
	if ConfigRaw != nil {
		if err = yaml.Unmarshal(ConfigRaw, &cg); err == nil {
			if cfg, ok := cg["engine"]; ok {
				engineCg = cfg.(Config)
			}
		}
	}
	go config.Update(engineCg)
	for name, config := range Plugins {
		if v, ok := cg[strings.ToLower(name)]; ok {
			config.RawConfig = v.(Config)
		}
		config.merge()
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

func (opt *Plugin) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	var cors bool
	if v, ok := opt.RawConfig["cors"]; ok {
		cors = v.(bool)
	} else if config.HTTP.CORS {
		cors = true
	}
	config.HandleFunc("/"+strings.ToLower(opt.Name)+pattern, func(rw http.ResponseWriter, r *http.Request) {
		if cors {
			util.CORS(rw, r)
		}
		handler(rw, r)
	})
}

func (opt *Plugin) HandleApi(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	opt.HandleFunc("/api"+pattern, handler)
}

// 读取独立配置合并入总配置中
func (opt *Plugin) merge() {
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
	go opt.Config.Update(opt.RawConfig)
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
	return err
}
