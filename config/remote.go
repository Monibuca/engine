package config

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/lucas-clemente/quic-go"
	"m7s.live/engine/v4/log"
)

type myResponseWriter2 struct {
	quic.Stream
	myResponseWriter
}

func (cfg *Engine) Remote(ctx context.Context) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"monibuca"},
	}

	conn, err := quic.DialAddr(cfg.Server, tlsConf, &quic.Config{
		KeepAlive: true,
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
	r := bufio.NewReader(s)
	wr := &myResponseWriter2{Stream: s}
	str, err := r.ReadString('\n')
	var req *http.Request
	if err == nil {
		if b, a, f := strings.Cut(strings.TrimSuffix(str, "\n"), "\r"); f {
			if len(a) > 0 {
				req, err = http.NewRequest("POST", b, strings.NewReader(a))
			} else {
				req, err = http.NewRequest("GET", b, nil)
			}
			if err == nil {
				h, _ := cfg.mux.Handler(req)
				h.ServeHTTP(wr, req)
			}
		} else {
			err = errors.New("theres no \\r")
		}
	}
	log.Error("read console server error:", err)
	return err
}
