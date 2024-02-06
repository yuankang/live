package main

/*************************************************/
/* audio 音频都转码为 aac 11025 mono s16le
/*************************************************/
/*
## 计算音频帧间隔和帧大小
帧率    = 20
采样率	= 8000Hz
码率	= 64000bps
帧间隔  = 1000ms / 20 = 50ms = 0.05s
帧大小  = 64000bps / 20 = 3200bit = 400byte
*/

//AudioData see video_file_format_spec_v10.pdf
type AudioDataAAC struct {
	SoundFormat uint8 //4bit
	SoundRate   uint8 //2bit
	SoundSize   uint8 //1bit
	SoundType   uint8 //1bit
	SoundData   []byte
}
