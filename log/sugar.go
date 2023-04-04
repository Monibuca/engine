package log

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
