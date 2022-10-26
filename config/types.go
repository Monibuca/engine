package config

import (
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

type PublishConfig interface {
	GetPublishConfig() *Publish
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
	PubAudio          bool
	PubVideo          bool
	KickExist         bool // 是否踢掉已经存在的发布者
	PublishTimeout    int  // 发布无数据超时
	WaitCloseTimeout  int  // 延迟自动关闭（等待重连）
	DelayCloseTimeout int  // 延迟自动关闭（无订阅时）
}

func (c *Publish) GetPublishConfig() *Publish {
	return c
}

type Subscribe struct {
	SubAudio       bool
	SubVideo       bool
	SubAudioTracks []string // 指定订阅的音频轨道
	SubVideoTracks []string // 指定订阅的视频轨道
	LiveMode       bool     // 实时模式：追赶发布者进度，在播放首屏后等待发布者的下一个关键帧，然后调到该帧。
	IFrameOnly     bool     // 只要关键帧
	WaitTimeout    int      // 等待流超时
}

func (c *Subscribe) GetSubscribeConfig() *Subscribe {
	return c
}

type Pull struct {
	RePull          int               // 断开后自动重拉,0 表示不自动重拉，-1 表示无限重拉，高于0 的数代表最大重拉次数
	PullOnStart     bool              // 启动时拉流
	PullOnSubscribe bool              // 订阅时自动拉流
	PullList        map[string]string // 自动拉流列表，以streamPath为key，url为value
}

func (p *Pull) GetPullConfig() *Pull {
	return p
}

func (p *Pull) AddPull(streamPath string, url string) {
	if p.PullList == nil {
		p.PullList = make(map[string]string)
	}
	p.PullList[streamPath] = url
}

type Push struct {
	RePush   int               // 断开后自动重推,0 表示不自动重推，-1 表示无限重推，高于0 的数代表最大重推次数
	PushList map[string]string // 自动推流列表
}

func (p *Push) GetPushConfig() *Push {
	return p
}

func (p *Push) AddPush(url string, streamPath string) {
	if p.PushList == nil {
		p.PushList = make(map[string]string)
	}
	p.PushList[url] = streamPath
}

type Console struct {
	Server        string //远程控制台地址
	Secret        string //远程控制台密钥
	PublicAddr    string //公网地址，提供远程控制台访问的地址，不配置的话使用自动识别的地址
	PublicAddrTLS string
}

type Engine struct {
	Publish
	Subscribe
	HTTP
	RTPReorder bool
	EnableAVCC bool //启用AVCC格式，rtmp协议使用
	EnableRTP  bool //启用RTP格式，rtsp、gb18181等协议使用
	Console
	LogLevel            string
	RTPReorderBufferLen int //RTP重排序缓冲长度
	SpeedLimit          int //速度限制最大等待时间
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
	case context.Context:
		util.RTPReorderBufferLen = uint16(cfg.RTPReorderBufferLen)
		if strings.HasPrefix(cfg.Console.Server, "wss") {
			go cfg.WsRemote()
		} else {
			go cfg.Remote(v)
		}
	}
}

var Global = &Engine{
	Publish{true, true, false, 10, 0, 0},
	Subscribe{true, true, nil, nil, true, false, 10},
	HTTP{ListenAddr: ":8080", CORS: true, mux: http.DefaultServeMux},
	false, true, true, Console{
		"console.monibuca.com:4242", "", "", "",
	}, "info", 50, 0,
}
