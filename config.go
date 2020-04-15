package engine

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Monibuca/engine/util"
	. "github.com/logrusorgru/aurora"
)

const (
	PLUGIN_NONE       = 0      //独立插件
	PLUGIN_SUBSCRIBER = 1      //订阅者插件
	PLUGIN_PUBLISHER  = 1 << 1 //发布者插件
	PLUGIN_HOOK       = 1 << 2 //钩子插件
)

// Plugins 所有的插件配置
var Plugins = make(map[string]*PluginConfig)

//PluginConfig 插件配置定义
type PluginConfig struct {
	Name   string      //插件名称
	Type   byte        //类型
	Config interface{} //插件配置
	UIDir  string      //界面目录
	// Deprecated: 界面文件，兼容老版本
	UI      string
	Version string //插件版本
	Dir     string //插件代码路径
	Run     func() //插件启动函数
}

// InstallPlugin 安装插件
func InstallPlugin(opt *PluginConfig) {
	Plugins[opt.Name] = opt
	_, pluginFilePath, _, _ := runtime.Caller(1)
	opt.Dir = filepath.Dir(pluginFilePath)
	ui := filepath.Join(opt.Dir, "ui", "dist")
	if util.Exist(ui) {
		opt.UIDir = ui
	}
	if parts := strings.Split(opt.Dir, "@"); len(parts) > 1 {
		opt.Version = parts[len(parts)-1]
	}
	Print(Green("install plugin"), BrightCyan(opt.Name), BrightBlue(opt.Version))
}

// ListenerConfig 带有监听地址端口的插件配置类型
type ListenerConfig struct {
	ListenAddr string
}

var config = &struct {
	EnableWaitRoom bool
}{true}
