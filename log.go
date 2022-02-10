package engine

import (
	"io"
	"github.com/mattn/go-colorable"
	"github.com/Monibuca/engine/v4/util"
	log "github.com/sirupsen/logrus"
)

// MultiLogWriter 可动态增减输出的多端写日志类
type MultiLogWriter struct {
	writers util.Slice[io.Writer]
	io.Writer
}
var colorableStdout = colorable.NewColorableStdout()
var LogWriter = MultiLogWriter{
	writers: util.Slice[io.Writer]{colorableStdout},
	Writer:  colorableStdout,
}

func init() {
	log.SetOutput(LogWriter)
}

func (ml *MultiLogWriter) Add(w io.Writer) {
	ml.writers.Add(w)
	ml.Writer = io.MultiWriter(ml.writers...)
}

func (ml *MultiLogWriter) Delete(w io.Writer) {
	ml.writers.Delete(w)
	ml.Writer = io.MultiWriter(ml.writers...)
}
