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

	conn, err := quic.DialAddr(cfg.Server, tlsConf, nil)
	if err != nil {
		log.Error("connect to console server ", cfg.Server, err)
		return err
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		log.Error("OpenStreamSync on console server ", cfg.Server, err)
		return err
	}

	_, err = stream.Write([]byte(cfg.Secret + "\n"))
	if err != nil {
		log.Error("Write Secret to console server ", cfg.Server, err)
		return err
	}
	var rMessage map[string]interface{}

	msg, err := io.ReadAll(stream)
	if err != nil {
		log.Error("read response console server ", cfg.Server, err)
		return err
	}
	if err = json.Unmarshal(msg, &rMessage); err != nil {
		log.Error("read response json console server ", cfg.Server, err)
		return err
	}
	if rMessage["code"].(float64) != 0 {
		log.Error("response from console server ", cfg.Server, " ", rMessage["msg"])
		return nil
	} else {
		log.Info("response from console server ", cfg.Server, " success")
	}
	for {
		s, err := conn.AcceptStream(ctx)
		if err == nil {
			go cfg.ReceiveRequest(s)
		} else {
			return err
		}
	}
}

func (cfg *Engine) ReceiveRequest(s quic.Stream) error {
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
				return s.Close()
			}
		} else {
			err = errors.New("theres no \\r")
		}
	}
	log.Error("read console server error:", err)
	return err
}
