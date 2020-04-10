package engine

import (
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Monibuca/engine/util"
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
	Name    string      //插件名称
	Type    byte        //类型
	Config  interface{} //插件配置
	UI      string      //界面路径
	Version string      //插件版本
	Dir     string      //插件代码路径
	Run     func()      //插件启动函数
}

// InstallPlugin 安装插件
func InstallPlugin(opt *PluginConfig) {
	log.Printf("install plugin %s version: %s", opt.Name, opt.Version)
	Plugins[opt.Name] = opt
	_, pluginFilePath, _, _ := runtime.Caller(1)
	opt.Dir = filepath.Dir(pluginFilePath)
	ui := filepath.Join(opt.Dir, "ui", "dist", "plugin-"+strings.ToLower(opt.Name)+".min.js")
	if util.Exist(ui) {
		opt.UI = ui
	}
}

// ListenerConfig 带有监听地址端口的插件配置类型
type ListenerConfig struct {
	ListenAddr string
}

var config = &struct {
	EnableWaitRoom bool
}{true}
