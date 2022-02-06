package util

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

type TCPListener interface {
	context.Context
	Process(*net.TCPConn)
}

// ListenAddrs Listen http and https
func ListenAddrs(addr, addTLS, cert, key string, handler http.Handler) {
	var g errgroup.Group
	if addTLS != "" {
		g.Go(func() error {
			return http.ListenAndServeTLS(addTLS, cert, key, handler)
		})
	}
	if addr != "" {
		g.Go(func() error { return http.ListenAndServe(addr, handler) })
	}
	if err := g.Wait(); err != nil {
		log.Fatal(err)
	}
}

func ListenTCP(addr string, process TCPListener) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-process.Done()
		l.Close()
	}()
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
				Printf("%s: Accept error: %v; retrying in %v", addr, err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return err
		}
		conn.(*net.TCPConn).SetNoDelay(false)
		tempDelay = 0
		go process.Process(conn.(*net.TCPConn))
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
