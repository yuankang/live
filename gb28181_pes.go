package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
)

/*************************************************/
/* pes
/*************************************************/
//PTS or DTS
//4+3+1+15+1+15+1=5byte
type OptTs struct {
	FixedValue1 uint8  //4bit, PTS:0x0010 or 0x0011, DTS:0x0001
	Ts32_30     uint8  //3bit, 33bit
	MarkerBit0  uint8  //1bit
	Ts29_15     uint16 //15bit
	MarkerBit1  uint8  //1bit
	Ts14_0      uint16 //15bit
	MarkerBit2  uint8  //1bit
}

//PtsDtsFlags uint8 // 2bit
//0x0 00, 没有PTS和DTS
//0x1 01, 禁止使用
//0x2 10, 只有PTS
//0x3 11, 有PTS 有DTS
//1+1+1=3byte
type OptPesHeader struct {
	FixedValue0            uint8  //2bit, 固定值0x2
	PesScramblingControl   uint8  //2bit, 加扰控制
	PesPriority            uint8  //1bit, 优先级
	DataAlignmentIndicator uint8  //1bit, 数据定位指示器
	Copyright              uint8  //1bit, 版本信息, 1为有版权, 0无版权
	OriginalOrCopy         uint8  //1bit, 原始或备份, 1为原始, 0为备份
	PtsDtsFlags            uint8  //2bit, 时间戳标志位
	EscrFlag               uint8  //1bit, 1表示PES头部有ESCR字段, 0表示没有
	EsRateFlag             uint8  //1bit, 1表示PES头部有ES_rate字段, 0表示没有
	DsmTrickModeFlag       uint8  //1bit, 1表示有1个8bit的track mode字段, 0表示没有
	AdditionalCopyInfoFlag uint8  //1bit, 1表示有additional_copy_info字段, 0表示没有
	PesCrcFlag             uint8  //1bit, 1表示PES包中有CRC字段, 0表示没有
	PesExtensionFlag       uint8  //1bit, 1表示PES头部中有extension字段存在, 0表示没有
	PesHeaderDataLength    uint8  //8bit, 表示后面还有x个字节, 之后就是负载数据. 指定在PES包头部中可选头部字段和任意的填充字节所占的总字节数, 可选字段的内容由上面的7个flag来控制
	Pts                    *OptTs //5byte
	Dts                    *OptTs //5byte
	StuffData              []byte //0xff
}

//3+3+3+5*1=14byte, 有pts
//3+3+3+5*2=19byte, 有pts和dts
type PesHeader struct {
	PacketStartCodePrefix uint32        //24bit, 固定值 0x000001
	StreamId              uint8         //8bit, 0xe0视频 0xc0音频
	PesPacketLength       uint16        //16bit, 包长度, 表示后面还有x个字节的数据，包括剩余的pes头数据和负载数据, 最大值65536, 0表示长度不限 通常为视频数据
	Oph                   *OptPesHeader //24bit
	Pts                   uint64        //不是包结构成员, 只是方便编码
	Dts                   uint64        //不是包结构成员, 只是方便编码
	Pcr                   uint64        //不是包结构成员, 只是方便编码
}

func ParseOptPesHeader(s *Stream, r *bytes.Reader) (*OptPesHeader, int) {
	var oph OptPesHeader
	var n int

	b8, _ := ReadUint8(r)
	n += 1
	oph.FixedValue0 = (b8 >> 6) & 0x3
	oph.PesScramblingControl = (b8 >> 4) & 0x3
	oph.PesPriority = (b8 >> 3) & 0x1
	oph.DataAlignmentIndicator = (b8 >> 2) & 0x1
	oph.Copyright = (b8 >> 1) & 0x1
	oph.OriginalOrCopy = (b8 >> 0) & 0x1
	b8, _ = ReadUint8(r)
	n += 1
	oph.PtsDtsFlags = (b8 >> 6) & 0x3
	oph.EscrFlag = (b8 >> 5) & 0x1
	oph.EsRateFlag = (b8 >> 4) & 0x1
	oph.DsmTrickModeFlag = (b8 >> 3) & 0x1
	oph.AdditionalCopyInfoFlag = (b8 >> 2) & 0x1
	oph.PesCrcFlag = (b8 >> 1) & 0x1
	oph.PesExtensionFlag = (b8 >> 0) & 0x1
	oph.PesHeaderDataLength, _ = ReadUint8(r)
	n += 1
	n += int(oph.PesHeaderDataLength)

	m := 0
	if oph.PtsDtsFlags == 0x2 || oph.PtsDtsFlags == 0x3 {
		oph.Pts, oph.Dts, m = ParseOptTs(s, r, &oph)
	}
	//TODO 如果出现一定要处理 否则从r中读数据会错位
	if oph.EscrFlag == 0x1 {
		s.log.Println("need todo something")
	}
	if oph.EsRateFlag == 0x1 {
		s.log.Println("need todo something")
	}
	if oph.DsmTrickModeFlag == 0x1 {
		s.log.Println("need todo something")
	}
	if oph.AdditionalCopyInfoFlag == 0x1 {
		s.log.Println("need todo something")
	}
	if oph.PesCrcFlag == 0x1 {
		s.log.Println("need todo something")
	}
	if oph.PesExtensionFlag == 0x1 {
		s.log.Println("need todo something")
	}

	pl := int(oph.PesHeaderDataLength) - m
	d, _ := ReadByte(r, uint32(pl))
	s.log.Printf("PesPadData:%x, len=%d", d, pl)
	//oph.StuffData = d

	s.log.Printf("%#v", oph)
	return &oph, n
}

func ParseOptTs(s *Stream, r *bytes.Reader, oph *OptPesHeader) (*OptTs, *OptTs, int) {
	var oPts OptTs
	var oDts OptTs
	var n int

	b8, _ := ReadUint8(r)
	n += 1
	oPts.FixedValue1 = (b8 >> 4) & 0xf
	oPts.Ts32_30 = (b8 >> 1) & 0x7
	oPts.MarkerBit0 = (b8 >> 0) & 0x1
	b16, _ := ReadUint16(r, 2, BE)
	n += 2
	oPts.Ts29_15 = (b16 >> 1) & 0x7fff
	oPts.MarkerBit1 = uint8((b16 >> 0) & 0x1)
	b16, _ = ReadUint16(r, 2, BE)
	n += 2
	oPts.Ts14_0 = (b16 >> 1) & 0x7fff
	oPts.MarkerBit2 = uint8((b16 >> 0) & 0x1)
	s.log.Printf("%#v, rLen=%d", oPts, n)

	if oph.PtsDtsFlags == 0x3 {
		b8, _ := ReadUint8(r)
		n += 1
		oDts.FixedValue1 = (b8 >> 4) & 0xf
		oDts.Ts32_30 = (b8 >> 1) & 0x7
		oDts.MarkerBit0 = (b8 >> 0) & 0x1
		b16, _ := ReadUint16(r, 2, BE)
		n += 2
		oDts.Ts29_15 = (b16 >> 1) & 0x7fff
		oDts.MarkerBit1 = uint8((b16 >> 0) & 0x1)
		b16, _ = ReadUint16(r, 2, BE)
		n += 2
		oDts.Ts14_0 = (b16 >> 1) & 0x7fff
		oDts.MarkerBit2 = uint8((b16 >> 0) & 0x1)
		s.log.Printf("%#v, rLen=%d", oDts, n)
	}
	return &oPts, &oDts, n
}

/*************************************************/
/* pes audio
/*************************************************/
func ParseAudio(s *Stream, pps *PsPacket, r *bytes.Reader) (*PsPacket, error) {
	s.log.Println("--->> ParseAudio start")
	var err error
	var ph PesHeader
	ph.PacketStartCodePrefix = 0x000001
	ph.StreamId = 0xc0

	ph.PesPacketLength, _ = ReadUint16(r, 2, BE)
	pps.UseNum += 2
	//ph.PesPacketLength 表示音频数据长度时 是准确值
	//s.log.Printf("PesPacketLength=%d, audio", ph.PesPacketLength)

	var n int
	ph.Oph, n = ParseOptPesHeader(s, r)
	pps.UseNum += n

	pp := &PsPacket{}
	pp.Type = "audio"
	pp.Timestamp = pps.Timestamp

	//这里获得去掉pes头的音频数据 也就是 g711或aac
	dl := ph.PesPacketLength - uint16(n)
	pp.Data, err = ReadByte(r, uint32(dl))
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	pps.UseNum += int(dl)
	s.log.Printf("AudioCodec:%s, DataLen:%d, PesPacketLength=%d", s.AudioCodecType, len(pp.Data), ph.PesPacketLength)
	s.log.Printf("%#v", ph)
	s.log.Println("--->> ParseAudio stop")
	return pp, nil
}

/*************************************************/
/* pes video
/*************************************************/
func ParseVideo(s *Stream, pps *PsPacket, r *bytes.Reader) (*PsPacket, error) {
	s.log.Println("--->> ParseVideo start")
	var err error
	var ph PesHeader
	ph.PacketStartCodePrefix = 0x000001
	ph.StreamId = 0xe0

	ph.PesPacketLength, _ = ReadUint16(r, 2, BE)
	pps.UseNum += 2
	//ph.PesPacketLength 表示视频数据长度时 不准确或为0 一般不用这个值
	//s.log.Printf("PesPacketLength=%d, video", ph.PesPacketLength)

	var n int
	ph.Oph, n = ParseOptPesHeader(s, r)
	pps.UseNum += n

	pp := &PsPacket{}
	pp.Type = "video"
	pp.Timestamp = pps.Timestamp

	//这里获得去掉pes头的视频数据 也就是nalu
	//FIXME: Marker=0用时间戳区分帧时, 视频后可能有时间相同的音频数据
	pp.Data, err = ioutil.ReadAll(r)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	pps.UseNum += len(pp.Data)
	s.log.Printf("VideoCodec:%s, DataLen:%d, PesPacketLength=%d", s.VideoCodecType, len(pp.Data), ph.PesPacketLength)
	s.log.Printf("%#v", ph)
	s.log.Println("--->> ParseVideo stop")
	return pp, nil
}

func VideoHandlerH264(s *Stream, psp *PsPacket, r *bytes.Reader) (int, error) {
	sc, err := ReadUint32(r, 4, BE)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	s.log.Printf("nalu StartCode=%#08x", sc)
	//psp.UseNum += 4

	d, err := ReadUint8(r)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	//psp.UseNum += 1

	//psp.Type = GetNaluType(d, "h264")

	var nh NaluHeader
	nh.ForbiddenZeroBit = (d >> 7) & 0x1
	nh.NalRefIdc = (d >> 5) & 0x3
	nh.NaluType = (d >> 0) & 0x1f
	s.log.Printf("%#v", nh)

	switch nh.NaluType {
	case 1: //P帧
		psp.Type = "VideoInterFrame"
	case 5: //IDR
		psp.Type = "VideoKeyFrame"
	case 6: //SEI
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:SEI")
	case 7: //SPS
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:SPS")
	case 8: //PPS
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:PPS")
	default:
		err := fmt.Errorf("NaluType:unknow, %d", nh.NaluType)
		s.log.Println(err)
	}
	return 0, err
}

func VideoHandlerH265(s *Stream, psp *PsPacket, r *bytes.Reader) (int, error) {
	sc, err := ReadUint32(r, 4, BE)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	s.log.Printf("nalu StartCode=%#08x", sc)
	//psp.UseNum += 4

	d, err := ReadByte(r, 2)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	//psp.UseNum += 2

	//psp.Type = GetNaluType(d, "h264")

	var nh NaluHeaderH265
	nh.ForbiddenZeroBit = d[0] >> 7
	nh.NalUnitType = (d[0] >> 1) & 0x3f
	nh.NuhLayerId = ((d[0] & 0x1) << 5) | (d[1]>>3)&0x1f
	nh.NuhTemporalIdPlus1 = d[1] & 0x7
	s.log.Printf("%#v", nh)

	switch nh.NalUnitType {
	case 1: //P帧
		psp.Type = "VideoInterFrame"
	case 19: //IDR
		psp.Type = "VideoKeyFrame"
	case 32: //VPS
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:VPS")
	case 33: //SPS
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:SPS")
	case 34: //PPS
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:PPS")
	case 35: //AUD
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:AUD")
	case 39: //PREFIX_SEI_NUT
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:SEI")
	case 40: //SUFFIX_SEI_NUT
		psp.Type = "VideoKeyFrame"
		s.log.Printf("NaluType:SEI")
	default:
		err := fmt.Errorf("NaluType:unknow, %d", nh.NalUnitType)
		s.log.Println(err)
	}
	return 0, err
}

/*************************************************/
/* rtp2rtmp
/*************************************************/
//VideoTag(VideoData+AvcVideoPacket)
//详见: video_file_format_spec_v10.pdf
type RtmpMsgData struct {
	FrameType       uint8  //4bit, 1keyframe, 2b/pframe
	CodecId         uint8  //4bit, 7h265, 12h265
	AVCPacketType   uint8  //1byte, 0videoheader, 1videodata
	CompositionTime uint32 //3byte, 没啥用
	Data            []byte
}

func CreateVideoHeader(dev *Stream) []byte {
	var AvcC AVCDecoderConfigurationRecord
	AvcC.ConfigurationVersion = 0x1
	AvcC.AVCProfileIndication = 0x4d
	AvcC.ProfileCompatibility = 0x40
	AvcC.AVCLevelIndication = 0x1f
	AvcC.Reserved0 = 0xff
	AvcC.LengthSizeMinuxOne = 0xff
	AvcC.Reserved1 = 0xe1
	AvcC.NumOfSps = 0xe1
	AvcC.SpsSize = uint16(len(dev.SpsData) - 4)
	//AvcC.SpsData = dev.SpsData
	AvcC.NumOfPps = 0x01
	AvcC.PpsSize = uint16(len(dev.PpsData) - 4)
	//AvcC.PpsData = dev.PpsData

	l := 11 + AvcC.SpsSize + AvcC.PpsSize
	d := make([]byte, l)

	var s int
	d[s] = AvcC.ConfigurationVersion
	s += 1
	d[s] = AvcC.AVCProfileIndication
	s += 1
	d[s] = AvcC.ProfileCompatibility
	s += 1
	d[s] = AvcC.AVCLevelIndication
	s += 1
	d[s] = ((AvcC.Reserved0 & 0x3f) << 2) | (AvcC.LengthSizeMinuxOne & 0x3)
	s += 1
	d[s] = (AvcC.Reserved1 & 0xe0) | (AvcC.NumOfSps & 0x1f)
	s += 1
	Uint16ToByte(AvcC.SpsSize, d[s:s+2], BE)
	s += 2
	copy(d[s:s+int(AvcC.SpsSize)], dev.SpsData[4:])
	s += int(AvcC.SpsSize)
	d[s] = AvcC.NumOfPps
	s += 1
	Uint16ToByte(AvcC.PpsSize, d[s:s+2], BE)
	s += 2
	copy(d[s:s+int(AvcC.PpsSize)], dev.PpsData[4:])
	s += int(AvcC.SpsSize)

	dev.log.Printf("SpsData:%x", dev.SpsData)
	dev.log.Printf("PpsData:%x", dev.PpsData)
	dev.log.Printf("AvcCData:%x", d)
	return d
}

func UpdateVideoHeader(s *Stream) error {
	//1 生成VideoHeader结构体和数据
	d := CreateVideoHeader(s)
	//1 增加AvcVideoPacket包装
	d = CreateVideoPacket(d, "header", "h264")

	//2 生成Chunk结构体
	c := CreateMessage(MsgTypeIdVideo, uint32(len(d)), d)
	c.Csid = 3
	c.Timestamp = s.RtpTsCurt

	//3 Chunk写入GopCache.VideoHeader
	//s.GopCache.VideoHeader = &c
	return nil
}

func UpdateAudioHeader(dev *Stream) []byte {
	var AacC AudioSpecificConfig
	AacC.ObjectType = 0
	AacC.SamplingIdx = 0
	AacC.ChannelNum = 0
	AacC.FrameLenFlag = 0
	AacC.DependCoreCoder = 0
	AacC.ExtensionFlag = 0
	return nil
}
