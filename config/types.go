package config

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"golang.org/x/net/websocket"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type PublishConfig interface {
	GetPublishConfig() Publish
}

type SubscribeConfig interface {
	GetSubscribeConfig() *Subscribe
}
type PullConfig interface {
	GetPullConfig() *Pull
}

type PushConfig interface {
	GetPushConfig() *Push
}

type Publish struct {
	PubAudio          bool          `default:"true"`
	PubVideo          bool          `default:"true"`
	InsertSEI         bool          // 是否启用SEI插入
	KickExist         bool          // 是否踢掉已经存在的发布者
	PublishTimeout    time.Duration `default:"10s"` // 发布无数据超时
	WaitCloseTimeout  time.Duration // 延迟自动关闭（等待重连）
	DelayCloseTimeout time.Duration // 延迟自动关闭（无订阅时）
	IdleTimeout       time.Duration // 空闲(无订阅)超时
	PauseTimeout      time.Duration `default:"30s"` // 暂停超时
	BufferTime        time.Duration // 缓冲长度(单位：秒)，0代表取最近关键帧
	SpeedLimit        time.Duration `default:"500ms"` //速度限制最大等待时间
	Key               string        // 发布鉴权key
	SecretArgName     string        `default:"secret"`   // 发布鉴权参数名
	ExpireArgName     string        `default:"expire"`   // 发布鉴权失效时间参数名
	RingSize          string        `default:"256-1024"` // 初始缓冲区大小
}

func (c Publish) GetPublishConfig() Publish {
	return c
}

type Subscribe struct {
	SubAudio        bool          `default:"true"`
	SubVideo        bool          `default:"true"`
	SubVideoArgName string        `default:"vts"`  // 指定订阅的视频轨道参数名
	SubAudioArgName string        `default:"ats"`  // 指定订阅的音频轨道参数名
	SubDataArgName  string        `default:"dts"`  // 指定订阅的数据轨道参数名
	SubModeArgName  string        `default:"mode"` // 指定订阅的模式参数名
	SubAudioTracks  []string      // 指定订阅的音频轨道
	SubVideoTracks  []string      // 指定订阅的视频轨道
	SubDataTracks   []string      // 指定订阅的数据轨道
	SubMode         int           // 0，实时模式：追赶发布者进度，在播放首屏后等待发布者的下一个关键帧，然后跳到该帧。1、首屏后不进行追赶。2、从缓冲最大的关键帧开始播放，也不追赶，需要发布者配置缓存长度
	SyncMode        int           // 0，采用时间戳同步，1，采用写入时间同步
	IFrameOnly      bool          // 只要关键帧
	WaitTimeout     time.Duration `default:"10s"` // 等待流超时
	WriteBufferSize int           `default:"0"`   // 写缓冲大小
	Key             string        // 订阅鉴权key
	SecretArgName   string        `default:"secret"` // 订阅鉴权参数名
	ExpireArgName   string        `default:"expire"` // 订阅鉴权失效时间参数名
	Internal        bool          `default:"false"`  // 是否内部订阅
}

func (c *Subscribe) GetSubscribeConfig() *Subscribe {
	return c
}

type Pull struct {
	RePull      int               // 断开后自动重拉,0 表示不自动重拉，-1 表示无限重拉，高于0 的数代表最大重拉次数
	PullOnStart map[string]string // 启动时拉流的列表
	PullOnSub   map[string]string // 订阅时自动拉流的列表
	Proxy       string            // 代理地址
}

func (p *Pull) GetPullConfig() *Pull {
	return p
}

func (p *Pull) AddPullOnStart(streamPath string, url string) {
	if p.PullOnStart == nil {
		p.PullOnStart = make(map[string]string)
	}
	p.PullOnStart[streamPath] = url
}

func (p *Pull) AddPullOnSub(streamPath string, url string) {
	if p.PullOnSub == nil {
		p.PullOnSub = make(map[string]string)
	}
	p.PullOnSub[streamPath] = url
}

type Push struct {
	RePush   int               // 断开后自动重推,0 表示不自动重推，-1 表示无限重推，高于0 的数代表最大重推次数
	PushList map[string]string // 自动推流列表
	Proxy    string            // 代理地址
}

func (p *Push) GetPushConfig() *Push {
	return p
}

func (p *Push) AddPush(url string, streamPath string) {
	if p.PushList == nil {
		p.PushList = make(map[string]string)
	}
	p.PushList[streamPath] = url
}

type Console struct {
	Server        string `default:"console.monibuca.com:44944"` //远程控制台地址
	Secret        string //远程控制台密钥
	PublicAddr    string //公网地址，提供远程控制台访问的地址，不配置的话使用自动识别的地址
	PublicAddrTLS string
}

type Engine struct {
	Publish
	Subscribe
	HTTP
	EnableAVCC     bool `default:"true"` //启用AVCC格式，rtmp、http-flv协议使用
	EnableRTP      bool `default:"true"` //启用RTP格式，rtsp、webrtc等协议使用
	EnableSubEvent bool `default:"true"` //启用订阅事件,禁用可以提高性能
	EnableAuth     bool `default:"true"` //启用鉴权
	Console
	LogLang             string        `default:"zh"`    //日志语言
	LogLevel            string        `default:"info"`  //日志级别
	RTPReorderBufferLen int           `default:"50"`    //RTP重排序缓冲长度
	EventBusSize        int           `default:"10"`    //事件总线大小
	PulseInterval       time.Duration `default:"5s"`    //心跳事件间隔
	DisableAll          bool          `default:"false"` //禁用所有插件
	PoolSize            int           //内存池大小
	enableReport        bool          `default:"false"` //启用报告,用于统计和监控
	reportStream        quic.Stream   // console server connection
	instanceId          string        // instance id 来自console
}

func (cfg *Engine) GetEnableReport() bool {
	return cfg.enableReport
}

func (cfg *Engine) GetInstanceId() string {
	return cfg.instanceId
}

var Global *Engine

func (cfg *Engine) InitDefaultHttp() {
	Global = cfg
	cfg.HTTP.mux = http.NewServeMux()
	cfg.HTTP.ListenAddrTLS = ":8443"
	cfg.HTTP.ListenAddr = ":8080"
}

type myResponseWriter struct {
}

func (*myResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (*myResponseWriter) WriteHeader(statusCode int) {
}
func (w *myResponseWriter) Flush() {
}

type myWsWriter struct {
	myResponseWriter
	*websocket.Conn
}

func (w *myWsWriter) Write(b []byte) (int, error) {
	return len(b), websocket.Message.Send(w.Conn, b)
}
func (cfg *Engine) WsRemote() {
	for {
		conn, err := websocket.Dial(cfg.Server, "", "https://console.monibuca.com")
		wr := &myWsWriter{Conn: conn}
		if err != nil {
			log.Error("connect to console server ", cfg.Server, " ", err)
			time.Sleep(time.Second * 5)
			continue
		}
		if err = websocket.Message.Send(conn, cfg.Secret); err != nil {
			time.Sleep(time.Second * 5)
			continue
		}
		var rMessage map[string]interface{}
		if err := websocket.JSON.Receive(conn, &rMessage); err == nil {
			if rMessage["code"].(float64) != 0 {
				log.Error("connect to console server ", cfg.Server, " ", rMessage["msg"])
				return
			} else {
				log.Info("connect to console server ", cfg.Server, " success")
			}
		}
		for {
			var msg string
			err := websocket.Message.Receive(conn, &msg)
			if err != nil {
				log.Error("read console server error:", err)
				break
			} else {
				b, a, f := strings.Cut(msg, "\n")
				if f {
					if len(a) > 0 {
						req, err := http.NewRequest("POST", b, strings.NewReader(a))
						if err != nil {
							log.Error("read console server error:", err)
							break
						}
						h, _ := cfg.mux.Handler(req)
						h.ServeHTTP(wr, req)
					} else {
						req, err := http.NewRequest("GET", b, nil)
						if err != nil {
							log.Error("read console server error:", err)
							break
						}
						h, _ := cfg.mux.Handler(req)
						h.ServeHTTP(wr, req)
					}
				} else {

				}
			}
		}
	}
}

func (cfg *Engine) OnEvent(event any) {
	switch v := event.(type) {
	case []byte:
		if cfg.reportStream != nil {
			cfg.reportStream.Write(v)
			cfg.reportStream.Write([]byte{0})
		}
	case context.Context:
		util.RTPReorderBufferLen = uint16(cfg.RTPReorderBufferLen)
		if strings.HasPrefix(cfg.Console.Server, "wss") {
			go cfg.WsRemote()
		} else {
			go cfg.WtRemote(v)
		}
	}
}
