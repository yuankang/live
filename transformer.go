package main

//Type    string
//"Metadata", "VideoHeader", "AudioHeader",
//"VideoKeyFrame", "VideoInterFrame"
//"AudioAacFrame", "AudioG711aFrame", "AudioG711uFrame"
//AvPacket表示一帧编码后的视频(nalu数据)或音频(aac/mp3/g711x数据)
//包结构体, 存放已压缩(编码)数据, 视频h264 音频AAC
type AvPacket struct {
	Type      string
	Timestamp uint32
	Data      []byte
}

//帧结构体, 存储未压缩(编码)数据, 视频YUV 音频PCM
type AvFrame struct {
}
