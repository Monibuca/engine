package engine

import "log"

const (
	PLUGIN_SUBSCRIBER = 1
	PLUGIN_PUBLISHER  = 1 << 1
	PLUGIN_HOOK       = 1 << 2
)

var (
	Plugins = make(map[string]*PluginConfig)
)

type PluginConfig struct {
	Name   string      //插件名称
	Type   byte        //类型
	Config interface{} //插件配置
	UI     string      //界面路径
	Run    func()      //插件启动函数
}

type Config struct {
	Plugins map[string]interface{}
}

func InstallPlugin(opt *PluginConfig) {
	log.Printf("install plugin %s", opt.Name)
	Plugins[opt.Name] = opt
}

type ListenerConfig struct {
	ListenAddr string
}
