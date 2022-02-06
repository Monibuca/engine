package util

import (
	"constraints"
	"os"
	"path/filepath"
	"runtime"
)

func Clone[T any](x T) *T {
	return &x
}

func CurrentDir(path ...string) string {
	if _, currentFilePath, _, _ := runtime.Caller(1); len(path) == 0 {
		return filepath.Dir(currentFilePath)
	} else {
		return filepath.Join(filepath.Dir(currentFilePath), filepath.Join(path...))
	}
}

// 检查文件或目录是否存在
// 如果由 filename 指定的文件或目录存在则返回 true，否则返回 false
func Exist(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil || os.IsExist(err)
}

func ConvertNum[F constraints.Integer, T constraints.Integer](from F, to T) T {
	return T(from)
}

func ToFloat64(num any) float64 {
	switch v := num.(type) {
	case uint:
		return float64(v)
	case int:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case float32:
		return float64(v)
	}
	return 0
}
func GetPtsDts(v uint64) uint64 {
	// 4 + 3 + 1 + 15 + 1 + 15 + 1
	// 0011
	// 0010 + PTS[30-32] + marker_bit + PTS[29-15] + marker_bit + PTS[14-0] + marker_bit
	pts1 := ((v >> 33) & 0x7) << 30
	pts2 := ((v >> 17) & 0x7fff) << 15
	pts3 := ((v >> 1) & 0x7fff)

	return pts1 | pts2 | pts3
}

func PutPtsDts(v uint64) uint64 {
	// 4 + 3 + 1 + 15 + 1 + 15 + 1
	// 0011
	// 0010 + PTS[30-32] + marker_bit + PTS[29-15] + marker_bit + PTS[14-0] + marker_bit
	// 0x100010001
	// 0001 0000 0000 0000 0001 0000 0000 0000 0001
	// 3个 market_it
	pts1 := (v >> 30) & 0x7 << 33
	pts2 := (v >> 15) & 0x7fff << 17
	pts3 := (v & 0x7fff) << 1

	return pts1 | pts2 | pts3 | 0x100010001
}

func GetPCR(v uint64) uint64 {
	// program_clock_reference_base(33) + Reserved(6) + program_clock_reference_extension(9)
	base := v >> 15
	ext := v & 0x1ff
	return base*300 + ext
}

func PutPCR(pcr uint64) uint64 {
	base := pcr / 300
	ext := pcr % 300
	return base<<15 | 0x3f<<9 | ext
}