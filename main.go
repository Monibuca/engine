package engine

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	// ConfigRaw 配置信息的原始数据
	ConfigRaw []byte
	// Version 引擎版本号
	Version = "1.1.3"
	// EngineInfo 引擎信息
	EngineInfo = &struct {
		Version   string
		StartTime time.Time //启动时间
	}{Version, time.Now()}
)

// Run 启动Monibuca引擎
func Run(configFile string) (err error) {
	if runtime.GOOS == "windows" {
		ioutil.WriteFile("shutdown.bat", []byte(fmt.Sprintf("taskkill /pid %d  -t  -f", os.Getpid())), 0777)
	} else {
		ioutil.WriteFile("shutdown.sh", []byte(fmt.Sprintf("kill -9 %d", os.Getpid())), 0777)
	}
	log.Printf("start monibuca version:%s", Version)
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		log.Printf("read config file error: %v", err)
		return
	}
	go Summary.StartSummary()
	var cg map[string]interface{}
	if _, err = toml.Decode(string(ConfigRaw), &cg); err == nil {
		if cfg, ok := cg["Monibuca"]; ok {
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
		log.Printf("decode config file error: %v", err)
	}
	return
}
