package log

import (
	"io"
	"os"
)

type MultipleWriter []io.Writer

func (m *MultipleWriter) Write(p []byte) (n int, err error) {
	for _, w := range *m {
		n, err = w.Write(p)
		if err != nil {
			m.Delete(w)
		}
	}
	return len(p), nil
}
func (m *MultipleWriter) Delete(writer io.Writer) {
	for i, w := range *m {
		if w == writer {
			*m = append((*m)[:i], (*m)[i+1:]...)
			return
		}
	}
}
func (m *MultipleWriter) Add(writer io.Writer) {
	*m = append(*m, writer)
}

var multipleWriter = &MultipleWriter{os.Stdout}

func AddWriter(writer io.Writer) {
	multipleWriter.Add(writer)
}
func DeleteWriter(writer io.Writer) {
	multipleWriter.Delete(writer)
}
