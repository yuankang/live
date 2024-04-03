package main

import (
	"fmt"
	"net"
	"time"
	"utils"
)

func Frame2Chunk(s *Stream, p AvPacket) Chunk {
	var st time.Time
	var div time.Duration
	var nis []NaluInfo
	var fl int
	var c Chunk

	if p.Type == "VideoKeyFrame" || p.Type == "VideoInterFrame" {
		st = time.Now()
		nis, fl = FindAnnexbStartCode(p.Data)
		div = time.Since(st)
		s.log.Printf("DataLen:%d, NaluNum:%d, useTime:%v", len(p.Data), len(nis), div)
		for i := 0; i < len(nis); i++ {
			s.log.Printf("NaluIdx:%d, %#v", i, nis[i])
		}

		//h264的annexB格式: startCode(3/4字节) + NaluData
		//h264的avcc格式: NaluLen(4字节) + NaluData
		//rtp(annexb),  rtmp(avcc), flv(avcc)???,  ts(annexb)
		s.log.Printf("%x", p.Data[:10])
		p.Data = Annexb2Avcc(p.Data, nis, fl)
		s.log.Printf("%x", p.Data[:10])
	}

	//CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	c.Fmt = 0
	c.Csid = 3
	c.Timestamp = p.Timestamp
	c.MsgLength = uint32(len(p.Data))
	c.MsgTypeId = MsgTypeIdVideo
	c.MsgStreamId = 0
	c.MsgData = p.Data
	c.DataType = p.Type
	return c
}

/*************************************************/
/* Gb28181媒体数据走内存发送给自己RtmpServer
/*************************************************/
func Gb281812Mem2RtmpServer(s *Stream) {
}

/*************************************************/
/* Gb28181媒体数据走网络发送给别的RtmpServer
/*************************************************/
func RtmpConn(s *Stream) (*Stream, error) {
	c, err := net.Dial("tcp", "127.0.0.1:1935")
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	//defer c.Close()

	sm, err := NewStream(c)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}

	sm.Type = "RtmpPusher"
	sm.RemoteAddr = "127.0.0.1:1935"
	sm.RemoteIp = "127.0.0.1"
	sm.RemotePort = "1935"
	sm.App = s.App
	sm.StreamId = s.StreamId

	fn := fmt.Sprintf("%s/%s/publish_rtmp_%s.log", conf.Log.StreamLogPath, sm.StreamId, utils.GetYMD())
	StreamLogRename(sm.LogFn, fn)

	sm.log.Println("==============================")
	//sm.log.Printf("RecFile:%s", sm.RecRtmpFn)

	if err := RtmpHandshakeClient(sm); err != nil {
		sm.log.Println(err)
		return nil, err
	}
	sm.log.Println("RtmpServerHandshake ok")

	cs := uint32(4096)
	sm.log.Printf("<== Set ChunkSize = %d", cs)
	d := Uint32ToByte(cs, nil, BE)
	cks := CreateMessage(MsgTypeIdSetChunkSize, 4, d)
	err = MessageSplit(sm, &cks, false)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	sm.RemoteChunkSize = cs

	err = SendConnMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	sm.log.Println("SendConnMsg() ok")

	err = SendCreateStreamMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	sm.log.Println("SendCreateStreamMsg() ok")

	err = SendPublishMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	sm.log.Println("SendPublishMsg() ok")
	return sm, nil
}

func AudioHandler(s, sm *Stream, p *PsPacket) error {
	return nil
}

func CreateSendMetaData(sm *Stream) ([]byte, error) {
	sm.log.Println("<== Send MetaData")
	info := make(Object)
	info["server"] = AppName
	info["version"] = AppVersion
	/*
		info["fileSize"] = 0
		info["duration"] = 0
		info["videocodecid"] = 7
		info["height"] = 1280
		info["width"] = 720
		info["framerate"] = 15
		info["audiocodecid"] = 10
		info["audiosamplerate"] = 11025
		info["audiosamplesize"] = 16
		info["stereo"] = false
	*/
	sm.log.Printf("MetaData:%#v", info)

	d, err := AmfMarshal(sm, "@setDataFrame", "onMetaData", info)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	//sm.log.Printf("MetaData:%s", string(d)) //有乱码

	rc := CreateMessage(MsgTypeIdDataAmf0, uint32(len(d)), d)
	err = MessageSplit(sm, &rc, true)
	if err != nil {
		sm.log.Println(err)
		return nil, err
	}
	return d, nil
}

//1+1+3+n=5+n
type RtmpVideoTag struct {
	FrameType       uint8  //4bit, 1 keyframe, 2 InterFrame
	CodecID         uint8  //4bit, 7 h264, 12 h265
	AVCPacketType   uint8  //8bit, 0 AVC sequence header, 1 AVC data
	CompositionTime uint32 //24bit, xxx
	Data            []byte //5+nByte
}

func CreateAvcSequenceHeader(s *Stream, sm *Stream) ([]byte, error) {
	var rvt RtmpVideoTag
	rvt.FrameType = 1
	rvt.CodecID = 7
	rvt.AVCPacketType = 0
	rvt.CompositionTime = 0

	var AvcC AVCDecoderConfigurationRecord
	AvcC.ConfigurationVersion = 0x01
	AvcC.AVCProfileIndication = s.SpsData[0]
	AvcC.ProfileCompatibility = s.SpsData[1]
	AvcC.AVCLevelIndication = s.SpsData[2]
	AvcC.Reserved0 = 0x3f
	AvcC.LengthSizeMinuxOne = 0x3
	AvcC.Reserved1 = 0x7
	AvcC.NumOfSps = 1
	AvcC.SpsSize = uint16(len(s.SpsData))
	//AvcC.SpsData = s.SpsData
	AvcC.NumOfPps = 1
	AvcC.PpsSize = uint16(len(s.PpsData))
	//AvcC.PpsData = s.PpsData

	var i uint16
	d := make([]byte, 5+11+AvcC.SpsSize+AvcC.PpsSize)

	d[i] = rvt.FrameType<<4 | rvt.CodecID
	i++
	d[i] = rvt.AVCPacketType
	i++
	Uint24ToByte(rvt.CompositionTime, d[i:i+3], BE)
	i += 3

	d[i] = AvcC.ConfigurationVersion
	i++
	d[i] = AvcC.AVCProfileIndication
	i++
	d[i] = AvcC.ProfileCompatibility
	i++
	d[i] = AvcC.AVCLevelIndication
	i++
	d[i] = AvcC.Reserved0<<2 | AvcC.LengthSizeMinuxOne
	i++
	d[i] = AvcC.Reserved1<<5 | AvcC.NumOfSps
	i++
	Uint16ToByte(AvcC.SpsSize, d[i:i+2], BE)
	i += 2
	copy(d[i:], s.SpsData)
	i += AvcC.SpsSize

	d[i] = AvcC.NumOfPps
	i++
	Uint16ToByte(AvcC.PpsSize, d[i:i+2], BE)
	i += 2
	copy(d[i:], s.PpsData)
	i += AvcC.PpsSize
	return d, nil
}

//此处h264数据是annexB格式 要转为 rtmp需要的avcc格式(NaluLen4字节)
func CreateAvcFrame(sm *Stream, ft int, ni *NaluInfo, sei []byte) []byte {
	var rvt RtmpVideoTag
	rvt.FrameType = uint8(ft)
	rvt.CodecID = 7
	rvt.AVCPacketType = 1
	rvt.CompositionTime = 0

	//这里默认 都是00000001 没有000001
	//data中含有4字节开始码, 去掉00000001 改为 4字节长度
	vLen := len(ni.Data)
	var seiLen int
	if sei != nil {
		seiLen = len(sei)
	}

	var i int
	var d []byte
	if sei != nil {
		d = make([]byte, 5+8+seiLen+vLen)
	} else {
		d = make([]byte, 5+4+vLen)
	}

	d[i] = rvt.FrameType<<4 | rvt.CodecID
	i++
	d[i] = rvt.AVCPacketType
	i++
	Uint24ToByte(rvt.CompositionTime, d[i:i+3], BE)
	i += 3

	if sei != nil {
		Uint32ToByte(uint32(seiLen), d[i:i+4], BE)
		i += 4
		copy(d[i:], sei)
		i += seiLen
	}

	Uint32ToByte(uint32(vLen), d[i:i+4], BE)
	i += 4
	copy(d[i:], ni.Data)
	i += vLen
	return d
}

func HandleAvcSequenceHeader(s, sm *Stream, p *PsPacket, nis []*NaluInfo) error {
	var ni *NaluInfo
	var ck *Chunk

	for i := 0; i < len(nis); i++ {
		ni = nis[i]
		sm.log.Printf("i=%d, Type=%s, Len=%d, Num(0x00)=%d, Pos=%d, Data=%x", i, ni.Type, ni.ByteLen, ni.ByteNum, ni.BytePos, ni.Data[:50])

		switch ni.Type {
		case "vps":
			if utils.SliceEqual(s.VpsData, ni.Data) {
				continue
			}
			s.VpsChange = true
			sm.log.Printf("VpsDataOld:%x", s.VpsData)
			sm.log.Printf("VpsDataNew:%x", ni.Data)
			s.VpsData = ni.Data
		case "sps":
			if utils.SliceEqual(s.SpsData, ni.Data) {
				continue
			}
			s.SpsChange = true
			sm.log.Printf("SpsDataOld:%x", s.SpsData)
			sm.log.Printf("SpsDataNew:%x", ni.Data)
			s.SpsData = ni.Data
			sps, err := SpsParse0(sm, s.SpsData)
			if err != nil {
				sm.log.Println(err)
				continue
			}
			//sm.log.Printf("%#v", sps)
			s.Width = int((sps.PicWidthInMbsMinus1 + 1) * 16)
			s.Height = int((sps.PicHeightInMapUnitsMinus1 + 1) * 16)
			sm.log.Printf("video width=%d, height=%d", s.Width, s.Height)
		case "pps":
			if utils.SliceEqual(s.PpsData, ni.Data) {
				continue
			}
			s.PpsChange = true
			sm.log.Printf("PpsDataOld:%x", s.PpsData)
			sm.log.Printf("PpsDataNew:%x", ni.Data)
			s.PpsData = ni.Data
		case "sei":
			if utils.SliceEqual(s.SeiData, ni.Data) {
				continue
			}
			s.SeiChange = true
			sm.log.Printf("SeiDataOld:%x", s.SeiData)
			sm.log.Printf("SeiDataNew:%x", ni.Data)
			s.SeiData = ni.Data
		case "ifrm", "pfrm":
		default:
			sm.log.Printf("undefine nalu type")
		}
	}

	//vps/sps/pps变更, 需发送video sequence header(h264含sps+pps, h265含vps+sps+pps)
	if s.VpsChange == true || s.SpsChange == true || s.PpsChange == true {
		sm.log.Println("<-- Send AvcSequenceHeader")
		sm.log.Printf("VpsChange=%t, SpsChange=%t, PpsChange=%t", s.VpsChange, s.SpsChange, s.PpsChange)

		//TODO 这里要区分h264 h265
		s.AvcSH, _ = CreateAvcSequenceHeader(s, sm)

		ck = CreateMessage0(MsgTypeIdVideo, uint32(len(s.AvcSH)), s.AvcSH)
		ck.Csid = 3
		ck.Timestamp = p.Timestamp / 90

		sm.log.Printf("MsgLen=%d, MsgData=%x", len(s.AvcSH), s.AvcSH)
		err := MessageSplit(sm, ck, true)
		if err != nil {
			sm.log.Println(err)
			return err
		}

		s.VpsChange = false
		s.SpsChange = false
		s.PpsChange = false
	}
	return nil
}

//1 拆分出nalu, h264关键帧(sps+pps+sei+ifrm), 非关键帧(pfrm)
//2 依据sps/pps, 生成或更新avc sequence header, 并发送
//3 依据sei, 生成并发送视频数据 annexB格式转avcc格式
func VideoHandler(s, sm *Stream, p *PsPacket) error {
	nis, err := FindAnnexbStartCode1(p.Data, s.VideoCodecType)
	if err != nil {
		sm.log.Println(err)
		return err
	}

	err = HandleAvcSequenceHeader(s, sm, p, nis)
	if err != nil {
		sm.log.Println(err)
		return err
	}

	var ni *NaluInfo
	var ds, d []byte
	var ck *Chunk
	for i := 0; i < len(nis); i++ {
		ni = nis[i]
		if ni.Type == "vps" || ni.Type == "sps" || ni.Type == "pps" || ni.Type == "sei" {
			continue
		}

		if ni.Type == "ifrm" && s.SeiChange == true {
			//sei变更, 需发送video data(sei+ifrm)
			s.SeiChange = false
			d = CreateAvcFrame(sm, 1, ni, s.SeiData)
		} else if ni.Type == "ifrm" || ni.Type == "pfrm" {
			//sei不变, 需发送video data(ifrm或pfrm)
			d = CreateAvcFrame(sm, 2, ni, nil)
		}
		ds = append(ds, d...)
	}

	ck = CreateMessage0(MsgTypeIdVideo, uint32(len(ds)), ds)
	ck.Csid = 3
	ck.Timestamp = p.Timestamp / 90

	err = MessageSplit(sm, ck, false)
	if err != nil {
		sm.log.Println(err)
		return err
	}
	return nil
}

func GbNetPushRtmp(s *Stream) {
	sm, err := RtmpConn(s)
	if err != nil {
		s.log.Println(err)
		return
	}

	//TODO 此处metadata不包含音视频参数
	_, err = CreateSendMetaData(sm)
	if err != nil {
		s.log.Println(err)
		return
	}

	var p *PsPacket
	var ok bool
	for {
		p, ok = <-s.PsPktChan
		if ok == false {
			sm.log.Printf("%s, GbNetPushRtmp() stop", sm.StreamId)
			break
		}
		sm.log.Printf("PsType=%s, PsTs=%d, PsData=%x", p.Type, p.Timestamp, p.Data)

		switch p.Type {
		case "video":
			err = VideoHandler(s, sm, p)
		case "audio":
			err = AudioHandler(s, sm, p)
		}
		if err != nil {
			sm.log.Println(err)
		}
	}
}
