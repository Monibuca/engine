package codec

import (
	"fmt"
	"io"
	"reflect"

	"m7s.live/engine/v4/util"
)

// Action Message Format -- AMF 0
// Action Message Format -- AMF 3
// http://download.macromedia.com/pub/labs/amf/amf0_spec_121207.pdf
// http://wwwimages.adobe.com/www.adobe.com/content/dam/Adobe/en/devnet/amf/pdf/amf-file-format-spec.pdf

// AMF Object == AMF Object Type(1 byte) + AMF Object Value
//
// AMF Object Value :
// AMF0_STRING : 2 bytes(datasize,记录string的长度) + data(string)
// AMF0_OBJECT : AMF0_STRING + AMF Object
// AMF0_NULL : 0 byte
// AMF0_NUMBER : 8 bytes
// AMF0_DATE : 10 bytes
// AMF0_BOOLEAN : 1 byte
// AMF0_ECMA_ARRAY : 4 bytes(arraysize,记录数组的长度) + AMF0_OBJECT
// AMF0_STRICT_ARRAY : 4 bytes(arraysize,记录数组的长度) + AMF Object

// 实际测试时,AMF0_ECMA_ARRAY数据如下:
// 8 0 0 0 13 0 8 100 117 114 97 116 105 111 110 0 0 0 0 0 0 0 0 0 0 5 119 105 100 116 104 0 64 158 0 0 0 0 0 0 0 6 104 101 105 103 104 116 0 64 144 224 0 0 0 0 0
// 8 0 0 0 13 | { 0 8 100 117 114 97 116 105 111 110 --- 0 0 0 0 0 0 0 0 0 } | { 0 5 119 105 100 116 104 --- 0 64 158 0 0 0 0 0 0 } | { 0 6 104 101 105 103 104 116 --- 0 64 144 224 0 0 0 0 0 } |...
// 13 | {AMF0_STRING --- AMF0_NUMBER} | {AMF0_STRING --- AMF0_NUMBER} | {AMF0_STRING --- AMF0_NUMBER} | ...
// 13 | {AMF0_OBJECT} | {AMF0_OBJECT} | {AMF0_OBJECT} | ...
// 13 | {duration --- 0} | {width --- 1920} | {height --- 1080} | ...

const (
	AMF0_NUMBER = iota // 浮点数
	AMF0_BOOLEAN
	AMF0_STRING
	AMF0_OBJECT
	AMF0_MOVIECLIP
	AMF0_NULL
	AMF0_UNDEFINED
	AMF0_REFERENCE
	AMF0_ECMA_ARRAY
	AMF0_END_OBJECT
	AMF0_STRICT_ARRAY
	AMF0_DATE
	AMF0_LONG_STRING
	AMF0_UNSUPPORTED
	AMF0_RECORDSET
	AMF0_XML_DOCUMENT
	AMF0_TYPED_OBJECT
	AMF0_AVMPLUS_OBJECT
)
const (
	AMF3_UNDEFINED = iota
	AMF3_NULL
	AMF3_FALSE
	AMF3_TRUE
	AMF3_INTEGER
	AMF3_DOUBLE
	AMF3_STRING
	AMF3_XML_DOC
	AMF3_DATE
	AMF3_ARRAY
	AMF3_OBJECT
	AMF3_XML
	AMF3_BYTE_ARRAY
	AMF3_VECTOR_INT
	AMF3_VECTOR_UINT
	AMF3_VECTOR_DOUBLE
	AMF3_VECTOR_OBJECT
	AMF3_DICTIONARY
)

var (
	END_OBJ   = []byte{0, 0, AMF0_END_OBJECT}
	ObjectEnd = &struct{}{}
	Undefined = &struct{}{}
)

type EcmaArray map[string]any

type AMF struct {
	util.Buffer
}

func (amf *AMF) ReadShortString() string {
	value, _ := amf.Unmarshal()
	return value.(string)
}

func (amf *AMF) ReadNumber() float64 {
	value, _ := amf.Unmarshal()
	rt, ok := value.(float64)
	if ok {
		return rt
	} else {
		return 0
	}
}

func (amf *AMF) ReadObject() map[string]any {
	value, _ := amf.Unmarshal()
	if value == nil {
		return nil
	}
	return value.(map[string]any)
}

func (amf *AMF) ReadBool() bool {
	value, _ := amf.Unmarshal()
	rt, ok := value.(bool)
	if ok {
		return rt
	} else {
		return false
	}
}

func (amf *AMF) readKey() (string, error) {
	if !amf.CanReadN(2) {
		return "", io.ErrUnexpectedEOF
	}
	l := int(amf.ReadUint16())
	if !amf.CanReadN(l) {
		return "", io.ErrUnexpectedEOF
	}
	return string(amf.ReadN(l)), nil
}

func (amf *AMF) readProperty(m map[string]any) (obj any, err error) {
	var k string
	var v any
	if k, err = amf.readKey(); err == nil {
		if v, err = amf.Unmarshal(); k == "" && v == ObjectEnd {
			obj = m
		} else if err == nil {
			m[k] = v
		}
	}
	return
}

func (amf *AMF) Unmarshal() (obj any, err error) {
	if !amf.CanRead() {
		return nil, io.ErrUnexpectedEOF
	}
	defer func(b util.Buffer) {
		if err != nil {
			amf.Buffer = b
		}
	}(amf.Buffer)
	switch t := amf.ReadByte(); t {
	case AMF0_NUMBER:
		if !amf.CanReadN(8) {
			return 0, io.ErrUnexpectedEOF
		}
		obj = amf.ReadFloat64()
	case AMF0_BOOLEAN:
		if !amf.CanRead() {
			return false, io.ErrUnexpectedEOF
		}
		obj = amf.ReadByte() == 1
	case AMF0_STRING:
		obj, err = amf.readKey()
	case AMF0_OBJECT:
		m := make(map[string]any)
		for err == nil && obj == nil {
			obj, err = amf.readProperty(m)
		}
	case AMF0_NULL:
		return nil, nil
	case AMF0_UNDEFINED:
		return Undefined, nil
	case AMF0_ECMA_ARRAY:
		size := amf.ReadUint32()
		m := make(EcmaArray)
		for i := uint32(0); i < size && err == nil && obj == nil; i++ {
			obj, err = amf.readProperty(m)
		}
	case AMF0_END_OBJECT:
		return ObjectEnd, nil
	case AMF0_STRICT_ARRAY:
		size := amf.ReadUint32()
		var list []any
		for i := uint32(0); i < size; i++ {
			v, err := amf.Unmarshal()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		obj = list
	case AMF0_DATE:
		if !amf.CanReadN(10) {
			return 0, io.ErrUnexpectedEOF
		}
		obj = amf.ReadFloat64()
		amf.ReadN(2)
	case AMF0_LONG_STRING,
		AMF0_XML_DOCUMENT:
		if !amf.CanReadN(4) {
			return "", io.ErrUnexpectedEOF
		}
		l := int(amf.ReadUint32())
		if !amf.CanReadN(l) {
			return "", io.ErrUnexpectedEOF
		}
		obj = string(amf.ReadN(l))
	default:
		err = fmt.Errorf("unsupported type:%d", t)
	}
	return
}

func (amf *AMF) writeProperty(key string, v any) {
	amf.WriteUint16(uint16(len(key)))
	amf.WriteString(key)
	amf.Marshal(v)
}

func MarshalAMFs(v ...any) []byte {
	var amf AMF
	return amf.Marshals(v...)
}

func (amf *AMF) Marshals(v ...any) []byte {
	for _, vv := range v {
		amf.Marshal(vv)
	}
	return amf.Buffer
}

func (amf *AMF) Marshal(v any) []byte {
	if v == nil {
		amf.WriteByte(AMF0_NULL)
		return amf.Buffer
	}
	switch vv := v.(type) {
	case string:
		if l := len(vv); l > 0xFFFF {
			amf.WriteByte(AMF0_LONG_STRING)
			amf.WriteUint32(uint32(l))
		} else {
			amf.WriteByte(AMF0_STRING)
			amf.WriteUint16(uint16(l))
		}
		amf.WriteString(vv)
	case float64, uint, float32, int, int16, int32, int64, uint16, uint32, uint64, uint8, int8:
		amf.WriteByte(AMF0_NUMBER)
		amf.WriteFloat64(util.ToFloat64(vv))
	case bool:
		amf.WriteByte(AMF0_BOOLEAN)
		if vv {
			amf.WriteByte(1)
		} else {
			amf.WriteByte(0)
		}
	case EcmaArray:
		amf.WriteByte(AMF0_ECMA_ARRAY)
		amf.WriteUint32(uint32(len(vv)))
		for k, v := range vv {
			amf.writeProperty(k, v)
		}
		amf.Write(END_OBJ)
	case map[string]any:
		amf.WriteByte(AMF0_OBJECT)
		for k, v := range vv {
			amf.writeProperty(k, v)
		}
		amf.Write(END_OBJ)
	default:
		v := reflect.ValueOf(vv)
		if !v.IsValid() {
			amf.WriteByte(AMF0_NULL)
			return amf.Buffer
		}
		switch v.Kind() {
		case reflect.Slice, reflect.Array:
			amf.WriteByte(AMF0_STRICT_ARRAY)
			size := v.Len()
			amf.WriteUint32(uint32(size))
			for i := 0; i < size; i++ {
				amf.Marshal(v.Index(i).Interface())
			}
			amf.Write(END_OBJ)
		default:
			panic("amf Marshal faild")
		}
	}
	return amf.Buffer
}
