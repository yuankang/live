package main

import (
	"encoding/binary"
	"fmt"
	"net"
	utils "utilsGIT"
)

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
	var ah RtmpAudioDataHeader
	switch s.AudioCodecType {
	case "G711a":
		ah.SoundFormat = 7
	case "G711u":
		ah.SoundFormat = 8
	case "AAC":
		ah.SoundFormat = 10
	}
	ah.SoundRate = 0
	ah.SoundSize = 0
	ah.SoundType = 0

	i := 0
	d := make([]byte, 1+len(p.Data))

	d[i] = ah.SoundFormat<<4 | ah.SoundRate<<2 | ah.SoundSize<<1 | ah.SoundType
	i++
	copy(d[i:], p.Data)
	i += len(p.Data)

	ck := CreateMessage0(MsgTypeIdAudio, uint32(len(d)), d)
	ck.Csid = 3
	ck.Timestamp = p.Timestamp / 90

	//sm.log.Printf("<-- aLen=%d, aData=%x", len(d), d)
	err := MessageSplit(sm, ck, false)
	if err != nil {
		sm.log.Println(err)
		return err
	}
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

func CreateHevcSequenceHeader(s *Stream, sm *Stream) ([]byte, error) {
	var rvt RtmpVideoTag
	rvt.FrameType = 1
	rvt.CodecID = 7
	rvt.AVCPacketType = 0
	rvt.CompositionTime = 0

	//前5个字节上面已经处理，HEVC sequence header从第6个字节开始
	//1c 00 00 00 00
	//01 01 60 00 00 00 80 00 00 00
	//00 00 78 f0 00 fc fd f8 f8 00
	//00 ff 03 20 00 01 00 17 40 01
	//0c 01 ff ff 01 60 00 00 03 00
	//80 00 00 03 00 00 03 00 78 ac
	//09 21 00 01 00 3c 42 01 01 01
	//60 00 00 03 00 80 00 00 03 00
	//00 03 00 78 a0 02 80 80 2d 1f
	//e3 6b bb c9 2e b0 16 e0 20 20
	//20 80 00 01 f4 00 00 30 d4 39
	//0e f7 28 80 3d 30 00 44 de 00
	//7a 60 00 89 bc 40 22 00 01 00
	//09 44 01 c1 72 b0 9c 38 76 24
	hc := HEVCDecoderConfigurationRecord{
		ConfigurationVersion:             1,
		GeneralProfileSpace:              0,
		GeneralTierFlag:                  0,
		GeneralProfileIdc:                1,
		GeneralProfileCompatibilityFlags: 0,
		GeneralConstraintIndicatorFlags:  0,
		GeneralLevelIdc:                  30,
		MinSpatialSegmentationIdc:        0,
		ParallelismType:                  0,
		ChromaFormat:                     1,
		BitDepthLumaMinus8:               0,
		BitDepthChromaMinus8:             0,
		AvgFrameRate:                     0,
		ConstantFrameRate:                0,
		NumTemporalLayers:                0,
		TemporalIdNested:                 0,
		LengthSizeMinusOne:               3,
		NumOfArrays:                      0,
	}

	i := 0
	d := make([]byte, 5+11)

	d[i] = rvt.FrameType<<4 | rvt.CodecID
	i++
	d[i] = rvt.AVCPacketType
	i++
	Uint24ToByte(rvt.CompositionTime, d[i:i+3], BE)
	i += 3

	d[i] = hc.ConfigurationVersion
	d[i] = (hc.GeneralProfileSpace << 6) | (hc.GeneralTierFlag << 5) | hc.GeneralProfileIdc
	binary.BigEndian.PutUint32(d[i:], hc.GeneralProfileCompatibilityFlags)
	binary.BigEndian.PutUint64(d[i:], hc.GeneralConstraintIndicatorFlags)
	d[i] = hc.GeneralLevelIdc
	binary.BigEndian.PutUint16(d[i:], hc.MinSpatialSegmentationIdc)
	d[i] = (hc.ParallelismType << 6) | (hc.ChromaFormat << 2) | (hc.BitDepthLumaMinus8 >> 1)
	//d[i] = ((hc.BitDepthLumaMinus8 & 0x01) << 7) | (hc.BitDepthChromaMinus8 << 3) | (hc.AvgFrameRate >> 8)
	d[i] = byte(hc.AvgFrameRate)
	d[i] = (hc.ConstantFrameRate << 6) | (hc.NumTemporalLayers << 3) | (hc.TemporalIdNested << 2) | (hc.LengthSizeMinusOne >> 1)
	d[i] = ((hc.LengthSizeMinusOne & 0x01) << 7) | hc.NumOfArrays
	return d, nil
}

func CreateAvcSequenceHeader(s *Stream, sm *Stream) ([]byte, error) {
	var rvt RtmpVideoTag
	rvt.FrameType = 1
	rvt.CodecID = 7
	rvt.AVCPacketType = 0
	rvt.CompositionTime = 0

	var AvcC AVCDecoderConfigurationRecord
	AvcC.ConfigurationVersion = 0x01
	AvcC.AVCProfileIndication = s.SpsData[1]
	AvcC.ProfileCompatibility = s.SpsData[2]
	AvcC.AVCLevelIndication = s.SpsData[3]
	AvcC.Reserved0 = 0x3f
	AvcC.LengthSizeMinuxOne = 0x3
	AvcC.Reserved1 = 0x7
	AvcC.NumOfSps = 1
	AvcC.SpsSize = uint16(len(s.SpsData))
	//AvcC.SpsData = s.SpsData
	AvcC.NumOfPps = 1
	AvcC.PpsSize = uint16(len(s.PpsData))
	//AvcC.PpsData = s.PpsData

	i := 0
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
	i += int(AvcC.SpsSize)

	d[i] = AvcC.NumOfPps
	i++
	Uint16ToByte(AvcC.PpsSize, d[i:i+2], BE)
	i += 2
	copy(d[i:], s.PpsData)
	i += int(AvcC.PpsSize)
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
	var pl int

	for i := 0; i < len(nis); i++ {
		ni = nis[i]

		pl = len(ni.Data)
		if pl > 10 {
			pl = 10
		}
		sm.log.Printf("i=%d, Type=%s, Len=%d, Num(0x00)=%d, Pos=%d, Data=%x", i, ni.Type, ni.ByteLen, ni.ByteNum, ni.BytePos, ni.Data[:pl])

		switch ni.Type {
		case "vps":
			if utils.SliceEqual(s.VpsData, ni.Data) {
				continue
			}
			s.VpsChange = true
			sm.log.Printf("VpsDataOld:%x", s.VpsData)
			s.VpsData = ni.Data
			sm.log.Printf("VpsDataNew:%x", s.VpsData)
		case "sps":
			if utils.SliceEqual(s.SpsData, ni.Data) {
				continue
			}
			s.SpsChange = true
			sm.log.Printf("SpsDataOld:%x", s.SpsData)
			s.SpsData = ni.Data
			sm.log.Printf("SpsDataNew:%x", s.SpsData)
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
			s.PpsData = ni.Data
			sm.log.Printf("PpsDataNew:%x", s.PpsData)
		case "sei":
			if utils.SliceEqual(s.SeiData, ni.Data) {
				continue
			}
			s.SeiChange = true
			sm.log.Printf("SeiDataOld:%x", s.SeiData)
			s.SeiData = ni.Data
			sm.log.Printf("SeiDataNew:%x", s.SeiData)
		case "ifrm", "pfrm":
		default:
			//sm.log.Printf("unknow nalu type")
		}
	}

	//vps/sps/pps变更, 需发送video sequence header(h264含sps+pps, h265含vps+sps+pps)
	if s.VpsChange == true || s.SpsChange == true || s.PpsChange == true {
		sm.log.Println("<-- Send AvcSequenceHeader")
		sm.log.Printf("VpsChange=%t, SpsChange=%t, PpsChange=%t", s.VpsChange, s.SpsChange, s.PpsChange)

		//TODO 这里要区分h264 h265
		if s.VideoCodecType == "H264" {
			s.AvcSH, _ = CreateAvcSequenceHeader(s, sm)
		} else {
			s.AvcSH, _ = CreateHevcSequenceHeader(s, sm)
		}

		ck = CreateMessage0(MsgTypeIdVideo, uint32(len(s.AvcSH)), s.AvcSH)
		ck.Csid = 3
		ck.Timestamp = p.Timestamp / 90

		sm.log.Printf("<-- SeqHeadLen=%d, SeqHeadData=%x", len(s.AvcSH), s.AvcSH)
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
	nis, err := FindAnnexbStartCode(p.Data, s.VideoCodecType)
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
		if ni.Type == "vps" || ni.Type == "sps" || ni.Type == "pps" || ni.Type == "sei" || ni.Type == "unknow" {
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

	//sm.log.Printf("<-- vLen=%d, vData=%x", len(ds), ds[:50])
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
		//sm.log.Printf("PsType=%s, PsTs=%d, PsLen=%d, PsData=%x", p.Type, p.Timestamp, len(p.Data), p.Data)
		sm.log.Printf("PsType=%s, PsTs=%d, PsLen=%d", p.Type, p.Timestamp, len(p.Data))

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
