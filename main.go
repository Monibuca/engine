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
	"runtime"
	"strings"
	"time" // colorable

	"github.com/Monibuca/utils/v3"
	"github.com/google/uuid"

	"github.com/Monibuca/engine/v3/util"

	"github.com/BurntSushi/toml"
	. "github.com/logrusorgru/aurora"
)

var Version = "3.2.2"

var (
	config = &struct {
		EnableAudio    bool
		EnableVideo    bool
		PublishTimeout time.Duration
		MaxRingSize    int
		AutoCloseAfter int
		RTPReorder     bool
	}{true, true, 60, 256, -1, false}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw     []byte
	StartTime     time.Time                        //启动时间
	Plugins       = make(map[string]*PluginConfig) // Plugins 所有的插件配置
	HasTranscoder bool
	Ctx           context.Context
	settingDir    string
)

//PluginConfig 插件配置定义
type PluginConfig struct {
	Name      string                       //插件名称
	Config    interface{}                  //插件配置
	Version   string                       //插件版本
	Dir       string                       //插件代码路径
	Run       func()                       //插件启动函数
	HotConfig map[string]func(interface{}) //热修改配置
}

func (opt *PluginConfig) Install(run func()) {
	opt.Run = run
	InstallPlugin(opt)
}

// InstallPlugin 安装插件
func InstallPlugin(opt *PluginConfig) {
	Plugins[opt.Name] = opt
	_, pluginFilePath, _, _ := runtime.Caller(1)
	opt.Dir = filepath.Dir(pluginFilePath)
	if parts := strings.Split(opt.Dir, "@"); len(parts) > 1 {
		opt.Version = parts[len(parts)-1]
	}
	utils.Print(Green("install plugin"), BrightCyan(opt.Name), BrightBlue(opt.Version))
}

func init() {
	if parts := strings.Split(utils.CurrentDir(), "@"); len(parts) > 1 {
		Version = parts[len(parts)-1]
	}
}

// Run 启动Monibuca引擎
func Run(ctx context.Context, configFile string) (err error) {
	Ctx = ctx
	if err := util.CreateShutdownScript(); err != nil {
		utils.Print(Red("create shutdown script error:"), err)
	}
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		utils.Print(Red("read config file error:"), err)
		return
	}
	settingDir = filepath.Join(filepath.Dir(configFile), ".m7s")
	if err = os.MkdirAll(settingDir, 0755); err != nil {
		utils.Print(Red("create dir .m7s error:"), err)
		return
	}
	utils.Print(BgGreen(Black("Ⓜ starting m7s ")), BrightBlue(Version))
	var cg map[string]interface{}
	if _, err = toml.Decode(string(ConfigRaw), &cg); err == nil {
		if cfg, ok := cg["Engine"]; ok {
			b, _ := json.Marshal(cfg)
			if err = json.Unmarshal(b, config); err != nil {
				log.Println(err)
			}
			config.PublishTimeout *= time.Second
		}
		for name, config := range Plugins {
			if cfg, ok := cg[name]; ok {
				config.updateSettings(cfg.(map[string]interface{}))
				b, _ := json.Marshal(cfg)
				if err = json.Unmarshal(b, config.Config); err != nil {
					log.Println(err)
					continue
				}
			} else if config.Config != nil {
				continue
			}
			if config.Run != nil {
				go config.Run()
			}
		}
	} else {
		utils.Print(Red("decode config file error:"), err)
	}
	UUID := uuid.NewString()
	reportTimer := time.NewTimer(time.Minute)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://monibuca.com:2022/report/engine", nil)
	req.Header.Set("os", runtime.GOOS)
	req.Header.Set("version", Version)
	req.Header.Set("uuid", UUID)
	var c http.Client
	for {
		req.Header.Set("streams", fmt.Sprintf("%d", len(Streams.m)))
		c.Do(req)
		select {
		case <-ctx.Done():
			return
		case <-reportTimer.C:
		}
	}
}
func objectAssign(target, source map[string]interface{}) {
	for k, v := range source {
		if _, ok := target[k]; !ok {
			target[k] = v
		} else {
			switch v := v.(type) {
			case map[string]interface{}:
				objectAssign(target[k].(map[string]interface{}), v)
			default:
				target[k] = v
			}
		}
	}
}
func (opt *PluginConfig) updateSettings(cfg map[string]interface{}) {
	if setting, err := ioutil.ReadFile(opt.settingPath()); err == nil {
		var cg map[string]interface{}
		if _, err = toml.Decode(string(setting), &cg); err == nil {
			objectAssign(cfg, cg)
		}
	}
}
func (opt *PluginConfig) settingPath() string {
	return filepath.Join(settingDir, opt.Name+".toml")
}
func (opt *PluginConfig) Save() error {
	file, err := os.OpenFile(opt.settingPath(), os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer file.Close()
		err = toml.NewEncoder(file).Encode(opt.Config)
	}
	return err
}
