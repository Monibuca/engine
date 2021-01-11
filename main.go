package engine

import (
	"encoding/json"
	"github.com/Monibuca/engine/v3/util"
	"io/ioutil"
	"log"
	"time" // colorable

	"github.com/BurntSushi/toml"
	. "github.com/logrusorgru/aurora"
)

var (
	config = &struct {
		EnableWaitStream bool
		EnableAudio      bool
		EnableVideo      bool
		PublishTimeout   time.Duration
	}{true, true, true, time.Minute}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw []byte
	StartTime time.Time //启动时间
)

// Run 启动Monibuca引擎
func Run(configFile string) (err error) {
	err = util.CreateShutdownScript()
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		Print(Red("read config file error:"), err)
		return
	}
	Print(BgGreen(Black("Ⓜ starting monibuca ")), BrightBlue(Version))
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
		Print(Red("decode config file error:"), err)
	}
	return
}
