package engine

import (
	"bytes"
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
	"github.com/Monibuca/engine/v4/log"
	"github.com/Monibuca/engine/v4/util"
	"github.com/google/uuid"
	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
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
	PushOnPublishList            = make(map[string][]PushOnPublish)             //发布时自动推流配置
	EventBus                     = make(chan any)
)

type PushOnPublish struct {
	config.Plugin
	Pusher
}

func (p PushOnPublish) Push() {
	p.OnEvent(p.Pusher)
}

type PullOnSubscribe struct {
	config.Plugin
	Puller
}

func (p PullOnSubscribe) Pull() {
	p.OnEvent(p.Puller)
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
	Engine.Logger = log.With(zap.Bool("engine", true))
	Engine.registerHandler()
	// 使得RawConfig具备全量配置信息，用于合并到插件配置中
	Engine.RawConfig = config.Struct2Config(EngineConfig.Engine)
	go EngineConfig.OnEvent(FirstConfig(Engine.RawConfig))
	for name, plugin := range Plugins {
		plugin.RawConfig = cg.GetChild(name)
		plugin.assign()
	}
	UUID := uuid.NewString()
	reportTimer := time.NewTimer(time.Minute)
	contentBuf := bytes.NewBuffer(nil)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://logs-01.loggly.com/inputs/758a662d-f630-40cb-95ed-2502a5e9c872/tag/monibuca/", nil)
	req.Header.Set("Content-Type", "application/json")

	content := fmt.Sprintf(`{"uuid":"%s","version":"%s","os":"%s","arch":"%s"`, UUID, Engine.Version, runtime.GOOS, runtime.GOARCH)
	var c http.Client
	for {
		contentBuf.Reset()
		postJson := fmt.Sprintf(`%s,"streams":%d}`, content, len(Streams.Map))
		contentBuf.WriteString(postJson)
		req.Body = ioutil.NopCloser(contentBuf)
		c.Do(req)
		select {
		case event := <-EventBus:
			for _, plugin := range Plugins {
				plugin.Config.OnEvent(event)
			}
		case <-ctx.Done():
			return
		case <-reportTimer.C:
		}
	}
}
