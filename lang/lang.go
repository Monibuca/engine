package lang

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed zh.yaml
var zhYaml []byte
var zh map[string]string

func init() {
	yaml.Unmarshal(zhYaml, &zh)
}

func Get(lang string) map[string]string {
	if lang == "zh" {
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