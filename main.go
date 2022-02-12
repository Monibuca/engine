package engine

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/Monibuca/engine/v4/config"
	"github.com/Monibuca/engine/v4/util"
	"github.com/google/uuid"
	. "github.com/logrusorgru/aurora"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	// ConfigRaw 配置信息的原始数据
	ConfigRaw    []byte
	StartTime    time.Time                  //启动时间
	Plugins      = make(map[string]*Plugin) // Plugins 所有的插件配置
	EngineConfig = &GlobalConfig{
		Engine:   config.Global,
		ServeMux: http.NewServeMux(),
	}
	settingDir                   string                                         //配置缓存目录，该目录按照插件名称作为文件名存储修改过的配置
	Engine                       = InstallPlugin(EngineConfig)                  //复用安装插件逻辑，将全局配置信息注入，并启动server
	toolManForGetHandlerFuncType http.HandlerFunc                               //专门用来获取HandlerFunc类型的工具人
	handlerFuncType              = reflect.TypeOf(toolManForGetHandlerFuncType) //供反射使用的Handler类型的类型
	MergeConfigs                 = []string{"Publish", "Subscribe"}             //需要合并配置的属性项，插件若没有配置则使用全局配置
	PullOnSubscribeList          = make(map[string]PullOnSubscribe)             //按需拉流的配置信息
)

type PullOnSubscribe struct {
	Plugin PullPlugin
	Puller
}

func (p PullOnSubscribe) Pull(streamPath string) {
	p.Plugin.PullStream(streamPath, p.Puller)
}

// Run 启动Monibuca引擎，传入总的Context，可用于关闭所有
func Run(ctx context.Context, configFile string) (err error) {
	Engine.Context = ctx
	if err := util.CreateShutdownScript(); err != nil {
		log.Error("create shutdown script error:", err)
	}
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		log.Error("read config file error:", err)
	}
	settingDir = filepath.Join(filepath.Dir(configFile), ".m7s")
	if err = os.MkdirAll(settingDir, 0755); err != nil {
		log.Error("create dir .m7s error:", err)
		return
	}
	log.Info(Blink("Ⓜ starting m7s v4"))
	var cg config.Config
	if ConfigRaw != nil {
		if err = yaml.Unmarshal(ConfigRaw, &cg); err == nil {
			Engine.RawConfig = cg.GetChild("global")
			//将配置信息同步到结构体
			Engine.RawConfig.Unmarshal(config.Global)
		}
	} else {
		log.Warn("no config file found , use default config values")
	}
	Engine.Entry = log.WithContext(Engine)
	Engine.registerHandler()
	go EngineConfig.Update(Engine.RawConfig)
	for name, plugin := range Plugins {
		plugin.RawConfig = cg.GetChild(name)
		plugin.assign()
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
