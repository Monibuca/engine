package engine

import (
	"encoding/json"
	"testing"
)

func TestJSON(t *testing.T) {
	t.Run(t.Name(), func(t *testing.T) {
		var s Stream
		s.StreamPath = "test"
		s.Publish()
		s.NewVideoTrack(7)
		bytes, err := json.Marshal(&s)
		if err == nil {
			str := string(bytes)
			t.Logf("%s", str)
		}
	})
}
