package main

import (
	"fmt"
	"utils"
)

/*************************************************/
/* audio data header
/*************************************************/
//video_file_format_spec_v10.pdf
//SoundFormat
//2:MP3, 7:g711a, 8:g711u, 10:AAC, 14:MP3 8-Khz,
//SoundRate
//0:5.5-kHz, 1:11025, 2:22050, 3:44100(aac always 3)
//SoundSize
//0:snd8Bit, 1:snd16Bit,
//SoundType
//0:sndMono, 单声道, 1:sndStereo, 双声道,
//AACPacketType
//0:AAC sequence header, 1:AAC raw,
//1+1=2Byte
type RtmpAudioDataHeader struct {
	SoundFormat   uint8 //4bit
	SoundRate     uint8 //2bit
	SoundSize     uint8 //1bit
	SoundType     uint8 //1bit
	AACPacketType uint8 //8bit, for aac
}

/*************************************************/
/* audio sequence header
/*************************************************/
//AudioSpecificConfig is explained in ISO 14496-3, P52
//ObjectType
//1:AAC MAIN, 2:AAC LC, 3:AAC SSR, 4:AAC LTP, 5:SBR,
//29:PS,
//SamplingIdx
//0:96000, 1:88200, 3:64000, 4:44100, 5:32000,
//6:24000, 7:22050, 8:16000, 9:12000, 10:11025,
//11:8000, 12:7350,
//ChannelNum
//1:单声道, 2:双声道,
//FrameLenFlag
//0:1024个采样点为一帧,
//5+4+4+3=16bit
type AudioSpecificConfig struct {
	ObjectType      uint8 //5bit
	SamplingIdx     uint8 //4bit
	ChannelNum      uint8 //4bit
	FrameLenFlag    uint8 //1bit
	DependCoreCoder uint8 //1bit, 一般为0
	ExtensionFlag   uint8 //1bit, 一般为0
}

func AudioHandle(s *RtmpStream, c *Chunk) error {
	var err error
	if len(c.MsgData) < 2 {
		err = fmt.Errorf("audio body has no data")
		return err
	}
	SoundFormat := (c.MsgData[0] & 0xF0) >> 4 // 4bit
	//SoundRate := (c.MsgData[0] & 0xC) >> 2    // 2bit
	//SoundSize := (c.MsgData[0] & 0x2) >> 1    // 1bit
	//SoundType := c.MsgData[0] & 0x1           // 1bit

	// TODO: 需要支持mp3, SoundFormat == 2
	if SoundFormat == 10 {
		s.AudioCodecType = "AAC"
		//s.log.Println("SoundFormat is AAC")
	} else {
		err = fmt.Errorf("untreated SoundFormat %d", SoundFormat)
		s.log.Println(err)
		return err
	}

	//0: AAC sequence header
	//1: AAC raw
	AACPacketType := c.MsgData[1]
	//先10 3 1 1 0, 以后都是10 3 1 1 1, aac, 44100, 16, stereo
	//  10 3 1 1 0,         10 1 1 0 1
	//s.log.Println(SoundFormat, SoundRate, SoundSize, SoundType, AACPacketType)
	//s.log.Printf("audioData:%x", c.MsgData)

	switch AACPacketType {
	case 0:
		s.log.Println("This frame is AAC sequence header")
		if len(c.MsgData) < 4 {
			err = fmt.Errorf("AAC sequence header has no data")
			return err
		}
		c.DataType = "AudioHeader"

		//0xaf 0x00 0x12 0x10
		//0101 11 1 1, 00000000, 00010 0100 0010 0 0 0
		//AudioSpecificConfig is explained in ISO 14496-3
		var AacC AudioSpecificConfig
		AacC.ObjectType = (c.MsgData[2] & 0xF8) >> 3 // 5bit
		AacC.SamplingIdx =
			((c.MsgData[2] & 0x7) << 1) | (c.MsgData[3] >> 7) // 4bit
		AacC.ChannelNum = (c.MsgData[3] & 0x78) >> 3     // 4bit
		AacC.FrameLenFlag = (c.MsgData[3] & 0x4) >> 2    // 1bit
		AacC.DependCoreCoder = (c.MsgData[3] & 0x2) >> 1 // 1bit
		AacC.ExtensionFlag = c.MsgData[3] & 0x1          // 1bit
		// 2, 4, 2, 0(1024), 0, 0
		s.log.Printf("%#v", AacC)
		s.AacC = &AacC

		if AacC.ObjectType == 31 {
			s.log.Printf("aac header more than 2 bytes, since ObjectType is equal to 31")
		}
		//s.GopCache.AudioHeader = c
		s.GopCache.AudioHeader.Store(s.Key, c)
	case 1:
		// Raw AAC frame data
		//s.log.Println("This frame is AAC raw")
		if s.FirstAudioTs == 0 {
			s.FirstAudioTs = c.Timestamp
			s.PrevAudioTs = c.Timestamp
		} else {
			//视频帧率60fps, 帧间隔1000/60=16.7ms
			//视频帧率25fps, 帧间隔1000/25=  40ms
			//视频帧率20fps, 帧间隔1000/20=  50ms
			//视频帧率 2fps, 帧间隔1000/ 5= 500ms
			//视频帧率 1fps, 帧间隔1000/ 5=1000ms
			//音画相差400ms, 人类就能明显感觉到不同步
			if c.Timestamp >= s.PrevAudioTs {
				s.TotalAudioDelta += c.Timestamp - s.PrevAudioTs
			}

			s.AudioTsDifValue = c.Timestamp - s.PrevAudioTs
			if s.AudioTsDifValue > 500 {
				s.log.Printf("bigjump: c.Ts(%d) - s.Pats(%d) = AtsDv(%d)", c.Timestamp, s.PrevAudioTs, s.AudioTsDifValue)
			}
			s.PrevAudioTs = c.Timestamp
		}

		//judge abnormal timestamp, don't use first A/V 250 packet to calculte, and only calculte 4 times
		s.PktNum++
		var deltaCal uint32
		if s.PktNum > conf.AdjustPktNum && s.CalNumAudio < AdjustSeqNum {
			var DurationAudio int64
			s.PktNumAudio++
			cTime := utils.GetTimestamp("ms")
			if s.StartTimeAudio == 0 {
				s.StartTimeAudio = cTime
			}
			DurationAudio = cTime - s.StartTimeAudio
			//use 10 seconds audio packet to calculate
			if DurationAudio >= 10000 {
				s.CalNumAudio++
				deltaCal = uint32(DurationAudio / int64(s.PktNumAudio))

				if (s.TotalAudioDelta / s.PktNumAudio) < 2*deltaCal {
					//normal
					s.SeqNumAudio = 0
				} else {
					//abnormal timestamp, need adjust
					s.DeltaCalAudio[s.SeqNumAudio] = deltaCal
					s.SeqNumAudio++
					s.log.Printf("aac abnormal delta timestamp:%d, cal delta timestamp:%d, seq num:%d >= 3 wil adjust", s.TotalAudioDelta/s.PktNumAudio, deltaCal, s.SeqNumAudio)
				}

				s.StartTimeAudio = cTime
				s.PktNumAudio = 0
				s.TotalAudioDelta = 0
			}
		} else {
			s.TotalAudioDelta = 0
		}
		//when three successive times happened abnormal delta timestamp, should adjust
		if conf.AdjustDts == true && s.SeqNumAudio >= AdjustSeqNum {
			if s.FirstAudioAdust == 0 {
				s.PrevAudioAdust = c.Timestamp
			} else {
				if s.DeltaCalVideo[0] == s.DeltaCalVideo[1] {
					deltaCal = s.DeltaCalVideo[0]
				} else if s.DeltaCalVideo[1] == s.DeltaCalVideo[2] {
					deltaCal = s.DeltaCalVideo[1]
				} else if s.DeltaCalVideo[0] == s.DeltaCalVideo[2] {
					deltaCal = s.DeltaCalVideo[0]
				} else {
					deltaCal = (s.DeltaCalVideo[0] + s.DeltaCalVideo[1] + s.DeltaCalVideo[2]) / AdjustSeqNum
				}
				c.Timestamp = s.PrevAudioAdust + deltaCal
			}
			s.FirstAudioAdust++
			s.PrevAudioAdust = c.Timestamp
		}

		c.DataType = "AudioAacFrame"
		//c.Fmt = c.FmtFirst
		if s.GopCache.MediaData.Len() > 0 {
			goplocks.Lock()
			s.GopCache.MediaData.PushBack(c)
			goplocks.Unlock()
		}
		//s.CountNum++
		//s.log.Println(s.CountNum)
		//s.log.Printf("%x", c.MsgData)
	default:
		err = fmt.Errorf("untreated AACPacketType %d", AACPacketType)
		s.log.Println(err)
		return err
	}
	return nil
}
