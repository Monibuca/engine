package engine

import (
	"io"
	"strings"

	"github.com/Monibuca/engine/v4/util"
	. "github.com/logrusorgru/aurora"
	"github.com/mattn/go-colorable"
	log "github.com/sirupsen/logrus"
)

var levelColors = []func(any) Value{Red, Red, Red, Yellow, Blue, Green, White}

// MultiLogWriter 可动态增减输出的多端写日志类
type MultiLogWriter struct {
	writers util.Slice[io.Writer]
	io.Writer
}

var colorableStdout = colorable.NewColorableStdout()
var LogWriter = &MultiLogWriter{
	writers: util.Slice[io.Writer]{colorableStdout},
	Writer:  colorableStdout,
}

func init() {
	log.SetOutput(LogWriter)
	log.SetFormatter(LogWriter)
}

func (ml *MultiLogWriter) Add(w io.Writer) {
	ml.writers.Add(w)
	ml.Writer = io.MultiWriter(ml.writers...)
}

func (ml *MultiLogWriter) Delete(w io.Writer) {
	ml.writers.Delete(w)
	ml.Writer = io.MultiWriter(ml.writers...)
}

func (ml *MultiLogWriter) Format(entry *log.Entry) (b []byte, err error) {
	pl := entry.Data["plugin"]
	if pl == nil {
		pl = "Engine"
	}
	l := strings.ToUpper(entry.Level.String())[:1]
	var props string
	if stream := entry.Data["stream"]; stream != nil {
		props = Sprintf("[s:%s] ", stream)
	}
	if puber := entry.Data["puber"]; puber != nil {
		props += Sprintf("[pub:%s] ", puber)
	}
	if suber := entry.Data["suber"]; suber != nil {
		props += Sprintf("[sub:%s] ", suber)
	}
	return []byte(Sprintf(levelColors[entry.Level]("%s [%s] [%s]\t %s%s\n"), l, entry.Time.Format("15:04:05"), pl, props, entry.Message)), nil
}
