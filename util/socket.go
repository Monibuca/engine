package util

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"
)

type TCPListener interface {
	context.Context
	Process(*net.TCPConn)
}

func GetJsonHandler[T any](fetch func() T, tickDur time.Duration) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("json") != "" {
			if err := json.NewEncoder(rw).Encode(fetch()); err != nil {
				rw.WriteHeader(500)
			}
			return
		}
		sse := NewSSE(rw, r.Context())
		tick := time.NewTicker(tickDur)
		for range tick.C {
			if sse.WriteJSON(fetch()) != nil {
				tick.Stop()
				break
			}
		}
	}
}



func ListenUDP(address string, networkBuffer int) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		log.Fatalf("udp server ResolveUDPAddr :%s error, %v", address, err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("udp server ListenUDP :%s error, %v", address, err)
	}
	if err = conn.SetReadBuffer(networkBuffer); err != nil {
		Printf("udp server video conn set read buffer error, %v", err)
	}
	if err = conn.SetWriteBuffer(networkBuffer); err != nil {
		Printf("udp server video conn set write buffer error, %v", err)
	}
	return conn, err
}

func CORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin[0])
	}
}
