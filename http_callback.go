package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"m7s.live/engine/v4/config"
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
	var streamIo any
	for _, endpoint := range EngineConfig.HTTPCallback {
		switch e := event.(type) {
		case SEclose:
			data.Event = "close"
			streamIo = e.Stream.Publisher.GetIO()

		case SEpublish:
			data.Event = "publish"
			streamIo = e.Stream.Publisher.GetIO()

		case ISubscriber:
			data.Event = "subscribe"
			streamIo = e.GetIO()
		default:
		}
		if streamIo == nil {
			return
		}
		switch s := streamIo.(type) {
		case *IO[config.Publish, IPublisher]:
			data.StreamName = s.Stream.StreamName
			data.AppName = s.Stream.AppName
			data.Schema = s.Type
		case *IO[config.Subscribe, ISubscriber]:
			data.StreamName = s.Stream.StreamName
			data.AppName = s.Stream.AppName
			data.Schema = s.Type
		}

		data.Time = time.Now().Unix()
		go doRequest(endpoint, data)
	}
}
