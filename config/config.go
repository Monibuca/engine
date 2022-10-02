package config

import (
	"net"
	"net/http"
	"reflect"
	"strings"

	"go.uber.org/zap"
	"m7s.live/engine/v4/log"
)

type Config map[string]any

type Plugin interface {
	// 可能的入参类型：FirstConfig 第一次初始化配置，Config 后续配置更新，SE系列（StateEvent）流状态变化事件
	OnEvent(any)
}

type TCPPlugin interface {
	Plugin
	ServeTCP(*net.TCPConn)
}

type HTTPPlugin interface {
	Plugin
	http.Handler
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
	defer func() {
		if err := recover(); err != nil {
			log.Error("Unmarshal error:", err)
		}
	}()
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
		name := t.Field(i).Name
		nameMap[strings.ToLower(name)] = name
	}
	for k, v := range config {
		name, ok := nameMap[k]
		if !ok {
			log.Error("no config named:", k)
			continue
		}
		// 需要被写入的字段
		fv := el.FieldByName(name)
		fvKind := fv.Kind()
		ft := fv.Type()
		value := reflect.ValueOf(v)
		if child, ok := v.(Config); ok { //处理值是递归情况（map)
			if fvKind == reflect.Map {
				if fv.IsNil() {
					fv.Set(reflect.MakeMap(ft))
				}
			}
			child.Unmarshal(fv)
		} else {
			switch fvKind {
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				fv.SetUint(uint64(value.Int()))
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				fv.SetInt(value.Int())
			case reflect.Float32, reflect.Float64:
				fv.SetFloat(value.Float())
			case reflect.Slice:
				var s reflect.Value
				if value.Kind() == reflect.Slice {
					l := value.Len()
					s = reflect.MakeSlice(ft, l, value.Cap())
					for i := 0; i < l; i++ {
						fv := value.Index(i)
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
					s.Index(0).Set(value)
				}
				fv.Set(s)
			default:
				fv.Set(value)
			}
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
				log.Debug("merge", zap.String("k", k), zap.Any("v", v))
				config[k] = v
			}
		} else {
			log.Debug("exist", zap.String("k", k))
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
	if v, ok := config[strings.ToLower(key)]; ok && v != nil {
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
		if !ft.IsExported() {
			continue
		}
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
