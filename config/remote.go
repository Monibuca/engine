package config

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"m7s.live/engine/v4/log"
)

type myResponseWriter2 struct {
	quic.Stream
	myResponseWriter
}

type myResponseWriter3 struct {
	handshake bool
	myResponseWriter2
	quic.Connection
}

func (w *myResponseWriter3) Write(b []byte) (int, error) {
	if !w.handshake {
		w.handshake = true
		return len(b), nil
	}
	println(string(b))
	return w.Stream.Write(b)
}

func (w *myResponseWriter3) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return net.Conn(w), bufio.NewReadWriter(bufio.NewReader(w), bufio.NewWriter(w)), nil
}

func (cfg *Engine) WtRemote(ctx context.Context) {
	retryDelay := [...]int{2, 3, 5, 8, 13}
	for i := 0; ctx.Err() == nil; i++ {
		connected, err := cfg.Remote(ctx)
		if err == nil {
			//不需要重试了，服务器返回了错误
			return
		}
		if Global.LogLang == "zh" {
			log.Error("连接到控制台服务器", cfg.Server, "失败", err)
		} else {
			log.Error("connect to console server ", cfg.Server, " ", err)
		}
		if connected {
			i = 0
		} else if i >= 5 {
			i = 4
		}
		time.Sleep(time.Second * time.Duration(retryDelay[i]))
	}
}

func (cfg *Engine) Remote(ctx context.Context) (wasConnected bool, err error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"monibuca"},
	}

	conn, err := quic.DialAddr(ctx, cfg.Server, tlsConf, &quic.Config{
		KeepAlivePeriod: time.Second * 10,
		EnableDatagrams: true,
	})
	wasConnected = err == nil
	if stream := quic.Stream(nil); err == nil {
		if stream, err = conn.OpenStreamSync(ctx); err == nil {
			_, err = stream.Write(append([]byte{1}, (cfg.Secret + "\n")...))
			if msg := []byte(nil); err == nil {
				if msg, err = bufio.NewReader(stream).ReadSlice(0); err == nil {
					var rMessage map[string]any
					if err = json.Unmarshal(msg[:len(msg)-1], &rMessage); err == nil {
						if rMessage["code"].(float64) != 0 {
							if Global.LogLang == "zh" {
								log.Error("控制台服务器", cfg.Server, "返回错误", rMessage["msg"])
							} else {
								log.Error("response from console server ", cfg.Server, " ", rMessage["msg"])
							}
							return false, nil
						} else {
							cfg.reportStream = stream
							if Global.LogLang == "zh" {
								log.Info("连接到控制台服务器", cfg.Server, "成功", rMessage)
							} else {
								log.Info("response from console server ", cfg.Server, " success ", rMessage)
							}
							if v, ok := rMessage["enableReport"]; ok {
								cfg.enableReport = v.(bool)
							}
							if v, ok := rMessage["instanceId"]; ok {
								cfg.instanceId = v.(string)
							}
						}
					}
				}
			}
		}
	}

	for err == nil {
		var s quic.Stream
		if s, err = conn.AcceptStream(ctx); err == nil {
			go cfg.ReceiveRequest(s, conn)
		}
	}
	return wasConnected, err
}

func (cfg *Engine) ReceiveRequest(s quic.Stream, conn quic.Connection) error {
	defer s.Close()
	wr := &myResponseWriter2{Stream: s}
	reader := bufio.NewReader(s)
	var req *http.Request
	url, _, err := reader.ReadLine()
	if err == nil {
		ctx, cancel := context.WithCancel(s.Context())
		defer cancel()
		req, err = http.NewRequestWithContext(ctx, "GET", string(url), reader)
		for err == nil {
			var h []byte
			if h, _, err = reader.ReadLine(); len(h) > 0 {
				if b, a, f := strings.Cut(string(h), ": "); f {
					req.Header.Set(b, a)
				}
			} else {
				break
			}
		}

		if err == nil {
			h, _ := cfg.mux.Handler(req)
			if req.Header.Get("Accept") == "text/event-stream" {
				go h.ServeHTTP(wr, req)
			} else if req.Header.Get("Upgrade") == "websocket" {
				var writer myResponseWriter3
				writer.Stream = s
				writer.Connection = conn
				req.Host = req.Header.Get("Host")
				if req.Host == "" {
					req.Host = req.URL.Host
				}
				if req.Host == "" {
					req.Host = "m7s.live"
				}
				h.ServeHTTP(&writer, req) //建立websocket连接,握手
			} else {
				h.ServeHTTP(wr, req)
			}
		}
		io.ReadAll(s)
	}
	if err != nil {
		log.Error("read console server error:", err)
	}
	return err
}
