package config

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/log"
)

type Config map[string]any

var durationType = reflect.TypeOf(time.Duration(0))

type Plugin interface {
	// 可能的入参类型：FirstConfig 第一次初始化配置，Config 后续配置更新，SE系列（StateEvent）流状态变化事件
	OnEvent(any)
}

type TCPPlugin interface {
	Plugin
	ServeTCP(net.Conn)
}

type HTTPPlugin interface {
	Plugin
	http.Handler
}

type QuicPlugin interface {
	Plugin
	ServeQuic(quic.Connection)
}

// CreateElem 创建Map或者Slice中的元素
func (config Config) CreateElem(eleType reflect.Type) reflect.Value {
	if eleType.Kind() == reflect.Pointer {
		newv := reflect.New(eleType.Elem())
		config.Unmarshal(newv)
		return newv
	} else {
		newv := reflect.New(eleType)
		config.Unmarshal(newv)
		return newv.Elem()
	}
}

func (config Config) Unmarshal(s any) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		log.Error("Unmarshal error:", err)
	// 	}
	// }()
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
		tt := t.Elem()
		for k, v := range config {
			if child, ok := v.(Config); ok {
				//复杂类型
				el.SetMapIndex(reflect.ValueOf(k), child.CreateElem(tt))
			} else {
				//基本类型
				el.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v).Convert(tt))
			}
		}
		return
	}
	//字段映射，小写对应的大写
	nameMap := make(map[string]string)
	for i, j := 0, t.NumField(); i < j; i++ {
		field := t.Field(i)
		name := field.Name
		if tag := field.Tag.Get("yaml"); tag != "" {
			name, _, _ = strings.Cut(tag, ",")
		} else {
			name = strings.ToLower(name)
		}
		nameMap[name] = field.Name
	}
	for k, v := range config {
		name, ok := nameMap[k]
		if !ok {
			log.Error("no config named:", k)
			continue
		}
		// 需要被写入的字段
		fv := el.FieldByName(name)
		if child, ok := v.(Config); ok { //处理值是递归情况（map)
			if fv.Kind() == reflect.Map {
				if fv.IsNil() {
					fv.Set(reflect.MakeMap(fv.Type()))
				}
			}
			child.Unmarshal(fv)
		} else {
			assign(name, fv, reflect.ValueOf(v))
		}
	}
}

// 覆盖配置
func (config Config) Assign(source Config) {
	for k, v := range source {
		switch m := config[k].(type) {
		case Config:
			switch vv := v.(type) {
			case Config:
				m.Assign(vv)
			case map[string]any:
				m.Assign(Config(vv))
			}
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
				if Global.LogLang == "zh" {
					log.Debug("合并配置", k, ":", v)
				} else {
					log.Debug("merge", k, ":", v)
				}
				config[k] = v
			}
		} else {
			log.Debug("exist", k)
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

func (config Config) Get(key string) (v any) {
	v = config[strings.ToLower(key)]
	return
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
	if v, ok := config[strings.ToLower(key)]; ok && v != nil {
		return v.(Config)
	}
	return nil
}

func Struct2Config(s any, prefix ...string) (config Config) {
	config = make(Config)
	var t reflect.Type
	var v reflect.Value
	if vv, ok := s.(reflect.Value); ok {
		t, v = vv.Type(), vv
	} else {
		t, v = reflect.TypeOf(s), reflect.ValueOf(s)
		if t.Kind() == reflect.Pointer {
			t, v = t.Elem(), v.Elem()
		}
	}
	for i, j := 0, t.NumField(); i < j; i++ {
		ft, fv := t.Field(i), v.Field(i)
		if !ft.IsExported() {
			continue
		}
		name := strings.ToLower(ft.Name)
		if tag := ft.Tag.Get("yaml"); tag != "" {
			if tag == "-" {
				continue
			}
			name, _, _ = strings.Cut(tag, ",")
		}
		var envPath []string
		if len(prefix) > 0 {
			envPath = append(prefix, strings.ToUpper(ft.Name))
			envKey := strings.Join(envPath, "_")
			if envValue := os.Getenv(envKey); envValue != "" {
				yaml.Unmarshal([]byte(fmt.Sprintf("%s: %s", name, envValue)), config)
				assign(envKey, fv, reflect.ValueOf(config[name]))
				config[name] = fv.Interface()
				return
			}
		}
		switch ft.Type.Kind() {
		case reflect.Struct:
			config[name] = Struct2Config(fv, envPath...)
		default:
			reflect.ValueOf(config).SetMapIndex(reflect.ValueOf(name), fv)
		}
	}
	return
}

func assign(k string, target reflect.Value, source reflect.Value) {
	ft := target.Type()
	if ft == durationType && target.CanSet() {
		if source.Type() == durationType {
			target.Set(source)
		} else if source.IsZero() || !source.IsValid() {
			target.SetInt(0)
		} else if d, err := time.ParseDuration(source.String()); err == nil {
			target.SetInt(int64(d))
		} else {
			if Global.LogLang == "zh" {
				log.Errorf("%s 无效的时间值: %v 请添加单位（s,m,h,d），例如：100ms, 10s, 4m, 1h", k, source)
			} else {
				log.Errorf("%s invalid duration value: %v please add unit (s,m,h,d)，eg: 100ms, 10s, 4m, 1h", k, source)
			}
			os.Exit(1)
		}
		return
	}
	switch target.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		target.SetUint(uint64(source.Int()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		target.SetInt(source.Int())
	case reflect.Float32, reflect.Float64:
		if source.CanFloat() {
			target.SetFloat(source.Float())
		} else {
			target.SetFloat(float64(source.Int()))
		}
	case reflect.Slice:
		var s reflect.Value
		if source.Kind() == reflect.Slice {
			l := source.Len()
			s = reflect.MakeSlice(ft, l, source.Cap())
			for i := 0; i < l; i++ {
				fv := source.Index(i)
				item := s.Index(i)
				if child, ok := fv.Interface().(Config); ok {
					item.Set(child.CreateElem(ft.Elem()))
				} else if fv.Kind() == reflect.Interface {
					item.Set(reflect.ValueOf(fv.Interface()).Convert(item.Type()))
				} else {
					item.Set(fv)
				}
			}
		} else {
			//值是单值，但类型是数组，默认解析为一个元素的数组
			s = reflect.MakeSlice(ft, 1, 1)
			s.Index(0).Set(source)
		}
		target.Set(s)
	default:
		if source.IsValid() {
			target.Set(source.Convert(ft))
		}
	}
}
