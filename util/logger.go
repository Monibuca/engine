package util

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	colorable "github.com/mattn/go-colorable"

	"github.com/logrusorgru/aurora"
)

// MultiLogWriter 多端写日志类
type MultiLogWriter struct {
	writers []io.Writer
	io.Writer
}

var logWriter MultiLogWriter
var multiLogger = log.New(&logWriter, "", log.LstdFlags)
var colorLogger = log.New(colorable.NewColorableStdout(), "", log.LstdFlags)

func init() {
	log.SetOutput(io.MultiWriter(os.Stdout, &logWriter))
	logWriter.Writer = io.MultiWriter()
}

// AddWriter 添加日志输出端
func AddWriter(wn io.Writer) {
	logWriter.writers = append(logWriter.writers, wn)
	logWriter.Writer = io.MultiWriter(logWriter.writers...)
}

// MayBeError 优雅错误判断加日志辅助函数
func MayBeError(info error) (hasError bool) {
	if hasError = info != nil; hasError {
		Print(aurora.Red(info))
	}
	return
}
func getNoColor(v ...interface{}) (noColor []interface{}) {
	noColor = append(noColor, v...)
	for i, value := range v {
		if vv, ok := value.(aurora.Value); ok {
			noColor[i] = vv.Value()
		}
	}
	return
}

// Print 带颜色识别
func Print(v ...interface{}) {
	noColor := getNoColor(v...)
	colorLogger.Output(2, fmt.Sprint(v...))
	multiLogger.Output(2, fmt.Sprint(noColor...))
}

// Printf calls Output to print to the standard logger.
// Arguments are handled in the manner of fmt.Printf.
func Printf(format string, v ...interface{}) {
	noColor := getNoColor(v...)
	colorLogger.Output(2, fmt.Sprintf(format, v...))
	multiLogger.Output(2, fmt.Sprintf(format, noColor...))
}

// Println calls Output to print to the standard logger.
// Arguments are handled in the manner of fmt.Println.
func Println(v ...interface{}) {
	noColor := getNoColor(v...)
	colorLogger.Output(2, fmt.Sprintln(v...))
	multiLogger.Output(2, fmt.Sprintln(noColor...))
}

type Event struct {
	Timestamp time.Time
	Level     int
	Label     string
	Tag       string
}
type EventContext struct {
	Name string
	context.Context
	EventChan chan *Event
}
