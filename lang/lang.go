package lang

import (
	_ "embed"

	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"strings"
	"runtime"
)

//go:embed zh.yaml
var zhYaml []byte
var zh map[string]string

func init() {
	yaml.Unmarshal(zhYaml, &zh)
}

func Get(lang string) map[string]string {
	if lang == "zh" {
		if runtime.GOOS == "linux" && !IsTerminalSupportChinese() {
			return nil
		}
		return zh
	}
	return nil
}

func Update(lang string, key string, value string) {
	if lang == "zh" {
		zh[key] = value
	}
}

func Merge(lang string, data map[string]string) {
	if lang == "zh" {
		for k, v := range data {
			zh[k] = v
		}
	}
}

func IsTerminalSupportChinese() bool {
	// 获取终端的环境变量
	env := os.Environ()

	// 查找 LANG 环境变量
	isSupportUTF8 := false
	for _, v := range env {
		if strings.Index(v, "LANG") != -1 && strings.Index(v, "UTF-8") != -1 {
			isSupportUTF8 = true
		}
	}
	if isSupportUTF8 {
		// 在终端中打印中文字符
		cmd := exec.Command("echo", "你好！")
		_, err := cmd.CombinedOutput()
		if err == nil {
			return true
		}
		return false
	}
	return false
}
