package config

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
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

func (w *myResponseWriter2) Flush() {

}

func (cfg *Engine) Remote(ctx context.Context) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"monibuca"},
	}

	conn, err := quic.DialAddr(cfg.Server, tlsConf, &quic.Config{
		KeepAlivePeriod: time.Second * 10,
	})

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
			go cfg.ReceiveRequest(s)
		} else if ctx.Err() == nil {
			go cfg.Remote(ctx)
		}
	}

	if err != nil {
		log.Error("connect to console server ", cfg.Server, err)
	}
	return err
}

func (cfg *Engine) ReceiveRequest(s quic.Stream) error {
	defer s.Close()
	wr := &myResponseWriter2{Stream: s}
	reader := bufio.NewReader(s)
	var req *http.Request
	url, _, err := reader.ReadLine()
	if err == nil {
		ctx, cancel := context.WithCancel(s.Context())
		defer cancel()
		req, err = http.NewRequestWithContext(ctx, "GET", string(url), s)
		for err == nil {
			var h []byte
			h, _, err = reader.ReadLine()
			if len(h) > 0 {
				b, a, f := strings.Cut(string(h), ": ")
				if f {
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
			} else {
				h.ServeHTTP(wr, req)
			}
			io.ReadAll(s)
		}
	}
	if err != nil {
		log.Error("read console server error:", err)
	}
	return err
}
