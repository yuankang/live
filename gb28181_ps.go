package main

import (
	"bytes"
	"fmt"
	"io"
)

/*************************************************/
/* ParsePsHeader
/*************************************************/
type PsPacket struct {
	Type      string //video or audio
	Timestamp uint32
	Data      []byte //pes data
	UseNum    int
}

//4+1+2+2+4+1+1*n=14+1*n
type PsHeader struct {
	PackStartCode      uint32 //32bit, 包起始码, 固定值0x000001BA
	Reversed0          uint8  //2bit,  0x01
	ScrBase32_30       uint8  //3bit,  SystemClockReferenceBase32_30
	MarkerBit0         uint8  //1bit,  标记位 0x1
	ScrBase29_15       uint16 //15bit, 系统时钟参考基准
	MarkerBit1         uint8  //1bit,  标记位 0x1
	ScrBase14_0        uint16 //15bit, 系统时钟参考
	MarkerBit2         uint8  //1bit,  标记位 0x1
	ScrExtension       uint16 //9bit,  SystemClockReferenceExtension
	MarkerBit3         uint8  //1bit,  标记位 0x1
	ProgramMuxRate     uint32 //22bit, 节目复合速率
	MarkerBit4         uint8  //1bit,  标记位 0x1
	MarkerBit5         uint8  //1bit,  标记位 0x1
	Reserved1          uint8  //5bit,
	PackStuffingLength uint8  //3bit,  该字段后填充字节的个数
	StuffingByte       []byte //8bit,  填充字节 0xff
}

func ParsePsHeader(s *Stream, pps *PsPacket, r *bytes.Reader) (int, error) {
	var ph PsHeader
	var n int

	ph.PackStartCode = 0x000001ba

	b8, err := ReadUint8(r)
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += 1
	ph.Reversed0 = (b8 >> 6) & 0x3
	ph.ScrBase32_30 = (b8 >> 3) & 0x7
	ph.MarkerBit0 = (b8 >> 2) & 0x1

	b16, err := ReadUint16(r, 2, BE)
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += 2
	ph.ScrBase29_15 = ((uint16(b8) & 0x3) << 13) | (b16 >> 3)
	ph.MarkerBit1 = uint8((b16 >> 2) & 0x1)

	b16a, err := ReadUint16(r, 2, BE)
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += 2
	ph.ScrBase14_0 = ((b16 & 0x3) << 13) | (b16a >> 3)
	ph.MarkerBit2 = uint8((b16a >> 2) & 0x1)

	b8, err = ReadUint8(r)
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += 1
	ph.ScrExtension = ((b16a & 0x3) << 7) | uint16(b8>>1)
	ph.MarkerBit2 = b8 & 0x1

	b32, err := ReadUint32(r, 4, BE)
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += 4
	ph.ProgramMuxRate = b32 >> 10
	ph.MarkerBit4 = uint8((b32 >> 9) & 0x1)
	ph.MarkerBit5 = uint8((b32 >> 8) & 0x1)
	ph.Reserved1 = uint8((b32 >> 3) & 0x1f)
	ph.PackStuffingLength = uint8(b32 & 0x7)

	ph.StuffingByte, err = ReadByte(r, uint32(ph.PackStuffingLength))
	if err != nil {
		s.log.Println(err)
		return n, err
	}
	n += int(ph.PackStuffingLength)

	//s.log.Printf("%#v, rLen=%d", ph, n)
	pps.UseNum += n
	return n, nil
}

/*************************************************/
/* ParsePsSysHeader
/*************************************************/
//4+2+4+1+1+3*n=12+3*n
type PsSystemHeader struct {
	SystemHeaderStartCode     uint32       //32bit, 固定值0x000001BB
	HeaderLength              uint16       //16bit, 表示后面还有多少字节
	MarkerBit0                uint8        //1bit
	RateBound                 uint32       //22bit, 速率界限, 取值不小于编码在节目流的任何包中的program_mux_rate字段的最大值。该字段可被解码器用于估计是否有能力对整个流解码。
	MarkerBit1                uint8        //1bit
	AudioBound                uint8        //6bit, 音频界限, 取值是在从0到32的闭区间中的整数
	FixedFlag                 uint8        //1bit, 固定标志, 1表示比特率恒定, 0表示比特率可变
	CspsFlag                  uint8        //1bit, 1表示节目流符合2.7.9中定义的限制
	SystemAudioLockFlag       uint8        //1bit, 系统音频锁定标志, 1表示在系统目标解码器的音频采样率和system_clock_frequency之间存在规定的比率
	SystemVideoLockFlag       uint8        //1bit, 系统视频锁定标志, 1表示在系统目标解码器的视频帧速率和system_clock_frequency之间存在规定的比率
	MarkerBit2                uint8        //1bit
	VideoBound                uint8        //5bit, 视频界限, 取值是在从0到16的闭区间中的整数
	PacketRateRestrictionFlag uint8        //1bit, 分组速率限制, 若CSPS标识为'1'，则该字段表示2.7.9中规定的哪个限制适用于分组速率。若CSPS标识为'0'，则该字段的含义未定义
	ReservedBits              uint8        //7bit, 保留位字段 0x7f
	PsSysBound                []PsSysBound //目前没啥用
}

//1+2=3
type PsSysBound struct {
	StreamId uint8 //8bit
	//流标识, 指示其后的P-STD_buffer_bound_scale和P-STD_buffer_size_bound字段所涉及的流的编码和基本流号码。
	//若取值'1011 1000'，则其后的P-STD_buffer_bound_scale和P-STD_buffer_size_bound字段指节目流中所有的音频流。
	//若取值'1011 1001'，则其后的P-STD_buffer_bound_scale和P-STD_buffer_size_bound字段指节目流中所有的视频流。
	//若stream_id取其它值，则应该是大于或等于'1011 1100'的一字节值且应根据表2-18解释为流的编码和基本流号码。
	Reversed             uint8  //2bit, 0x3
	PStdBufferBoundScale uint8  //1bit, 缓冲区界限比例, 表示用于解释后续P-STD_buffer_size_bound字段的比例系数。若前面的stream_id表示一个音频流，则该字段值为'0'。若表示一个视频流，则该字段值为'1'。对于所有其它的流类型，该字段值可以为'0'也可以为'1'。
	PStdBufferSizeBound  uint16 //13bit, 缓冲区大小界限, 若P-STD_buffer_bound_scale的值为'0'，则该字段以128字节为单位来度量缓冲区大小的边界。若P-STD_buffer_bound_scale的值为'1'，则该字段以1024字节为单位来度量缓冲区大小的边界。
}

func ParsePsSysHeader(s *Stream, pps *PsPacket, r *bytes.Reader) (int, error) {
	var psh PsSystemHeader
	var n int

	psh.SystemHeaderStartCode = 0x000001ba
	psh.HeaderLength, _ = ReadUint16(r, 2, BE)
	n += 2
	b32, _ := ReadUint32(r, 4, BE)
	n += 4
	psh.MarkerBit0 = uint8((b32 >> 31) & 0x1)
	psh.RateBound = (b32 >> 9) & 0x3fffff
	psh.MarkerBit1 = uint8((b32 >> 8) & 0x1)
	psh.AudioBound = uint8((b32 >> 2) & 0x3f)
	psh.FixedFlag = uint8((b32 >> 1) & 0x1)
	psh.CspsFlag = uint8((b32 >> 0) & 0x1)
	b8, _ := ReadUint8(r)
	n += 1
	psh.SystemAudioLockFlag = (b8 >> 7) & 0x1
	psh.SystemVideoLockFlag = (b8 >> 6) & 0x1
	psh.MarkerBit2 = (b8 >> 5) & 0x1
	psh.VideoBound = (b8 >> 0) & 0x1f
	b8, _ = ReadUint8(r)
	n += 1
	psh.PacketRateRestrictionFlag = (b8 >> 7) & 0x1
	psh.ReservedBits = (b8 >> 0) & 0x7f

	//PsSysBound []PsSysBound //目前没啥用
	dl := psh.HeaderLength - 6
	_, _ = ReadByte(r, uint32(dl))
	//d, _ := ReadByte(r, uint32(dl))
	n += int(dl)
	//s.log.Printf("PsSysBound:%x", d)
	/*
		psh.StreamId, _ = ReadUint8(r)
		n += 1
		b16, _ := ReadUint16(r, 2, BE)
		n += 2
		psh.Reversed = uint8((b16 >> 14) & 0x3)
		psh.PStdBufferBoundScale = uint8((b16 >> 13) & 0x1)
		psh.PStdBufferSizeBound = (b16 >> 0) & 0x1fff
	*/

	//s.log.Printf("%#v, rLen=%d", psh, n)
	pps.UseNum += n
	return n, nil
}

/*************************************************/
/* ParsePgmStreamMap 节目流映射
/*************************************************/
//4+2+1+1+2+2+4*n+4=16+4*n
type PgmStreamMap struct {
	PacketStartCodePrefix     uint32          //24bit, 固定值 0x000001
	MapStreamId               uint8           //8bit, 映射流标识 值为0xBC
	ProgramStreamMapLength    uint16          //16bit, 表示后面还有多少字节, 该字段的最大值为0x3FA(1018)
	CurrentNextIndicator      uint8           //1bit, 当前下一个指示符字段, 1表示当前可用, 0表示下个可用
	Reserved0                 uint8           //2bit
	ProgramStreamMapVersion   uint8           //5bit, 表示整个节目流映射的版本号, 节目流映射的定义发生变化，该字段将递增1，并对32取模
	Reserved1                 uint8           //7bit
	MarkerBit                 uint8           //1bit
	ProgramStreamInfoLength   uint16          //16bit, 紧跟在该字段后的描述信息的总长度
	ProgramStreamInfo         []byte          //xbit, 描述信息
	ElementaryStreamMapLength uint16          //16bit, 基本流映射长度, PgmStreamInfo的长度
	PgmStreamInfos            []PgmStreamInfo //32bit, 基本流信息
	CRC32                     uint32          //32bit
}

//StreamType 详见 ISO_IEC_13818-01_2007, Table 2-34, P60
//0x1B  H.264 视频流
//0x24  H.265 视频流, ISO/IEC 13818-1:2018 增加了这个
//0x0F  AAC 音频流
//0x83	PCM未压缩的音频流, G.711a/G.711u是PCM编码变种
//0x90	G.711a 音频流
//0x91	G.711u 音频流
//0x92	G.722.1 音频流
//0x93	G.723.1 音频流
//0x99	G.729 音频流
//StreamType == 0x1b && ElementaryStreamId == 0xe0 是h264
//StreamType == 0x24 && ElementaryStreamId == 0xe0 是h265
//1+1+2=4
type PgmStreamInfo struct {
	StreamType         uint8  //8bit, 取值不能为0x05
	ElementaryStreamId uint8  //8bit, 0x(C0~DF)指音频, 0x(E0~EF)为视频
	DescriptorLength   uint16 //16bit, 指出紧跟在该字段后的描述的字节长度
	DescriptorData     []byte
}

func ParsePgmStreamMap(s *Stream, pps *PsPacket, r *bytes.Reader) (int, error) {
	var psm PgmStreamMap
	var n int

	psm.PacketStartCodePrefix = 0x000001
	psm.MapStreamId = 0xbc
	psm.ProgramStreamMapLength, _ = ReadUint16(r, 2, BE)
	n += 2
	b8, _ := ReadUint8(r)
	n += 1
	psm.CurrentNextIndicator = (b8 >> 7) & 0x1
	psm.Reserved0 = (b8 >> 5) & 0x3
	psm.ProgramStreamMapVersion = (b8 >> 0) & 0x1f
	b8, _ = ReadUint8(r)
	n += 1
	psm.Reserved1 = (b8 >> 1) & 0x7f
	psm.MarkerBit = (b8 >> 0) & 0x1
	psm.ProgramStreamInfoLength, _ = ReadUint16(r, 2, BE)
	n += 2
	if psm.ProgramStreamInfoLength != 0 {
		d, _ := ReadByte(r, uint32(psm.ProgramStreamInfoLength))
		n += int(psm.ProgramStreamInfoLength)
		s.log.Printf("descriptorData:%x", d)
	}
	psm.ElementaryStreamMapLength, _ = ReadUint16(r, 2, BE)
	n += 2
	for i := 0; i < int(psm.ElementaryStreamMapLength); {
		j := ParsePgmStreamInfo(s, r)
		i += j
	}
	n += int(psm.ElementaryStreamMapLength)
	psm.CRC32, _ = ReadUint32(r, 4, BE)
	n += 4

	//s.log.Printf("%#v, rLen=%d", psm, n)
	pps.UseNum += n
	return n, nil
}

func ParsePgmStreamInfo(s *Stream, r *bytes.Reader) int {
	var sm PgmStreamInfo
	var n int

	sm.StreamType, _ = ReadUint8(r)
	n += 1
	sm.ElementaryStreamId, _ = ReadUint8(r)
	n += 1
	sm.DescriptorLength, _ = ReadUint16(r, 2, BE)
	n += 2

	if sm.DescriptorLength != 0 {
		sm.DescriptorData, _ = ReadByte(r, uint32(sm.DescriptorLength))
		n += int(sm.DescriptorLength)
	}

	if sm.ElementaryStreamId == 0xe0 {
		switch sm.StreamType {
		case 0x1b:
			s.VideoCodecType = "H264"
		case 0x24:
			s.VideoCodecType = "H265"
		default:
			s.log.Printf("VideoFormat: %x, unknow", sm.StreamType)
			return n
		}
		s.log.Printf("VideoCodecType=%s, StreamType=%x, esid=%x, descLen=%d, descData=%x, rLen=%d", s.VideoCodecType, sm.StreamType, sm.ElementaryStreamId, sm.DescriptorLength, sm.DescriptorData, n)
	}
	if sm.ElementaryStreamId == 0xc0 {
		switch sm.StreamType {
		case 0x0f:
			s.AudioCodecType = "AAC"
		case 0x83:
			s.AudioCodecType = "PCM"
		case 0x90:
			s.AudioCodecType = "G711a"
		case 0x91:
			s.AudioCodecType = "G711u"
		default:
			s.log.Printf("AudioFormat=%x, unknow", sm.StreamType)
			return n
		}
		s.log.Printf("AudioCodecType=%s, StreamType=%x, esid=%x, descLen=%d, descData=%x, rLen=%d", s.AudioCodecType, sm.StreamType, sm.ElementaryStreamId, sm.DescriptorLength, sm.DescriptorData, n)
	}
	return n
}

/*************************************************/
/* ParsePs
/*************************************************/
func IsTrailing(s *Stream, scb []byte) bool {
	sc := ByteToUint32(scb, BE)
	//s.log.Printf("StartCode:%#08x", sc)
	switch sc {
	case 0x000001ba: //ps 开始码
		return false
	case 0x000001e0: //video 开始码
		return false
	case 0x000001c0: //audio 开始码
		return false
	}
	return true
}

//pps中可能有时间戳相同的音频和视频数据, 要做分离
func ParsePs(s *Stream, pps *PsPacket) error {
	var err error
	var sc uint32
	var i uint32
	var pp *PsPacket

	r := bytes.NewReader(pps.Data)
	for {
		pp = nil
		sc, err = ReadUint32(r, 4, BE)
		if err != nil {
			if err != io.EOF {
				s.log.Println(err)
				return err
			}
			return nil
		}
		pps.UseNum += 4
		s.log.Printf("i=%d, StartCode=%#08x", i, sc)
		i++

		//PS流总是以0x000001BA开始, 以0x000001B9结束
		//对于PS文件 有且只有一个结束码0x000001B9, 对于直播PS流 没有结束码
		switch sc {
		case 0x000001ba:
			_, err = ParsePsHeader(s, pps, r)
		case 0x000001bb:
			_, err = ParsePsSysHeader(s, pps, r)
		case 0x000001bc:
			_, err = ParsePgmStreamMap(s, pps, r)
		case 0x000001c0:
			pp, err = ParseAudio(s, pps, r)
		case 0x000001e0:
			pp, err = ParseVideo(s, pps, r)
		default:
			err = fmt.Errorf("undefined startcode %#08x", sc)
		}
		if err != nil {
			s.log.Println(err)
			return err
		}

		if pp == nil {
			continue
		}

		if len(s.PsPktChan) < conf.RtpRtcp.PsPktChanNum {
			s.PsPktChan <- pp
		} else {
			s.log.Printf("PsPktChanLen=%d, MaxLen=%d", len(s.PsPktChan), conf.RtpRtcp.PsPktChanNum)
		}
	}
	return nil
}
