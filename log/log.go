package log

import (
	// . "github.com/logrusorgru/aurora"
	"io"
	"os"

	// "github.com/mattn/go-colorable"
	"gopkg.in/yaml.v3"

	// log "github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var sugaredLogger *zap.SugaredLogger
var logger *zap.Logger

// var levelColors = []func(any) Value{Red, Red, Red, Yellow, Blue, Green, White}

// type LogWriter func(*log.Entry) string

// var colorableStdout = colorable.NewColorableStdout()
type MultipleWriter []io.Writer

func (m *MultipleWriter) Write(p []byte) (n int, err error) {
	for _, w := range *m {
		n, err = w.Write(p)
		if err != nil {
			m.Delete(w)
		}
	}
	return len(p), nil
}
func (m *MultipleWriter) Delete(writer io.Writer) {
	for i, w := range *m {
		if w == writer {
			*m = append((*m)[:i], (*m)[i+1:]...)
			return
		}
	}
}
func (m *MultipleWriter) Add(writer io.Writer) {
	*m = append(*m, writer)
}

var multipleWriter = &MultipleWriter{os.Stdout}
var Config = zap.NewDevelopmentConfig()
func AddWriter(writer io.Writer) {
	multipleWriter.Add(writer)
}
func DeleteWriter(writer io.Writer) {
	multipleWriter.Delete(writer)
}
func init() {
	// std.SetOutput(colorableStdout)
	// std.SetFormatter(LogWriter(defaultFormatter))
	Config.EncoderConfig.NewReflectedEncoder = func(w io.Writer) zapcore.ReflectedEncoder {
		return yaml.NewEncoder(w)
	}
	Config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	Config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	logger = zap.New(
		zapcore.NewCore(zapcore.NewConsoleEncoder(Config.EncoderConfig), zapcore.AddSync(multipleWriter), Config.Level),
	)
	sugaredLogger = logger.Sugar()
}

type Zap interface {
	With(fields ...zap.Field) *zap.Logger
	Debug(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
}

func With(fields ...zap.Field) *zap.Logger {
	return logger.With(fields...)
}

func Debug(args ...any) {
	sugaredLogger.Debug(args...)
}

func Info(args ...any) {
	sugaredLogger.Info(args...)
}

func Warn(args ...any) {
	sugaredLogger.Warn(args...)
}

func Error(args ...any) {
	sugaredLogger.Error(args...)
}

func Debugf(format string, args ...interface{}) {
	sugaredLogger.Debugf(format, args...)
}

// Infof logs a message at level Info on the standard logger.
func Infof(format string, args ...interface{}) {
	sugaredLogger.Infof(format, args...)
}

// Warnf logs a message at level Warn on the standard logger.
func Warnf(format string, args ...interface{}) {
	sugaredLogger.Warnf(format, args...)
}

// Errorf logs a message at level Error on the standard logger.
func Errorf(format string, args ...interface{}) {
	sugaredLogger.Errorf(format, args...)
}

// Panicf logs a message at level Panic on the standard logger.
func Panicf(format string, args ...interface{}) {
	sugaredLogger.Panicf(format, args...)
}

// Fatalf logs a message at level Fatal on the standard logger then the process will exit with status set to 1.
func Fatalf(format string, args ...interface{}) {
	sugaredLogger.Fatalf(format, args...)
}

// func defaultFormatter(entry *log.Entry) string {
// 	pl := entry.Data["plugin"]
// 	if pl == nil {
// 		pl = "Engine"
// 	}
// 	l := strings.ToUpper(entry.Level.String())[:1]
// 	var props string
// 	if stream := entry.Data["stream"]; stream != nil {
// 		props = Sprintf("[s:%s] ", stream)
// 	}
// 	if puber := entry.Data["puber"]; puber != nil {
// 		props += Sprintf("[pub:%s] ", puber)
// 	}
// 	if suber := entry.Data["suber"]; suber != nil {
// 		props += Sprintf("[sub:%s] ", suber)
// 	}
// 	return Sprintf(levelColors[entry.Level]("%s [%s] [%s]\t %s%s\n"), l, entry.Time.Format("15:04:05"), pl, props, entry.Message)
// }

// func (f LogWriter) Format(entry *log.Entry) (b []byte, err error) {
// 	return []byte(f(entry)), nil
// }

// var (
// 	// std is the name of the standard logger in stdlib `log`
// 	std = log.New()
// )

// func StandardLogger() *log.Logger {
// 	return std
// }

// // SetOutput sets the standard logger output.
// func SetOutput(out io.Writer) {
// 	std.SetOutput(out)
// }

// // SetFormatter sets the standard logger formatter.
// func SetFormatter(formatter log.Formatter) {
// 	std.SetFormatter(formatter)
// }

// // SetReportCaller sets whether the standard logger will include the calling
// // method as a field.
// func SetReportCaller(include bool) {
// 	std.SetReportCaller(include)
// }

// // SetLevel sets the standard logger level.
// func SetLevel(level log.Level) {
// 	std.SetLevel(level)
// }

// // GetLevel returns the standard logger level.
// func GetLevel() log.Level {
// 	return std.GetLevel()
// }

// // IsLevelEnabled checks if the log level of the standard logger is greater than the level param
// func IsLevelEnabled(level log.Level) bool {
// 	return std.IsLevelEnabled(level)
// }

// // AddHook adds a hook to the standard logger hooks.
// func AddHook(hook log.Hook) {
// 	std.AddHook(hook)
// }

// // WithError creates an entry from the standard logger and adds an error to it, using the value defined in ErrorKey as key.
// func WithError(err error) *log.Entry {
// 	return std.WithField(log.ErrorKey, err)
// }

// // WithContext creates an entry from the standard logger and adds a context to it.
// func WithContext(ctx context.Context) *log.Entry {
// 	return std.WithContext(ctx)
// }

// // WithField creates an entry from the standard logger and adds a field to
// // it. If you want multiple fields, use `WithFields`.
// //
// // Note that it doesn't log until you call Debug, Print, Info, Warn, Fatal
// // or Panic on the Entry it returns.
// func WithField(key string, value interface{}) *log.Entry {
// 	return std.WithField(key, value)
// }

// // WithFields creates an entry from the standard logger and adds multiple
// // fields to it. This is simply a helper for `WithField`, invoking it
// // once for each field.
// //
// // Note that it doesn't log until you call Debug, Print, Info, Warn, Fatal
// // or Panic on the Entry it returns.
// func WithFields(fields log.Fields) *log.Entry {
// 	return std.WithFields(fields)
// }

// // WithTime creates an entry from the standard logger and overrides the time of
// // logs generated with it.
// //
// // Note that it doesn't log until you call Debug, Print, Info, Warn, Fatal
// // or Panic on the Entry it returns.
// func WithTime(t time.Time) *log.Entry {
// 	return std.WithTime(t)
// }

// // Trace logs a message at level Trace on the standard logger.
// func Trace(args ...interface{}) {
// 	std.Trace(args...)
// }

// // Debug logs a message at level Debug on the standard logger.
// func Debug(args ...interface{}) {
// 	std.Debug(args...)
// }

// // Print logs a message at level Info on the standard logger.
// func Print(args ...interface{}) {
// 	std.Print(args...)
// }

// // Info logs a message at level Info on the standard logger.
// func Info(args ...interface{}) {
// 	std.Info(args...)
// }

// // Warn logs a message at level Warn on the standard logger.
// func Warn(args ...interface{}) {
// 	std.Warn(args...)
// }

// // Warning logs a message at level Warn on the standard logger.
// func Warning(args ...interface{}) {
// 	std.Warning(args...)
// }

// // Error logs a message at level Error on the standard logger.
// func Error(args ...interface{}) {
// 	std.Error(args...)
// }

// // Panic logs a message at level Panic on the standard logger.
// func Panic(args ...interface{}) {
// 	std.Panic(args...)
// }

// // Fatal logs a message at level Fatal on the standard logger then the process will exit with status set to 1.
// func Fatal(args ...interface{}) {
// 	std.Fatal(args...)
// }

// // TraceFn logs a message from a func at level Trace on the standard logger.
// func TraceFn(fn log.LogFunction) {
// 	std.TraceFn(fn)
// }

// // DebugFn logs a message from a func at level Debug on the standard logger.
// func DebugFn(fn log.LogFunction) {
// 	std.DebugFn(fn)
// }

// // PrintFn logs a message from a func at level Info on the standard logger.
// func PrintFn(fn log.LogFunction) {
// 	std.PrintFn(fn)
// }

// // InfoFn logs a message from a func at level Info on the standard logger.
// func InfoFn(fn log.LogFunction) {
// 	std.InfoFn(fn)
// }

// // WarnFn logs a message from a func at level Warn on the standard logger.
// func WarnFn(fn log.LogFunction) {
// 	std.WarnFn(fn)
// }

// // WarningFn logs a message from a func at level Warn on the standard logger.
// func WarningFn(fn log.LogFunction) {
// 	std.WarningFn(fn)
// }

// // ErrorFn logs a message from a func at level Error on the standard logger.
// func ErrorFn(fn log.LogFunction) {
// 	std.ErrorFn(fn)
// }

// // PanicFn logs a message from a func at level Panic on the standard logger.
// func PanicFn(fn log.LogFunction) {
// 	std.PanicFn(fn)
// }

// // FatalFn logs a message from a func at level Fatal on the standard logger then the process will exit with status set to 1.
// func FatalFn(fn log.LogFunction) {
// 	std.FatalFn(fn)
// }

// // Tracef logs a message at level Trace on the standard logger.
// func Tracef(format string, args ...interface{}) {
// 	std.Tracef(format, args...)
// }

// // Debugf logs a message at level Debug on the standard logger.
// func Debugf(format string, args ...interface{}) {
// 	std.Debugf(format, args...)
// }

// // Printf logs a message at level Info on the standard logger.
// func Printf(format string, args ...interface{}) {
// 	std.Printf(format, args...)
// }

// // Infof logs a message at level Info on the standard logger.
// func Infof(format string, args ...interface{}) {
// 	std.Infof(format, args...)
// }

// // Warnf logs a message at level Warn on the standard logger.
// func Warnf(format string, args ...interface{}) {
// 	std.Warnf(format, args...)
// }

// // Warningf logs a message at level Warn on the standard logger.
// func Warningf(format string, args ...interface{}) {
// 	std.Warningf(format, args...)
// }

// // Errorf logs a message at level Error on the standard logger.
// func Errorf(format string, args ...interface{}) {
// 	std.Errorf(format, args...)
// }

// // Panicf logs a message at level Panic on the standard logger.
// func Panicf(format string, args ...interface{}) {
// 	std.Panicf(format, args...)
// }

// // Fatalf logs a message at level Fatal on the standard logger then the process will exit with status set to 1.
// func Fatalf(format string, args ...interface{}) {
// 	std.Fatalf(format, args...)
// }

// // Traceln logs a message at level Trace on the standard logger.
// func Traceln(args ...interface{}) {
// 	std.Traceln(args...)
// }

// // Debugln logs a message at level Debug on the standard logger.
// func Debugln(args ...interface{}) {
// 	std.Debugln(args...)
// }

// // Println logs a message at level Info on the standard logger.
// func Println(args ...interface{}) {
// 	std.Println(args...)
// }

// // Infoln logs a message at level Info on the standard logger.
// func Infoln(args ...interface{}) {
// 	std.Infoln(args...)
// }

// // Warnln logs a message at level Warn on the standard logger.
// func Warnln(args ...interface{}) {
// 	std.Warnln(args...)
// }

// // Warningln logs a message at level Warn on the standard logger.
// func Warningln(args ...interface{}) {
// 	std.Warningln(args...)
// }

// // Errorln logs a message at level Error on the standard logger.
// func Errorln(args ...interface{}) {
// 	std.Errorln(args...)
// }

// // Panicln logs a message at level Panic on the standard logger.
// func Panicln(args ...interface{}) {
// 	std.Panicln(args...)
// }

// // Fatalln logs a message at level Fatal on the standard logger then the process will exit with status set to 1.
// func Fatalln(args ...interface{}) {
// 	std.Fatalln(args...)
// }
