package util

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

var Null = struct{}{}

func Clone[T any](x T) *T {
	return &x
}

func initFatalLog() *os.File {
	fatal_log_dir := "./fatal"
	if _fatal_log := os.Getenv("M7S_FATAL_LOG"); _fatal_log != "" {
		fatal_log_dir = _fatal_log
	}
	os.MkdirAll(fatal_log_dir, 0766)
	fatal_log := filepath.Join(fatal_log_dir, "latest.log")
	info, err := os.Stat(fatal_log)
	if err == nil && info.Size() != 0 {
		os.Rename(fatal_log, filepath.Join(fatal_log_dir, info.ModTime().Format("2006-01-02 15:04:05")+".log"))
	}
	logFile, err := os.OpenFile(fatal_log, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		log.Println("服务启动出错", "打开异常日志文件失败", err)
		return nil
	}
	return logFile
}

func CurrentDir(path ...string) string {
	if _, currentFilePath, _, _ := runtime.Caller(1); len(path) == 0 {
		return filepath.Dir(currentFilePath)
	} else {
		return filepath.Join(filepath.Dir(currentFilePath), filepath.Join(path...))
	}
}

// 检查文件或目录是否存在
// 如果由 filename 指定的文件或目录存在则返回 true，否则返回 false
func Exist(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || os.IsExist(err)
}

func ConvertNum[F Integer, T Integer](from F, to T) T {
	return T(from)
}

// Bit1 检查字节中的某一位是否为1 |0|1|2|3|4|5|6|7|
func Bit1(b byte, index int) bool {
	return b&(1<<(7-index)) != 0
}

func WaitTerm(cancel context.CancelFunc) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigc)
	<-sigc
	cancel()
}

// 判断目录是否是基础目录的子目录
func IsSubdir(baseDir, joinedDir string) bool {
	rel, err := filepath.Rel(baseDir, joinedDir)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, "/")
}

func Conditoinal[T any](cond bool, t, f T) T {
	if cond {
		return t
	} else {
		return f
	}
}
