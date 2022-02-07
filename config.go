package engine

import (
	"context"
	"log"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

type Second int

func (s Second) Duration() time.Duration {
	return time.Duration(s) * time.Second
}

type PluginConfig interface {
	Update(Config)
}

type TCPPluginConfig interface {
	PluginConfig
	context.Context
	ServeTCP(*net.TCPConn)
}

type HTTPPluginConfig interface {
	PluginConfig
	context.Context
	http.Handler
}

type Config map[string]any

func (config Config) Unmarshal(s any) {
	var el reflect.Value
	if v, ok := s.(reflect.Value); ok {
		el = v
	} else {
		el = reflect.ValueOf(s).Elem()
	}
	t := el.Type()
	for k, v := range config {
		var fv reflect.Value
		value := reflect.ValueOf(v)
		if f, ok := t.FieldByName(strings.ToUpper(k[:1]) + k[1:]); ok {
			// 兼容首字母大写的属性
			fv = el.FieldByName(f.Name)
		} else if f, ok := t.FieldByName(strings.ToUpper(k)); ok {
			// 兼容全部大写的属性
			fv = el.FieldByName(f.Name)
		} else {
			continue
		}
		if t.Kind() == reflect.Slice {
			l := value.Len()
			s := reflect.MakeSlice(t.Elem(), l, value.Cap())
			for i := 0; i < l; i++ {
				fv := value.Field(i)
				if fv.Type() == reflect.TypeOf(config) {
					fv.FieldByName("Unmarshal").Call([]reflect.Value{s.Field(i)})
				} else {
					s.Field(i).Set(fv)
				}
			}
			fv.Set(s)
		} else if child, ok := v.(Config); ok {
			child.Unmarshal(fv)
		} else {
			fv.Set(value)
		}
	}
}

func (config Config) Assign(source Config) {
	for k, v := range source {
		m, isMap := v.(map[string]any)
		if _, ok := config[k]; !ok || !isMap {
			config[k] = v
		} else {
			Config(config[k].(map[string]any)).Assign(m)
		}
	}
}

func (config Config) Has(key string) (ok bool) {
	if config == nil {
		return
	}
	_, ok = config[key]
	return
}

type TCPConfig struct {
	ListenAddr string
	ListenNum  int //同时并行监听数量，0为CPU核心数量
}

func (tcp *TCPConfig) listen(l net.Listener, handler func(*net.TCPConn)) {
	var tempDelay time.Duration
	for {
		conn, err := l.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Printf("%s: Accept error: %v; retrying in %v", tcp.ListenAddr, err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return
		}
		conn.(*net.TCPConn).SetNoDelay(false)
		tempDelay = 0
		go handler(conn.(*net.TCPConn))
	}
}
func (tcp *TCPConfig) Listen(plugin TCPPluginConfig) error {
	l, err := net.Listen("tcp", tcp.ListenAddr)
	if err != nil {
		return err
	}
	count := tcp.ListenNum
	if count == 0 {
		count = runtime.NumCPU()
	}
	for i := 0; i < count; i++ {
		go tcp.listen(l, plugin.ServeTCP)
	}
	<-plugin.Done()
	return l.Close()
}

type HTTPConfig struct {
	ListenAddr    string
	ListenAddrTLS string
	CertFile      string
	KeyFile       string
	CORS          bool //是否自动添加CORS头
}

// ListenAddrs Listen http and https
func (config *HTTPConfig) Listen(plugin HTTPPluginConfig) error {
	var g errgroup.Group
	if config.ListenAddrTLS != "" {
		g.Go(func() error {
			return http.ListenAndServeTLS(config.ListenAddrTLS, config.CertFile, config.KeyFile, plugin)
		})
	}
	if config.ListenAddr != "" {
		g.Go(func() error { return http.ListenAndServe(config.ListenAddr, plugin) })
	}
	g.Go(func() error {
		<-plugin.Done()
		return plugin.Err()
	})
	return g.Wait()
}

type PublishConfig struct {
	PubAudio         bool
	PubVideo         bool
	KillExit         bool   // 是否踢掉已经存在的发布者
	PublishTimeout   Second // 发布无数据超时
	WaitCloseTimeout Second // 延迟自动关闭（无订阅时）
}

type SubscribeConfig struct {
	SubAudio    bool
	SubVideo    bool
	IFrameOnly  bool   // 只要关键帧
	WaitTimeout Second // 等待流超时
}

type PullConfig struct {
	AutoReconnect   bool              // 自动重连
	PullOnStart     bool              // 启动时拉流
	PullOnSubscribe bool              // 订阅时自动拉流
	AutoPullList    map[string]string // 自动拉流列表
}

type PushConfig struct {
	AutoPushList map[string]string // 自动推流列表
}

type EngineConfig struct {
	*http.ServeMux
	context.Context
	Publish    PublishConfig
	Subscribe  SubscribeConfig
	HTTP       HTTPConfig
	RTPReorder bool
	EnableAVCC bool //启用AVCC格式，rtmp协议使用
	EnableRTP  bool //启用RTP格式，rtsp、gb18181等协议使用
	EnableFLV  bool //开启FLV格式，hdl协议使用
}
