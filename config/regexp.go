package config

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

type Regexp struct {
	*regexp.Regexp
}

func (r *Regexp) UnmarshalYAML(node *yaml.Node) error {
	r.Regexp = regexp.MustCompile(node.Value)
	return nil
}

func (r *Regexp) MarshalYAML() (interface{}, error) {
	if r.Regexp == nil {
		return "", nil
	}
	return r.String(), nil
}

func (r *Regexp) MarshalJSON() ([]byte, error) {
	if r.Regexp == nil {
		return []byte(`""`), nil
	}
	return []byte(`"` + r.String() + `"`), nil
}

func (r *Regexp) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		b = b[1:]
	}
	if b[len(b)-1] == '"' {
		b = b[:len(b)-1]
	}
	r.Regexp = regexp.MustCompile(string(b))
	return nil
}