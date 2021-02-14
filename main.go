package engine

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"time" // colorable

	"github.com/Monibuca/utils/v3"

	"github.com/Monibuca/engine/v3/util"

	"github.com/BurntSushi/toml"
	. "github.com/logrusorgru/aurora"
)

const Version = "3.0.1"

var (
	config = &struct {
		EnableAudio    bool
		EnableVideo    bool
		PublishTimeout time.Duration
	}{true, true, time.Minute}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw     []byte
	StartTime     time.Time                        //启动时间
	Plugins       = make(map[string]*PluginConfig) // Plugins 所有的插件配置
	HasTranscoder bool
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

// Run 启动Monibuca引擎
func Run(configFile string) (err error) {
	err = util.CreateShutdownScript()
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		utils.Print(Red("read config file error:"), err)
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
		}
		for name, config := range Plugins {
			if cfg, ok := cg[name]; ok {
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
	return
}
