//go:build windows

package util

import (
	"log"
	"os"
	"syscall"
	"time"
)

var (
	kernel32         = syscall.MustLoadDLL("kernel32.dll")
	procSetStdHandle = kernel32.MustFindProc("SetStdHandle")
)

func setStdHandle(stdhandle int32, handle syscall.Handle) error {
	r0, _, e1 := syscall.Syscall(procSetStdHandle.Addr(), 2, uintptr(stdhandle), uintptr(handle), 0)
	if r0 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}

// redirectStderr to the file passed in
func init() {
	fatal_log := "./fatal.log"
	if _fatal_log := os.Getenv("M7S_FATAL_LOG"); _fatal_log != "" {
		fatal_log = _fatal_log
	}
	logFile, err := os.OpenFile(fatal_log, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	err = setStdHandle(syscall.STD_ERROR_HANDLE, syscall.Handle(logFile.Fd()))
	if err != nil {
		log.Fatalf("Failed to redirect stderr to file: %v", err)
	}
	// SetStdHandle does not affect prior references to stderr
	os.Stderr = logFile
	os.Stderr.WriteString("\n" + time.Now().Format("2006-01-02 15:04:05") + "--------------------------------\n")
}
