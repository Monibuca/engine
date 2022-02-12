package config

import (
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type Config map[string]any

type Second int

func (s Second) Duration() time.Duration {
	return time.Duration(s) * time.Second
}

type Plugin interface {
	Update(Config)
}

type TCPPlugin interface {
	Plugin
	ServeTCP(*net.TCPConn)
}

type HTTPPlugin interface {
	Plugin
	http.Handler
}

func (config Config) Unmarshal(s any) {
	if s == nil {
		return
	}
	var el reflect.Value
	if v, ok := s.(reflect.Value); ok {
		el = v
	} else {
		el = reflect.ValueOf(s)
	}
	if el.Kind() == reflect.Pointer {
		el = el.Elem()
	}
	t := el.Type()
	if t.Kind() == reflect.Map {
		for k, v := range config {
			el.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v).Convert(t.Elem()))
		}
		return
	}
	//字段映射，小写对应的大写
	nameMap := make(map[string]string)
	for i, j := 0, t.NumField(); i < j; i++ {
		name := t.Field(i).Name
		nameMap[strings.ToLower(name)] = name
	}
	for k, v := range config {
		name, ok := nameMap[k]
		if !ok {
			logrus.Error("no config named:", k)
			continue
		}
		// 需要被写入的字段
		fv := el.FieldByName(name)
		if value := reflect.ValueOf(v); value.Kind() == reflect.Slice {
			l := value.Len()
			s := reflect.MakeSlice(fv.Type(), l, value.Cap())
			for i := 0; i < l; i++ {
				fv := value.Field(i)
				if fv.Type() == reflect.TypeOf(config) {
					fv.FieldByName("Unmarshal").Call([]reflect.Value{s.Field(i)})
				} else {
					s.Field(i).Set(fv)
				}
			}
			fv.Set(s)
		} else if child, ok := v.(Config); ok {
			if fv.Kind() == reflect.Map {
				if fv.IsNil() {
					fv.Set(reflect.MakeMap(fv.Type()))
				}
			}
			child.Unmarshal(fv)
		} else {
			fv.Set(value)
		}
	}
}

// 覆盖配置
func (config Config) Assign(source Config) {
	for k, v := range source {
		switch m := config[k].(type) {
		case Config:
			m.Assign(v.(Config))
		default:
			config[k] = v
		}
	}
}

// 合并配置，不覆盖
func (config Config) Merge(source Config) {
	for k, v := range source {
		if _, ok := config[k]; !ok {
			switch m := config[k].(type) {
			case Config:
				m.Merge(v.(Config))
			default:
				config[k] = v
			}
		}
	}
}

func (config *Config) Set(key string, value any) {
	if *config == nil {
		*config = Config{strings.ToLower(key): value}
	} else {
		(*config)[strings.ToLower(key)] = value
	}
}

func (config Config) Get(key string) any {
	v, _ := config[strings.ToLower(key)]
	return v
}

func (config Config) Has(key string) (ok bool) {
	_, ok = config[strings.ToLower(key)]
	return
}

func (config Config) HasChild(key string) (ok bool) {
	_, ok = config[strings.ToLower(key)].(Config)
	return ok
}

func (config Config) GetChild(key string) Config {
	if v, ok := config[strings.ToLower(key)]; ok {
		return v.(Config)
	}
	return nil
}

func Struct2Config(s any) (config Config) {
	config = make(Config)
	var t reflect.Type
	var v reflect.Value
	if vv, ok := s.(reflect.Value); ok {
		v = vv
		t = vv.Type()
	} else {
		t = reflect.TypeOf(s)
		v = reflect.ValueOf(s)
		if t.Kind() == reflect.Pointer {
			v = v.Elem()
			t = t.Elem()
		}
	}
	for i, j := 0, t.NumField(); i < j; i++ {
		ft := t.Field(i)
		name := strings.ToLower(ft.Name)
		switch ft.Type.Kind() {
		case reflect.Struct:
			config[name] = Struct2Config(v.Field(i))
		case reflect.Slice:
			fallthrough
		default:
			reflect.ValueOf(config).SetMapIndex(reflect.ValueOf(name), v.Field(i))
		}
	}
	return
}
