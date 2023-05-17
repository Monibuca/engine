//go:build (linux && !arm64) || darwin

package util

import (
	"log"
	"os"
	"syscall"
	"time"
)

func init() {
	fatal_log := "./fatal.log"
	if _fatal_log := os.Getenv("M7S_FATAL_LOG"); _fatal_log != "" {
		fatal_log = _fatal_log
	}
	logFile, err := os.OpenFile(fatal_log, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		log.Println("服务启动出错", "打开异常日志文件失败", err)
		return
	}
	// 将进程标准出错重定向至文件，进程崩溃时运行时将向该文件记录协程调用栈信息
	syscall.Dup2(int(logFile.Fd()), int(os.Stderr.Fd()))

	os.Stderr.WriteString("\n" + time.Now().Format("2006-01-02 15:04:05") + "--------------------------------\n")
}
