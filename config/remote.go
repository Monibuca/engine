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

	"github.com/lucas-clemente/quic-go"
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

func (cfg *Engine) Remote(ctx context.Context) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"monibuca"},
	}

	conn, err := quic.DialAddr(cfg.Server, tlsConf, &quic.Config{
		KeepAlivePeriod: time.Second * 10,
	})
	wasConnected := err == nil
	if stream := quic.Stream(nil); err == nil {
		if stream, err = conn.OpenStreamSync(ctx); err == nil {
			_, err = stream.Write([]byte(cfg.Secret + "\n"))
			if msg := []byte(nil); err == nil {
				if msg, err = io.ReadAll(stream); err == nil {
					var rMessage map[string]interface{}
					if err = json.Unmarshal(msg, &rMessage); err == nil {
						if rMessage["code"].(float64) != 0 {
							log.Error("response from console server ", cfg.Server, " ", rMessage["msg"])
							return nil
						} else {
							log.Info("response from console server ", cfg.Server, " success")
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

	if err != nil {
		if wasConnected {
			log.Error("connect to console server ", cfg.Server, " ", err)
		}
		if ctx.Err() == nil {
			go cfg.Remote(ctx)
		}
	}

	return err
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
