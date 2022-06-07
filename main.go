package engine // import "m7s.live/engine/v4"

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/uuid"
	. "github.com/logrusorgru/aurora"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/config"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

var (
	ExecPath = os.Args[0]
	ExecDir  = filepath.Dir(ExecPath)
	// ConfigRaw 配置信息的原始数据
	ConfigRaw    []byte
	StartTime    time.Time                  //启动时间
	Plugins      = make(map[string]*Plugin) // Plugins 所有的插件配置
	EngineConfig = &GlobalConfig{
		Engine: config.Global,
	}
	settingDir   = filepath.Join(ExecDir, ".m7s")           //配置缓存目录，该目录按照插件名称作为文件名存储修改过的配置
	Engine       = InstallPlugin(EngineConfig)              //复用安装插件逻辑，将全局配置信息注入，并启动server
	MergeConfigs = []string{"Publish", "Subscribe", "HTTP"} //需要合并配置的属性项，插件若没有配置则使用全局配置
	EventBus     = make(chan any, 10)
	apiList      []string //注册到引擎的API接口列表
)

// Run 启动Monibuca引擎，传入总的Context，可用于关闭所有
func Run(ctx context.Context, configFile string) (err error) {
	StartTime = time.Now()
	Engine.Context = ctx
	if _, err := os.Stat(configFile); err != nil {
		configFile = filepath.Join(ExecDir, configFile)
	}
	if err := util.CreateShutdownScript(); err != nil {
		log.Error("create shutdown script error:", err)
	}
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		log.Warn("read config file error:", err.Error())
	}
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
		} else {
			log.Error("parsing yml error:", err)
		}
	}
	Engine.Logger = log.With(zap.Bool("engine", true))
	Engine.registerHandler()
	// 使得RawConfig具备全量配置信息，用于合并到插件配置中
	Engine.RawConfig = config.Struct2Config(EngineConfig.Engine)
	log.With(zap.String("config", "global")).Debug("", zap.Any("config", EngineConfig))
	go EngineConfig.Listen(Engine)
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
	if EngineConfig.Secret != "" {
		EngineConfig.OnEvent(ctx)
	}
	var c http.Client
	for {
		select {
		case event := <-EventBus:
			for _, plugin := range Plugins {
				if plugin.RawConfig["enable"] != false {
					plugin.Config.OnEvent(event)
				}
			}
		case <-ctx.Done():
			return
		case <-reportTimer.C:
			contentBuf.Reset()
			postJson := fmt.Sprintf(`%s,"streams":%d}`, content, len(Streams.Map))
			contentBuf.WriteString(postJson)
			req.Body = ioutil.NopCloser(contentBuf)
			c.Do(req)
		}
	}
}
