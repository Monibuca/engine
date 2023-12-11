package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"gopkg.in/yaml.v3"
	"m7s.live/engine/v4/log"
)

type Config struct {
	Ptr     reflect.Value //指向配置结构体值
	Value   any           //当前值,优先级：动态修改值>环境变量>配置文件>defaultYaml>全局配置>默认值
	Modify  any           //动态修改的值
	Env     any           //环境变量中的值
	File    any           //配置文件中的值
	Global  *Config       //全局配置中的值,指针类型
	Default any           //默认值
	Enum    []struct {
		Label string `json:"label"`
		Value any    `json:"value"`
	}
	name     string // 小写
	propsMap map[string]*Config
	props    []*Config
	tag      reflect.StructTag
}

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

func (config *Config) Range(f func(key string, value Config)) {
	if m, ok := config.Value.(map[string]Config); ok {
		for k, v := range m {
			f(k, v)
		}
	}
}

func (config *Config) IsMap() bool {
	_, ok := config.Value.(map[string]Config)
	return ok
}

func (config *Config) Get(key string) (v *Config) {
	if config.propsMap == nil {
		config.propsMap = make(map[string]*Config)
	}
	if v, ok := config.propsMap[key]; ok {
		return v
	} else {
		v = &Config{
			name: key,
		}
		config.propsMap[key] = v
		config.props = append(config.props, v)
		return v
	}
}

func (config Config) Has(key string) (ok bool) {
	if config.propsMap == nil {
		return false
	}
	_, ok = config.propsMap[strings.ToLower(key)]
	return ok
}

func (config *Config) MarshalJSON() ([]byte, error) {
	if config.propsMap == nil {
		return json.Marshal(config.Value)
	}
	return json.Marshal(config.propsMap)
}

// Parse 第一步读取配置结构体的默认值
func (config *Config) Parse(s any, prefix ...string) {
	var t reflect.Type
	var v reflect.Value
	if vv, ok := s.(reflect.Value); ok {
		t, v = vv.Type(), vv
	} else {
		t, v = reflect.TypeOf(s), reflect.ValueOf(s)
	}
	if t.Kind() == reflect.Pointer {
		t, v = t.Elem(), v.Elem()
	}
	config.Ptr = v
	config.Default = v.Interface()
	config.Value = v.Interface()
	if len(prefix) > 0 { // 读取环境变量
		envKey := strings.Join(prefix, "_")
		if envValue := os.Getenv(envKey); envValue != "" {
			yaml.Unmarshal([]byte(fmt.Sprintf("env: %s", envValue)), config)
			config.Value = config.Env
			config.Ptr.Set(reflect.ValueOf(config.Env))
		}
	}
	if t.Kind() == reflect.Struct {
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
			prop := config.Get(name)
			prop.Parse(fv, append(prefix, strings.ToUpper(ft.Name))...)
			prop.tag = ft.Tag
			for _, kv := range strings.Split(ft.Tag.Get("enum"), ",") {
				kvs := strings.Split(kv, ":")
				if len(kvs) != 2 {
					continue
				}
				var tmp struct {
					Value any
				}
				yaml.Unmarshal([]byte(fmt.Sprintf("value: %s", strings.TrimSpace(kvs[0]))), &tmp)
				prop.Enum = append(prop.Enum, struct {
					Label string `json:"label"`
					Value any    `json:"value"`
				}{
					Label: strings.TrimSpace(kvs[1]),
					Value: tmp.Value,
				})
			}
		}
	}
}

// ParseDefaultYaml 第二步读取全局配置
func (config *Config) ParseGlobal(g *Config) {
	config.Global = g
	if config.propsMap != nil {
		for k, v := range config.propsMap {
			v.ParseGlobal(g.Get(k))
		}
	} else {
		config.Value = g.Value
	}
}

// ParseDefaultYaml 第三步读取内嵌默认配置
func (config *Config) ParseDefaultYaml(defaultYaml map[string]any) {
	if defaultYaml == nil {
		return
	}
	for k, v := range defaultYaml {
		if config.Has(k) {
			if prop := config.Get(k); prop.props != nil {
				if v != nil {
					prop.ParseDefaultYaml(v.(map[string]any))
				}
			} else {
				dv := prop.assign(k, v)
				prop.Default = dv.Interface()
				if prop.Env == nil {
					prop.Value = dv.Interface()
					prop.Ptr.Set(dv)
				}
			}
		}
	}
}

// ParseFile 第四步读取用户配置文件
func (config *Config) ParseUserFile(conf map[string]any) {
	if conf == nil {
		return
	}
	config.File = conf
	for k, v := range conf {
		if config.Has(k) {
			if prop := config.Get(k); prop.props != nil {
				if v != nil {
					prop.ParseUserFile(v.(map[string]any))
				}
			} else {
				fv := prop.assign(k, v)
				prop.File = fv.Interface()
				if prop.Env == nil {
					prop.Value = fv.Interface()
					prop.Ptr.Set(fv)
				}
			}
		}
	}
}

// ParseModifyFile 第五步读取动态修改配置文件
func (config *Config) ParseModifyFile(conf map[string]any) {
	if conf == nil {
		return
	}
	config.Modify = conf
	for k, v := range conf {
		if config.Has(k) {
			if prop := config.Get(k); prop.props != nil {
				if v != nil {
					prop.ParseModifyFile(v.(map[string]any))
				}
			} else {
				mv := prop.assign(k, v)
				prop.Modify = mv.Interface()
				prop.Value = mv.Interface()
				prop.Ptr.Set(mv)
			}
		}
	}
}

func (config *Config) GetMap() map[string]any {
	m := make(map[string]any)
	for k, v := range config.propsMap {
		if v.props != nil {
			if vv := v.GetMap(); vv != nil {
				m[k] = vv
			}
		} else if v.Value != nil {
			m[k] = v.Value
		}
	}
	if len(m) > 0 {
		return m
	}
	return nil
}

func (config *Config) schema(index int) (r any) {
	defer func() {
		err := recover()
		if err != nil {
			log.Error(err)
		}
	}()
	if config.props != nil {
		r := Card{
			Type:       "void",
			Component:  "Card",
			Properties: make(map[string]any),
			Index:      index,
		}
		r.ComponentProps = map[string]any{
			"title": config.name,
		}
		for i, v := range config.props {
			if strings.HasPrefix(v.tag.Get("desc"), "废弃") {
				continue
			}
			r.Properties[v.name] = v.schema(i)
		}
		return r
	} else {
		p := Property{
			Title:   config.name,
			Default: config.Value,
			DecoratorProps: map[string]any{
				"tooltip": config.tag.Get("desc"),
			},
			ComponentProps: map[string]any{},
			Decorator:      "FormItem",
			Index:          index,
		}
		if config.Modify != nil {
			p.Description = "已动态修改"
		} else if config.Env != nil {
			p.Description = "使用环境变量中的值"
		} else if config.File != nil {
			p.Description = "使用配置文件中的值"
		} else if config.Global != nil {
			p.Description = "已使用全局配置中的值"
		}
		p.Enum = config.Enum
		if config.Ptr.Type() == durationType {
			p.Type = "string"
			p.Component = "Input"
			str := config.Value.(time.Duration).String()
			p.ComponentProps = map[string]any{
				"placeholder": str,
			}
			p.Default = str
			p.DecoratorProps["addonAfter"] = "时间,单位：s,m,h,d，例如：100ms, 10s, 4m, 1h"
		} else {
			switch config.Ptr.Kind() {
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
				p.Type = "number"
				p.Component = "NumberPicker"
				p.ComponentProps = map[string]any{
					"placeholder": config.Value,
				}
			case reflect.Bool:
				p.Type = "boolean"
				p.Component = "Switch"
			case reflect.String:
				p.Type = "string"
				p.Component = "Input"
				p.ComponentProps = map[string]any{
					"placeholder": config.Value,
				}
			case reflect.Slice:
				p.Type = "array"
				p.Component = "Input"
				p.ComponentProps = map[string]any{
					"placeholder": config.Value,
				}
				p.DecoratorProps["addonAfter"] = "数组，每个元素用逗号分隔"
			case reflect.Map:
				var children []struct {
					Key   string `json:"mkey"`
					Value any    `json:"mvalue"`
				}
				p := Property{
					Type:      "array",
					Component: "ArrayTable",
					Decorator: "FormItem",
					Properties: map[string]any{
						"addition": map[string]string{
							"type":        "void",
							"title":       "添加",
							"x-component": "ArrayTable.Addition",
						},
					},
					Index: index,
					Title: config.name,
					Items: &Object{
						Type: "object",
						Properties: map[string]any{
							"c1": Card{
								Type:      "void",
								Component: "ArrayTable.Column",
								ComponentProps: map[string]any{
									"title": config.tag.Get("key"),
									"width": 300,
								},
								Properties: map[string]any{
									"mkey": Property{
										Type:      "string",
										Decorator: "FormItem",
										Component: "Input",
									},
								},
								Index: 0,
							},
							"c2": Card{
								Type:      "void",
								Component: "ArrayTable.Column",
								ComponentProps: map[string]any{
									"title": config.tag.Get("value"),
								},
								Properties: map[string]any{
									"mvalue": Property{
										Type:      "string",
										Decorator: "FormItem",
										Component: "Input",
									},
								},
								Index: 1,
							},
							"operator": Card{
								Type:      "void",
								Component: "ArrayTable.Column",
								ComponentProps: map[string]any{
									"title": "操作",
								},
								Properties: map[string]any{
									"remove": Card{
										Type:      "void",
										Component: "ArrayTable.Remove",
									},
								},
								Index: 2,
							},
						},
					},
				}
				iter := config.Ptr.MapRange()
				for iter.Next() {
					children = append(children, struct {
						Key   string `json:"mkey"`
						Value any    `json:"mvalue"`
					}{
						Key:   iter.Key().String(),
						Value: iter.Value().Interface(),
					})
				}
				p.Default = children
				return p
			}
		}
		if len(p.Enum) > 0 {
			p.Component = "Radio.Group"
		}
		return p
	}
}

func (config *Config) GetFormily() (r Formily) {
	r.Form.LabelCol = 4
	r.Form.WrapperCol = 20
	r.Schema = Object{
		Type:       "object",
		Properties: make(map[string]any),
	}
	for i, v := range config.props {
		if strings.HasPrefix(v.tag.Get("desc"), "废弃") {
			continue
		}
		r.Schema.Properties[v.name] = v.schema(i)
	}
	return
}

// func (config *Config) GetModify() map[string]any {
// 	m := make(map[string]any)
// 	for k, v := range config.props {
// 		if v.props != nil {
// 			if vv := v.GetModify(); vv != nil {
// 				m[k] = vv
// 			}
// 		} else if v.Modify != nil {
// 			m[k] = v.Modify
// 		}
// 	}
// 	if len(m) > 0 {
// 		return m
// 	}
// 	return nil
// }

var regexPureNumber = regexp.MustCompile(`^\d+$`)

func (config *Config) assign(k string, v any) (target reflect.Value) {
	ft := config.Ptr.Type()

	source := reflect.ValueOf(v)

	if ft == durationType {
		target = reflect.New(ft).Elem()
		if source.Type() == durationType {
			target.Set(source)
		} else if source.IsZero() || !source.IsValid() {
			target.SetInt(0)
		} else {
			timeStr := source.String()
			if d, err := time.ParseDuration(timeStr); err == nil && !regexPureNumber.MatchString(timeStr) {
				target.SetInt(int64(d))
			} else {
				if Global.LogLang == "zh" {
					log.Errorf("%s 无效的时间值: %v 请添加单位（s,m,h,d），例如：100ms, 10s, 4m, 1h", k, source)
				} else {
					log.Errorf("%s invalid duration value: %v please add unit (s,m,h,d)，eg: 100ms, 10s, 4m, 1h", k, source)
				}
				os.Exit(1)
			}
		}
		return
	}

	tmpStruct := reflect.StructOf([]reflect.StructField{
		{
			Name: strings.ToUpper(k),
			Type: ft,
		},
	})
	tmpValue := reflect.New(tmpStruct)
	tmpByte, _ := yaml.Marshal(map[string]any{k: v})
	yaml.Unmarshal(tmpByte, tmpValue.Interface())
	return tmpValue.Elem().Field(0)
	// switch target.Kind() {
	// case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	// 	target.SetUint(uint64(source.Int()))
	// case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
	// 	target.SetInt(source.Int())
	// case reflect.Float32, reflect.Float64:
	// 	if source.CanFloat() {
	// 		target.SetFloat(source.Float())
	// 	} else {
	// 		target.SetFloat(float64(source.Int()))
	// 	}
	// case reflect.Map:

	// case reflect.Slice:
	// 	var s reflect.Value
	// 	if source.Kind() == reflect.Slice {
	// 		l := source.Len()
	// 		s = reflect.MakeSlice(ft, l, source.Cap())
	// 		for i := 0; i < l; i++ {
	// 			fv := source.Index(i)
	// 			item := s.Index(i)
	// 			if child, ok := fv.Interface().(map[string]any); ok {
	// 				panic(child)
	// 				// item.Set(child.CreateElem(ft.Elem()))
	// 			} else if fv.Kind() == reflect.Interface {
	// 				item.Set(reflect.ValueOf(fv.Interface()).Convert(item.Type()))
	// 			} else {
	// 				item.Set(fv)
	// 			}
	// 		}
	// 	} else {
	// 		//值是单值，但类型是数组，默认解析为一个元素的数组
	// 		s = reflect.MakeSlice(ft, 1, 1)
	// 		s.Index(0).Set(source)
	// 	}
	// 	target.Set(s)
	// default:
	// 	if source.IsValid() {
	// 		target.Set(source.Convert(ft))
	// 	}
	// }
	return
}
