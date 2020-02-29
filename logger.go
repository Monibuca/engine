package engine

import (
	"io"
	"log"
)

// LogWriter 多端写日志类
type LogWriter struct {
	io.Writer
	origin io.Writer
}

func (w *LogWriter) Write(data []byte) (n int, err error) {
	if n, err = w.Writer.Write(data); err != nil {
		go log.SetOutput(w.origin)
	}
	return w.origin.Write(data)
}

// AddWriter 添加日志输出端
func AddWriter(wn io.Writer) {
	log.SetOutput(&LogWriter{
		Writer: wn,
		origin: log.Writer(),
	})
}

// MayBeError 优雅错误判断加日志辅助函数
func MayBeError(info error) (hasError bool) {
	if hasError = info != nil; hasError {
		log.Print(info)
	}
	return
}
