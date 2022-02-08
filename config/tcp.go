package config

import (
	"context"
	"log"
	"net"
	"runtime"
	"time"
)

type TCP struct {
	ListenAddr string
	ListenNum  int //同时并行监听数量，0为CPU核心数量
}

func (tcp *TCP) listen(l net.Listener, handler func(*net.TCPConn)) {
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
func (tcp *TCP) Listen(ctx context.Context, plugin TCPPlugin) error {
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
	<-ctx.Done()
	return l.Close()
}
