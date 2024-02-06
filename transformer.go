package main

//Type    string
//"Metadata", "VideoHeader", "AudioHeader",
//"VideoKeyFrame", "VideoInterFrame"
//"AudioAacFrame", "AudioG711aFrame", "AudioG711uFrame"
//AvPacket表示一帧编码后的视频(nalu数据)或音频(aac/mp3/g711x数据)
type AvPacket struct {
	Type      string
	Timestamp uint32
	Data      []byte
}
