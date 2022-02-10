package config

type Publish struct {
	PubAudio         bool
	PubVideo         bool
	KickExsit         bool   // 是否踢掉已经存在的发布者
	PublishTimeout   Second // 发布无数据超时
	WaitCloseTimeout Second // 延迟自动关闭（无订阅时）
}

type Subscribe struct {
	SubAudio    bool
	SubVideo    bool
	IFrameOnly  bool   // 只要关键帧
	WaitTimeout Second // 等待流超时
}

type Pull struct {
	AutoReconnect   bool              // 自动重连
	PullOnStart     bool              // 启动时拉流
	PullOnSubscribe bool              // 订阅时自动拉流
	AutoPullList    map[string]string // 自动拉流列表
}

type Push struct {
	AutoPushList map[string]string // 自动推流列表
}

type Engine struct {
	Publish
	Subscribe
	HTTP
	RTPReorder bool
	EnableAVCC bool //启用AVCC格式，rtmp协议使用
	EnableRTP  bool //启用RTP格式，rtsp、gb18181等协议使用
	EnableFLV  bool //开启FLV格式，hdl协议使用
}

var Global = &Engine{
	Publish{true, true, false, 10, 10},
	Subscribe{true, true, false, 10},
	HTTP{ListenAddr: ":8080", CORS: true},
	false, true, true, true,
}
