package config

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"m7s.live/engine/v4/log"
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
	PubAudio         bool
	PubVideo         bool
	KickExist        bool // 是否踢掉已经存在的发布者
	PublishTimeout   int  // 发布无数据超时
	WaitCloseTimeout int  // 延迟自动关闭（无订阅时）
}

func (c *Publish) GetPublishConfig() *Publish {
	return c
}

type Subscribe struct {
	SubAudio    bool
	SubVideo    bool
	IFrameOnly  bool // 只要关键帧
	WaitTimeout int  // 等待流超时
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

func (p *Push) AddPush(streamPath string, url string) {
	if p.PushList == nil {
		p.PushList = make(map[string]string)
	}
	p.PushList[streamPath] = url
}

type Engine struct {
	Publish
	Subscribe
	HTTP
	RTPReorder bool
	EnableAVCC bool   //启用AVCC格式，rtmp协议使用
	EnableRTP  bool   //启用RTP格式，rtsp、gb18181等协议使用
	EnableFLV  bool   //开启FLV格式，hdl协议使用
	ConsoleURL string //远程控制台地址
	Secret     string //远程控制台密钥
}
type myResponseWriter struct {
	io.Writer
}

func (w *myResponseWriter) Write(b []byte) (int, error) {
	return len(b), wsutil.WriteClientMessage(w, ws.OpBinary, b)
}

func (w *myResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (w *myResponseWriter) WriteHeader(statusCode int) {
}
func (cfg *Engine) OnEvent(event any) {
	switch v := event.(type) {
	case context.Context:
		go func() {
			for {
				conn, _, _, err := ws.Dial(v, cfg.ConsoleURL)
				wr := &myResponseWriter{conn}
				if err != nil {
					log.Error("connect to console server error:", err)
					time.Sleep(time.Second * 5)
					continue
				}
				err = wsutil.WriteClientMessage(conn, ws.OpText, []byte(`{"command":"init","secret":"123456"}`))
				if err != nil {
					time.Sleep(time.Second * 5)
					continue
				}
				for {
					msg, _, err := wsutil.ReadServerData(conn)
					if err != nil {
						log.Error("read console server error:", err)
						break
					} else {
						r := bufio.NewReader(bytes.NewReader(msg))
						URL, _ := r.ReadString('\n')
						req, err := http.NewRequest("GET", URL, r)
						if err != nil {
							log.Error("receive console request :", msg, err)
							break
						}
						h, _ := cfg.mux.Handler(req)
						h.ServeHTTP(wr, req)
					}
				}
			}
		}()
	}
}

var Global = &Engine{
	Publish{true, true, false, 10, 0},
	Subscribe{true, true, false, 10},
	HTTP{ListenAddr: ":8080", CORS: true, mux: http.DefaultServeMux},
	false, true, true, true, "wss://console.monibuca.com", "",
}
