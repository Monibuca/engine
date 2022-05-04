package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
)

const retryTimes = 3

type HttpCallbackData struct {
	StreamName string `json:"stream_name"` //媒体流名称
	AppName    string `json:"app_name"`
	Event      string `json:"event"`  //事件名称
	Schema     string `json:"schema"` //媒体流类型
	Time       int64  `json:"time"`   //调用时间
}

func doRequest(host string, data any) error {
	param, _ := json.Marshal(data)

	// Execute the request
	return util.Retry(retryTimes, time.Second, func() error {
		resp, err := http.DefaultClient.Post(host, "application/json", bytes.NewBuffer(param))
		if err != nil {
			// Retry
			log.Warnf("post %s error: %s", host, err.Error())
			return err
		}
		defer resp.Body.Close()

		s := resp.StatusCode
		switch {
		case s >= 500:
			// Retry
			return fmt.Errorf("server %s error: %v", host, s)
		case s >= 400:
			// Don't retry, it was client's fault
			return util.RetryStopErr(fmt.Errorf("client %s error: %v", host, s))
		default:
			// Happy
			return nil
		}
	})
}

func HttpCallbackEvent(event any) {
	data := HttpCallbackData{}
	var s *Stream
	for _, endpoint := range EngineConfig.HTTPCallback {
		switch e := event.(type) {
		case SEclose:
			data.Event = "close"
			s = e.Stream
		case SEpublish:
			data.Event = "publish"
			s = e.Stream
		case ISubscriber:
			data.Event = "subscribe"
			s = e.GetIO().Stream
		default:
		}
		if s == nil {
			return
		}
		data.StreamName = s.StreamName
		data.AppName = s.AppName
		if s.Publisher != nil {
			data.Schema = s.Publisher.GetIO().Type
		}
		data.Time = time.Now().Unix()
		go doRequest(endpoint, data)
	}
}
