package engine

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Monibuca/engine/v2/util"
	. "github.com/logrusorgru/aurora"
)

// Plugins 所有的插件配置
var Plugins = make(map[string]*PluginConfig)

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
	Print(Green("install plugin"), BrightCyan(opt.Name), BrightBlue(opt.Version))
}