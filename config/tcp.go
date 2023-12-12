package config

import (
	"context"
	"crypto/tls"
	_ "embed"
	"net"
	"runtime"
	"time"

	"m7s.live/engine/v4/log"
)

//go:embed local.monibuca.com_bundle.pem
var LocalCert []byte

//go:embed local.monibuca.com.key
var LocalKey []byte

var _ TCPConfig = (*TCP)(nil)

type TCPConfig interface {
	ListenTCP(context.Context, TCPPlugin) error
}

type TCP struct {
	ListenAddr    string `desc:"监听地址，格式为ip:port，ip 可省略默认监听所有网卡"`
	ListenAddrTLS string `desc:"监听地址，格式为ip:port，ip 可省略默认监听所有网卡"`
	CertFile      string `desc:"证书文件"`
	KeyFile       string `desc:"私钥文件"`
	ListenNum     int    `desc:"同时并行监听数量，0为CPU核心数量"` //同时并行监听数量，0为CPU核心数量
	NoDelay       bool   `desc:"是否禁用Nagle算法"`        //是否禁用Nagle算法
}

func (tcp *TCP) listen(l net.Listener, handler func(net.Conn)) {
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
				log.Warnf("%s: Accept error: %v; retrying in %v", tcp.ListenAddr, err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return
		}
		var tcpConn *net.TCPConn
		switch v := conn.(type) {
		case *net.TCPConn:
			tcpConn = v
		case *tls.Conn:
			tcpConn = v.NetConn().(*net.TCPConn)
		}
		if !tcp.NoDelay {
			tcpConn.SetNoDelay(false)
		}
		tempDelay = 0
		go handler(conn)
	}
}
func (tcp *TCP) ListenTCP(ctx context.Context, plugin TCPPlugin) error {
	l, err := net.Listen("tcp", tcp.ListenAddr)
	if err != nil {
		if Global.LogLang == "zh" {
			log.Fatalf("%s: 监听失败: %v", tcp.ListenAddr, err)
		} else {
			log.Fatalf("%s: Listen error: %v", tcp.ListenAddr, err)
		}
		return err
	}
	count := tcp.ListenNum
	if count == 0 {
		count = runtime.NumCPU()
	}
	log.Infof("tcp listen %d at %s", count, tcp.ListenAddr)
	for i := 0; i < count; i++ {
		go tcp.listen(l, plugin.ServeTCP)
	}
	if tcp.ListenAddrTLS != "" {
		keyPair, _ := tls.X509KeyPair(LocalCert, LocalKey)
		if tcp.CertFile != "" || tcp.KeyFile != "" {
			keyPair, err = tls.LoadX509KeyPair(tcp.CertFile, tcp.KeyFile)
		}
		if err != nil {
			if Global.LogLang == "zh" {
				log.Fatalf("加载证书失败: %v", err)
			} else {
				log.Fatalf("LoadX509KeyPair error: %v", err)
			}
			return err
		}
		l, err = tls.Listen("tcp", tcp.ListenAddrTLS, &tls.Config{
			Certificates: []tls.Certificate{keyPair},
		})
		if err != nil {
			if Global.LogLang == "zh" {
				log.Fatalf("%s: 监听失败: %v", tcp.ListenAddrTLS, err)
			} else {
				log.Fatalf("%s: Listen error: %v", tcp.ListenAddrTLS, err)
			}
			return err
		}
		log.Infof("tls tcp listen %d at %s", count, tcp.ListenAddrTLS)
		for i := 0; i < count; i++ {
			go tcp.listen(l, plugin.ServeTCP)
		}
	}
	<-ctx.Done()
	return l.Close()
}
