// +build linux

package util

import (
	"log"
	"os"
	"runtime"
	"syscall"
)

func init() {
	logFile, err := os.OpenFile("./fatal.log", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
	if err != nil {
		log.Println("服务启动出错", "打开异常日志文件失败", err)
		return
	}
	// 将进程标准出错重定向至文件，进程崩溃时运行时将向该文件记录协程调用栈信息
	if runtime.GOARCH == "arm64" {
		syscall.Dup3(int(logFile.Fd()), int(os.Stderr.Fd()), 0)
	} else {
		syscall.Dup2(int(logFile.Fd()), int(os.Stderr.Fd()))
	}
}
