package main

import (
	"fmt"
	"strings"
)

/*************************************************/
/* Rtsp媒体数据走内存发送给自己RtmpServer
/*************************************************/
func RtspMem2RtmpServer(rs *RtspStream) {

}

/*************************************************/
/* Rtsp媒体数据走网络发送给别的RtmpServer
/*************************************************/
//0 建立连接
//1 发送metadata(amf0编码)
//2 发送video sequence header(1B+1B+3B+H264SeqHeader有vps/sps/pps)
//  FrameType(4bit), CodecId(4bit), AVCPacketType(8bit), CompositionTime(24bit)
//  FrameType: 1:keyframe, 2:b/pframe
//  CodecId: 7:h264, 12:h265
//  AVCPacketType: 0:AVC sequence header, 1:AVC NALU, 2:AVC end of sequence
//3 发送audio sequence header(1B+1B+aacSeqHeader)
//4 发送video data(1B+1B+3B+4字节长度+naluHeader+naluData)
//5 发送audio data(1B+1B+不带adts的aacData)
//6 音频要统一转码为aac 11025Hz s16 单声道
func RtmpSendMetadata(rs *RtspStream, s *Stream) error {
	//onMetaData define video_file_format_spec_v10.pdf
	s.log.Println("<== Send Metadata")
	info := make(Object)
	info["server"] = AppName
	//info["videodatarate"] = 0
	info["videocodecid"] = 7
	//info["videocodecid"] = 12 //FIXME
	//info["framerate"] = 25
	info["width"] = rs.Width
	info["height"] = rs.Height
	info["audiocodecid"] = 10
	info["audiosamplerate"] = 11025
	info["audiosamplesize"] = 16
	info["stereo"] = false
	s.log.Printf("%#v", info)

	d, _ := AmfMarshal(s, "@setDataFrame", "onMetaData", info)
	rs.RtmpMetaData = d

	rc := CreateMessage(MsgTypeIdDataAmf0, uint32(len(d)), d)
	err := MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func RtmpSendVideoSeqHeader(rs *RtspStream, s *Stream) error {
	spsData := rs.SpsData
	ppsData := rs.PpsData
	if spsData == nil {
		spsData = rs.Sdp.SpsData
		ppsData = rs.Sdp.PpsData
	}
	s.log.Printf("spsData:%x", spsData)
	s.log.Printf("ppsData:%x", ppsData)

	var AvcC AVCDecoderConfigurationRecord
	AvcC.ConfigurationVersion = 0x01 //8bit, 通常0x01
	//spsData第1字节是naluHeader
	AvcC.AVCProfileIndication = uint8(spsData[1]) //8bit, 值同第1个sps的第1字节
	AvcC.ProfileCompatibility = uint8(spsData[2]) //8bit, 值同第1个sps的第2字节
	AvcC.AVCLevelIndication = uint8(spsData[3])   //8bit, 值同第1个sps的第3字节
	AvcC.Reserved0 = 0x3f                         //6bit, 保留全1
	AvcC.LengthSizeMinuxOne = 0x03                //2bit, 通常这个值为3, 即NAL码流中使用3+1=4字节表示NALU的长度
	AvcC.Reserved1 = 0x07                         //3bit, 保留全1
	AvcC.NumOfSps = 0x01                          //5bit, 通常为1
	AvcC.SpsSize = uint16(len(spsData))           //16bit, SpsData长度
	AvcC.SpsData = spsData                        //xByte
	AvcC.NumOfPps = 0x01                          //8bit, 通常为1
	AvcC.PpsSize = uint16(len(ppsData))           //16bit, PpsData长度
	AvcC.PpsData = ppsData                        //yByte
	//s.log.Printf("%#v", AvcC)

	var vp RtmpVideoDataHeader
	vp.FrameType = 1 //4bit, 1:iFrame, 2:p/bFrame
	vp.CodecID = 7   //4bit, 7:h264, 12:h265
	if strings.Contains(strings.ToLower(rs.Sdp.VideoPayloadTypeStr), strings.ToLower("H265")) {
		vp.CodecID = 12
	}
	vp.AvcPacketType = 0   //8bit, 0:seqHeader, 1:nalu, 2:end
	vp.CompositionTime = 0 //24bit, 没啥用 等于0就行

	var n int
	l := 5 + 11 + AvcC.SpsSize + AvcC.PpsSize
	d := make([]byte, l)

	d[n] = ((vp.FrameType & 0xf) << 4) | (vp.CodecID & 0xf)
	n += 1
	d[n] = vp.AvcPacketType
	n += 1
	Uint24ToByte(vp.CompositionTime, d[n:n+3], BE)
	n += 3

	d[n] = AvcC.ConfigurationVersion
	n += 1
	d[n] = AvcC.AVCProfileIndication
	n += 1
	d[n] = AvcC.ProfileCompatibility
	n += 1
	d[n] = AvcC.AVCLevelIndication
	n += 1
	d[n] = ((AvcC.Reserved0 & 0x3f) << 2) | (AvcC.LengthSizeMinuxOne & 0x3)
	n += 1
	d[n] = (AvcC.Reserved1 & 0xe0) | (AvcC.NumOfSps & 0x1f)
	n += 1
	Uint16ToByte(AvcC.SpsSize, d[n:n+2], BE)
	n += 2
	copy(d[n:n+int(AvcC.SpsSize)], spsData)
	n += int(AvcC.SpsSize)
	d[n] = AvcC.NumOfPps
	n += 1
	Uint16ToByte(AvcC.PpsSize, d[n:n+2], BE)
	n += 2
	copy(d[n:n+int(AvcC.PpsSize)], ppsData)
	n += int(AvcC.SpsSize)

	s.log.Printf("SpsData:%x", spsData)
	s.log.Printf("PpsData:%x", ppsData)
	s.log.Printf("AvcCData:%x", d)

	rs.RtmpVideoSeqHeader = d

	rc := CreateMessage(MsgTypeIdVideo, uint32(len(d)), d)
	err := MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}

	vp.FrameType = 1     //4bit, 1:iFrame, 2:p/bFrame
	vp.AvcPacketType = 1 //8bit, 0:seqHeader, 1:nalu, 2:end
	n = 0
	rs.RtmpVideoIframePre[n] = ((vp.FrameType & 0xf) << 4) | (vp.CodecID & 0xf)
	n += 1
	rs.RtmpVideoIframePre[n] = vp.AvcPacketType
	n += 1
	Uint24ToByte(vp.CompositionTime, rs.RtmpVideoIframePre[n:n+3], BE)
	n += 3

	vp.FrameType = 2     //4bit, 1:iFrame, 2:p/bFrame
	vp.AvcPacketType = 1 //8bit, 0:seqHeader, 1:nalu, 2:end
	n = 0
	rs.RtmpVideoPframePre[n] = ((vp.FrameType & 0xf) << 4) | (vp.CodecID & 0xf)
	n += 1
	rs.RtmpVideoPframePre[n] = vp.AvcPacketType
	n += 1
	Uint24ToByte(vp.CompositionTime, rs.RtmpVideoIframePre[n:n+3], BE)
	n += 3
	return nil
}

func AudioGetSamplingIdx(s int) int {
	switch s {
	case 96000:
		return 0
	case 88200:
		return 1
	case 64000:
		return 3
	case 44100:
		return 4
	case 32000:
		return 5
	case 24000:
		return 6
	case 22050:
		return 7
	case 16000:
		return 8
	case 12000:
		return 9
	case 11025:
		return 10
	case 8000:
		return 11
	case 7350:
		return 12
	default:
		return 10
	}
}

func RtmpSendAudioSeqHeader(rs *RtspStream, s *Stream) error {
	var adh RtmpAudioDataHeader
	//FIXME 依据情况设置参数
	adh.SoundFormat = 10  //4bit, 2:mp3, 10:aac
	adh.SoundRate = 1     //2bit, 0:5.5kHz, 1:11kHz, 2:22kHz, 3:44kHz
	adh.SoundSize = 1     //1bit, 0:8bit, 1:16bit
	adh.SoundType = 0     //1bit, 0:mono, 1:stereo
	adh.AACPacketType = 0 //8bit, 0:AacSeqHeader, 1:AacRaw

	AacC := &AudioSpecificConfig{}
	if rs.Sdp.AacC != nil {
		AacC = rs.Sdp.AacC
	} else {
		AacC.ObjectType = 0x2 //5bit
		s := rs.Sdp.AudioClockRate
		AacC.SamplingIdx = uint8(AudioGetSamplingIdx(s)) //4bit
		AacC.ChannelNum = uint8(rs.Sdp.AudioChannelNum)  //4bit
		AacC.FrameLenFlag = 0                            //1bit
		AacC.DependCoreCoder = 0                         //1bit
		AacC.ExtensionFlag = 0                           //1bit
	}

	var n int
	l := 2 + 2
	d := make([]byte, l)

	d[n] = ((adh.SoundFormat & 0xf) << 4) | ((adh.SoundRate & 0x3) << 2) | ((adh.SoundSize & 0x1) << 1) | ((adh.SoundType & 0x1) << 0)
	n += 1
	d[n] = adh.AACPacketType
	n += 1

	d[n] = ((AacC.ObjectType & 0x1f) << 3) | ((AacC.SamplingIdx & 0xf) >> 1)
	n += 1
	d[n] = ((AacC.SamplingIdx & 0x1) << 7) | ((AacC.ChannelNum & 0xf) << 3) | ((AacC.FrameLenFlag & 0x1) << 2) | ((AacC.DependCoreCoder & 0x1) << 1) | (AacC.ExtensionFlag & 0x1)
	n += 1

	rs.RtmpAudioSeqHeader = d

	rc := CreateMessage(MsgTypeIdAudio, uint32(len(d)), d)
	err := MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}

	adh.AACPacketType = 1 //8bit, 0:AacSeqHeader, 1:AacRaw
	n = 0
	rs.RtmpAudioAacPre[n] = ((adh.SoundFormat & 0xf) << 4) | ((adh.SoundRate & 0x3) << 2) | ((adh.SoundSize & 0x1) << 1) | ((adh.SoundType & 0x1) << 0)
	n += 1
	rs.RtmpAudioAacPre[n] = adh.AACPacketType
	n += 1
	return nil
}

func RtmpSendMediaData(rs *RtspStream, s *Stream, p *AvPacket) error {
	var n, l, sl, dl int
	var d []byte
	var TypeId uint32
	var err error

	switch p.Type {
	case "VideoKeyFrame":
		TypeId = MsgTypeIdVideo
		sl = len(rs.SeiData)
		dl = len(p.Data)
		l = 5 + 4 + dl
		if rs.SeiData != nil {
			l = 5 + 4 + sl + 4 + dl
		}
		d = make([]byte, l)
		copy(d[n:n+5], rs.RtmpVideoIframePre[:])
		n += 5
		if rs.SeiData != nil {
			Uint32ToByte(uint32(sl), d[n:n+4], BE)
			n += 4
			copy(d[n:], rs.SeiData)
			n += sl
		}
		Uint32ToByte(uint32(dl), d[n:n+4], BE)
		n += 4
		copy(d[n:], p.Data)
		n += dl
	case "VideoInterFrame":
		TypeId = MsgTypeIdVideo
		dl = len(p.Data)
		l = 5 + 4 + dl
		d = make([]byte, l)
		copy(d[n:n+5], rs.RtmpVideoPframePre[:])
		n += 5
		Uint32ToByte(uint32(dl), d[n:n+4], BE)
		n += 4
		copy(d[n:], p.Data)
	case "AudioAacFrame":
		TypeId = MsgTypeIdAudio
		l = 2 + len(p.Data)
		d = make([]byte, l)
		copy(d[n:n+2], rs.RtmpAudioAacPre[:])
		n += 2
		copy(d[n:], p.Data)
	case "AudioG711aFrame":
		s.log.Println("send AudioG711a Frame")
	case "AudioG711uFrame":
		s.log.Println("send AudioG711u Frame")
	default:
		err = fmt.Errorf("undefined pType=%s", p.Type)
		s.log.Println(err)
		return err
	}

	rc := CreateMessage(TypeId, uint32(len(d)), d)
	rc.Timestamp = p.Timestamp
	err = MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func RtspNet2RtmpServer(rs *RtspStream) {
	if rs.Rqst == nil {
		rs.Rqst = &RtspRqst{}
		rs.Rqst.PushIp = "127.0.0.1"
		rs.Rqst.PushPort = "1935"
		rs.Rqst.PushApp = "live"
		rs.Rqst.PushUrl = "rtmp://127.0.0.1:1935/live/" + rs.StreamId
	}

	s, err := RtmpPusher(rs.Rqst.PushIp, rs.Rqst.PushPort, rs.Rqst.PushApp, rs.StreamId)
	if err != nil {
		rs.log.Println(err)
		return
	}
	rs.log.Printf("start push %s", rs.Rqst.PushUrl)

	var p *AvPacket
	var ok bool
	for {
		p, ok = <-rs.AvPkt2RtmpChan
		if ok == false {
			rs.log.Printf("%s RtspNet2RtmpServer() stop", rs.StreamId)
			return
		}

		if rs.RtmpMetaData == nil {
			RtmpSendMetadata(rs, s)
			RtmpSendVideoSeqHeader(rs, s)
			RtmpSendAudioSeqHeader(rs, s)
		}

		l := len(p.Data)
		if l > 10 {
			l = 10
		}
		//rs.log.Printf("pType:%s, pTs:%d, pDataLen:%d pData:%x", p.Type, p.Timestamp, len(p.Data), p.Data[:l])
		RtmpSendMediaData(rs, s, p)
	}
}
