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

	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/util"
	"github.com/google/uuid"

	. "github.com/logrusorgru/aurora"
	"gopkg.in/yaml.v3"
)

var (
	// ConfigRaw 配置信息的原始数据
	ConfigRaw    []byte
	StartTime    time.Time                  //启动时间
	Plugins      = make(map[string]*Plugin) // Plugins 所有的插件配置
	settingDir   string
	EngineConfig = &GlobalConfig{
		Engine:   config.Global,
		ServeMux: http.NewServeMux(),
	}
	Engine = InstallPlugin(EngineConfig)
)

func InstallPlugin[T config.Plugin](config T) *Plugin {
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
	Plugins[name] = plugin
	log.Print(Green("install plugin"), BrightCyan(name), BrightBlue(plugin.Version))
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
}

// Run 启动Monibuca引擎
func Run(ctx context.Context, configFile string) (err error) {
	Engine.Context = ctx
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
	util.Print(BgGreen(White("Ⓜ starting m7s ")))
	var cg config.Config
	if ConfigRaw != nil {
		if err = yaml.Unmarshal(ConfigRaw, &cg); err == nil {
			Engine.RawConfig = cg.GetChild("global")
		}
	}
	Engine.registerHandler()
	go EngineConfig.Update(Engine.RawConfig)
	for name, config := range Plugins {
		config.RawConfig = cg.GetChild(name)
		config.assign()
	}
	UUID := uuid.NewString()
	reportTimer := time.NewTimer(time.Minute)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://monibuca.com:2022/report/engine", nil)
	req.Header.Set("os", runtime.GOOS)
	req.Header.Set("version", Engine.Version)
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
	if opt == nil {
		return
	}
	var cors bool
	if v, ok := opt.RawConfig["cors"]; ok {
		cors = v.(bool)
	} else if EngineConfig.CORS {
		cors = true
	}
	if opt != Engine {
		pattern = "/" + strings.ToLower(opt.Name) + pattern
	}
	Engine.HandleFunc(pattern, func(rw http.ResponseWriter, r *http.Request) {
		if cors {
			util.CORS(rw, r)
		}
		handler(rw, r)
	})
}

func (opt *Plugin) HandleApi(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if opt == nil {
		return
	}
	pattern = "/api" + pattern
	util.Println("http handle added:", pattern)
	opt.HandleFunc(pattern, handler)
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
	for i, j := 0, t.NumField(); i < j; i++ {
		fname := t.Field(i).Name
		if Engine.RawConfig.Has(fname) {
			if !opt.RawConfig.Has(fname) {
				opt.RawConfig.Set(fname, Engine.RawConfig[fname])
			} else if opt.RawConfig.HasChild(fname) {
				opt.RawConfig.GetChild(fname).Merge(Engine.RawConfig.GetChild(fname))
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
	go opt.Config.Update(opt.RawConfig)
}

func (opt *Plugin) registerHandler() {
	t := reflect.TypeOf(opt.Config).Elem()
	v := reflect.ValueOf(opt.Config).Elem()
	// 注册http响应
	for i, j := 0, t.NumMethod(); i < j; i++ {
		mt := t.Method(i)
		if strings.HasPrefix(mt.Name, "API") {
			parts := strings.Split(mt.Name, "_")
			parts[0] = ""
			patten := reflect.ValueOf(strings.Join(parts, "/"))
			reflect.ValueOf(opt.HandleApi).Call([]reflect.Value{patten, v.Method(i)})
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
	return err
}
