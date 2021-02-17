# m7s核心引擎

该项目为m7s的引擎部分，该部分逻辑是流媒体服务器的核心转发逻辑。仅包含最基础的功能，不含任何网络协议部分，但包含了一个插件的引入机制，其他功能均由插件实现

# 引擎的基本功能
- 引擎初始化会加载配置文件，并逐个调用插件的Run函数
- 具有发布功能的插件，会通过GetStream函数创建一个流，即Stream对象，这个Stream对象随后可以被订阅
- Stream对象中含有两个列表，一个是VideoTracks一个是AudioTracks用来存放视频数据和音频数据
- 每一个VideoTrack或者AudioTrack中包含一个RingBuffer，用来存储发布者提供的数据，同时提供订阅者访问。
- 具有订阅功能的插件，会通过GetStream函数获取到一个流，然后选择VideoTracks、AudioTracks里面的RingBuffer进行连续的读取

# 发布插件如何发布流

以rtmp协议为例子
```go
if pub := new(engine.Publisher); pub.Publish(streamPath) {
    pub.Type = "RTMP"
    stream = pub.Stream
    err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
    err = nc.SendMessage(SEND_PUBLISH_START_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_Start, Level_Status))
} else {
    err = nc.SendMessage(SEND_PUBLISH_RESPONSE_MESSAGE, newPublishResponseMessageData(nc.streamID, NetStream_Publish_BadName, Level_Error))
}
```
默认会创建一个VideoTrack和一个AudioTrack
当我们接收到数据的时候就可以朝里面填充物数据了
```go
rec_video = func(msg *Chunk) {
    // 等待AVC序列帧
    if msg.Body[1] != 0 {
        return
    }
    vt := stream.VideoTracks[0]
    var ts_video uint32
    var info codec.AVCDecoderConfigurationRecord
    //0:codec,1:IsAVCSequence,2~4:compositionTime
    if _, err := info.Unmarshal(msg.Body[5:]); err == nil {
        vt.SPSInfo, err = codec.ParseSPS(info.SequenceParameterSetNALUnit)
        vt.SPS = info.SequenceParameterSetNALUnit
        vt.PPS = info.PictureParameterSetNALUnit
    }
    vt.RtmpTag = msg.Body
    nalulenSize := int(info.LengthSizeMinusOne&3 + 1)
    rec_video = func(msg *Chunk) {
        nalus := msg.Body[5:]
        if msg.Timestamp == 0xffffff {
            ts_video += msg.ExtendTimestamp
        } else {
            ts_video += msg.Timestamp // 绝对时间戳
        }
        for len(nalus) > nalulenSize {
            nalulen := 0
            for i := 0; i < nalulenSize; i++ {
                nalulen += int(nalus[i]) << (8 * (nalulenSize - i - 1))
            }
            vt.Push(ts_video, nalus[nalulenSize:nalulen+nalulenSize])
            nalus = nalus[nalulen+nalulenSize:]
        }
    }
    close(vt.WaitFirst)
}
```
在填充数据之前，需要获取到SPS和PPS，然后设置好，因为订阅者需要先发送这个数据
然后通过Track到Push函数将数据填充到RingBuffer里面去

# 订阅插件如何订阅流

```go
subscriber := engine.Subscriber{
    Type: "RTMP",
    ID:   fmt.Sprintf("%s|%d", conn.RemoteAddr().String(), nc.streamID),
}
if err = subscriber.Subscribe(streamPath); err == nil {
    streams[nc.streamID] = &subscriber
    err = nc.SendMessage(SEND_CHUNK_SIZE_MESSAGE, uint32(nc.writeChunkSize))
    err = nc.SendMessage(SEND_STREAM_IS_RECORDED_MESSAGE, nil)
    err = nc.SendMessage(SEND_STREAM_BEGIN_MESSAGE, nil)
    err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Reset, Level_Status))
    err = nc.SendMessage(SEND_PLAY_RESPONSE_MESSAGE, newPlayResponseMessageData(nc.streamID, NetStream_Play_Start, Level_Status))
    vt, at := subscriber.GetVideoTrack("h264"), subscriber.OriginAudioTrack
    if vt != nil {
        var lastVideoTime uint32
        err = nc.SendMessage(SEND_FULL_VDIEO_MESSAGE, &AVPack{Payload: vt.RtmpTag})
        subscriber.OnVideo = func(pack engine.VideoPack) {
            if lastVideoTime == 0 {
                lastVideoTime = pack.Timestamp
            }
            t := pack.Timestamp - lastVideoTime
            lastVideoTime = pack.Timestamp
            payload := codec.Nalu2RTMPTag(pack.Payload)
            defer utils.RecycleSlice(payload)
            err = nc.SendMessage(SEND_VIDEO_MESSAGE, &AVPack{Timestamp: t, Payload: payload})
        }
    }
    if at != nil {
        var lastAudioTime uint32
        var aac byte
        if at.SoundFormat == 10 {
            aac = at.RtmpTag[0]
            err = nc.SendMessage(SEND_FULL_AUDIO_MESSAGE, &AVPack{Payload: at.RtmpTag})
        }
        subscriber.OnAudio = func(pack engine.AudioPack) {
            if lastAudioTime == 0 {
                lastAudioTime = pack.Timestamp
            }
            t := pack.Timestamp - lastAudioTime
            lastAudioTime = pack.Timestamp
            payload := codec.Audio2RTMPTag(aac, pack.Payload)
            defer utils.RecycleSlice(payload)
            err = nc.SendMessage(SEND_AUDIO_MESSAGE, &AVPack{Timestamp: t, Payload: payload})
        }
    }
    go subscriber.Play(at, vt)
}
```
- 在发送数据前，需要先发送音视频的序列帧