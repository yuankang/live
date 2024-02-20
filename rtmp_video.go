package main

import (
	"bytes"
	"fmt"
	"io"
	"utils"
)

/*************************************************/
/* metadata
/*************************************************/
//Metadata 数据要缓存起来，发送给播放者
func MetadataHandle(s *RtmpStream, c *Chunk) error {
	c.DataType = "Metadata"
	r := bytes.NewReader(c.MsgData)
	vs, err := AmfUnmarshal(s, r) // 序列化转结构化
	if err != nil && err != io.EOF {
		s.log.Println(err)
		return err
	}
	s.log.Printf("Metadata: %#v", vs)

	//s.GopCache.MetaData = c
	s.GopCache.MetaData.Store(s.Key, c)
	return nil
}

/*************************************************/
/* video data header
/*************************************************/
//video_file_format_spec_v10.pdf
//FrameType
//1:iFrame, 2:pFrame or bFrame,
//CodecID
//7:h264, 12:h265,
//AvcPacketType
//0:AVC sequence header, 1:AVC NALU, 2:AVC end of sequence,
//1+1+3=5Byte
type RtmpVideoDataHeader struct {
	FrameType       uint8  //4bit
	CodecID         uint8  //4bit
	AvcPacketType   uint8  //8bit
	CompositionTime uint32 //24bit, 没啥用 等于0就行
}

/*************************************************/
/* video sequence header
/*************************************************/
//See ISO 14496-15, 5.2.4.1 for AVCDecoderConfigurationRecord
//ISO/IEC 14496-15:2019 要花钱购买
//https://www.iso.org/standard/74429.html
// SPS defined in ISO/IEC 14496-10, 位于7.3.2.1.1
// PPS defined in ISO/IEC 14496-10
// AVCDecoderConfigurationRecord 就是AVC sequence header
// AVCDecoderConfigurationRecord在FLV文件中一般情况也是出现1次，也就是第一个video tag
//5+3+x+3+y=11+x+y
type AVCDecoderConfigurationRecord struct {
	ConfigurationVersion uint8  //8bit, 通常0x01
	AVCProfileIndication uint8  //8bit, 值同第1个sps的第1字节
	ProfileCompatibility uint8  //8bit, 值同第1个sps的第2字节
	AVCLevelIndication   uint8  //8bit, 值同第1个sps的第3字节
	Reserved0            uint8  //6bit, 保留全1
	LengthSizeMinuxOne   uint8  //2bit, 通常这个值为3, 即NAL码流中使用3+1=4字节表示NALU的长度
	Reserved1            uint8  //3bit, 保留全1
	NumOfSps             uint8  //5bit, 通常为1
	SpsSize              uint16 //16bit, SpsData长度
	SpsData              []byte //xByte
	NumOfPps             uint8  //8bit, 通常为1
	PpsSize              uint16 //16bit, PpsData长度
	PpsData              []byte //yByte
}

type HVCCNALUnit struct {
	ArrayCompleteness uint8  // 1bit
	Reserved0         uint8  // 1bit, 0b
	NaluType          uint8  // 6bit, 32vps, 33sps, 34pps, 39sei
	NALunitType       uint8  // 6bit, 32vps, 33sps, 34pps, 39sei
	NumNalus          uint16 // 16bit, 大端
	NaluLen           uint16 // 16bit, 大端
	NaluData          []byte // NaluLen个byte
}

//TODO: 在哪个文档中定义呢???
type HEVCDecoderConfigurationRecord struct {
	ConfigurationVersion             uint8         // 8bit, 0x01
	GeneralProfileSpace              uint8         // 2bit
	GeneralTierFlag                  uint8         // 1bit
	GeneralProfileIdc                uint8         // 5bit
	GeneralProfileCompatibilityFlags uint32        // 32bit
	GeneralConstraintIndicatorFlags  uint64        // 48bit
	GeneralLevelIdc                  uint8         // 8bit
	Reserved0                        uint8         // 4bit, 1111b
	MinSpatialSegmentationIdc        uint16        // 12bit
	Reserved1                        uint8         // 6bit, 111111b
	ParallelismType                  uint8         // 2bit
	Reserved2                        uint8         // 6bit, 111111b
	ChromaFormat                     uint8         // 2bit
	Reserved3                        uint8         // 5bit, 11111b
	BitDepthLumaMinus8               uint8         // 3bit
	Reserved4                        uint8         // 5bit, 11111b
	BitDepthChromaMinus8             uint8         // 3it
	AvgFrameRate                     uint16        // 16bit
	ConstantFrameRate                uint8         // 2bit
	NumTemporalLayers                uint8         // 3bit
	TemporalIdNested                 uint8         // 1bit
	LengthSizeMinusOne               uint8         // 2bit
	NumOfArrays                      uint8         // 8bit, 前面共22字节
	Array                            []HVCCNALUnit // vps，sps，pps 在这里
	Vps                              []HVCCNALUnit
	Sps                              []HVCCNALUnit
	Pps                              []HVCCNALUnit
	Sei                              []HVCCNALUnit
}

/*************************************************/
/* video h264
/*************************************************/
func VideoHandleH264(s *RtmpStream, c *Chunk) error {
	var err error
	if len(c.MsgData) < 2 {
		err = fmt.Errorf("AVC body no enough data")
		return err
	}
	FrameType := c.MsgData[0] >> 4 // 4bit

	//0: AVC sequence header
	//1: AVC NALU
	//2: AVC end of sequence
	AVCPacketType := c.MsgData[1] // 8bit
	//创作时间 int24
	//CompositionTime := ByteToInt32(c.MsgData[2:5], BE) // 24bit

	if AVCPacketType == 0 {
		s.log.Printf("This frame is AVC sequence header:%v, %x", len(c.MsgData), c.MsgData)
		if len(c.MsgData) < 14 {
			err = fmt.Errorf("AVC body no enough data")
			//s.log.Printf("AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return err
		}
		c.DataType = "VideoHeader"

		// 前5个字节上面已经处理，AVC sequence header从第6个字节开始
		//0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x4d, 0x40, 0x1f, 0xff,
		//0xe1, 0x00, 0x1c, 0x67, 0x4d, 0x40, 0x1f, 0xe8, 0x80, 0x28,
		//0x02, 0xdd, 0x80, 0xb5, 0x01, 0x01, 0x01, 0x40, 0x00, 0x00,
		//0x03, 0x00, 0x40, 0x00, 0x00, 0x0c, 0x03, 0xc6, 0x0c, 0x44,
		//0x80, 0x01, 0x00, 0x04, 0x68, 0xeb, 0xef, 0x20
		//AVCDecoderConfigurationRecord, ISO 14496-15, 5.2.4.1, P16
		//ISO/IEC 14496-15:2019 要花钱购买
		//https://www.iso.org/standard/74429.html
		var AvcC AVCDecoderConfigurationRecord
		AvcC.ConfigurationVersion = c.MsgData[5]     // 8bit, 0x01
		AvcC.AVCProfileIndication = c.MsgData[6]     // 8bit, 0x4d, 0100 1101
		AvcC.ProfileCompatibility = c.MsgData[7]     // 8bit, 0x40, 0100 0000
		AvcC.AVCLevelIndication = c.MsgData[8]       // 8bit, 0x1f
		AvcC.Reserved0 = (c.MsgData[9] & 0xFC) >> 2  // 6bit, 0xff, 1111 1111
		AvcC.LengthSizeMinuxOne = c.MsgData[9] & 0x3 // 2bit, 0xff
		AvcC.Reserved1 = (c.MsgData[10] & 0xE0) >> 5 // 3bit, 0xe1, 11100001
		//FIXME: NumOfSps和NumOsPps 可以>=1, 现在是按=1处理的
		AvcC.NumOfSps = c.MsgData[10] & 0x1F // 5bit, 0xe1
		s.log.Printf("%#v", AvcC)

		//有时候 推流者 会把 VideoHeader + VideioKeyFrame 放到一个Message发送
		//TODO: 这种情况下 处理完 VideoHeader后, 要检查是否有剩余数据
		//如果不检查 就找不到关键帧了, GOP首帧 也不是关键帧了

		var temp uint32
		var spsLen [32]uint16
		var spsData [32][]byte
		var totalSpsLen uint32
		var i uint8
		for i = 0; i < AvcC.NumOfSps; i++ {
			if uint32(len(c.MsgData)) < (13 + temp) {
				s.log.Printf("AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				err = fmt.Errorf("AVC body no enough data")
				return err
			}
			spsLen[i] = ByteToUint16(c.MsgData[11+temp:13+temp], BE) // 16bit, 0x001c

			if uint32(len(c.MsgData)) < (13 + temp + uint32(spsLen[i])) {
				s.log.Printf("AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				err = fmt.Errorf("AVC body no enough data")
				return err
			}
			spsData[i] = c.MsgData[13+temp : 13+temp+uint32(spsLen[i])]
			temp += 2 + uint32(spsLen[i])
			totalSpsLen += uint32(spsLen[i])
		}
		EndPos := 11 + temp // 11 + 2 + 28

		AvcC.NumOfPps = c.MsgData[EndPos] // 8bit, 0x01
		var ppsData [256][]byte
		var ppsLen [256]uint16
		var totalPpsLen uint32
		temp = EndPos + 1
		for i = 0; i < AvcC.NumOfPps; i++ {
			if uint32(len(c.MsgData)) < (2 + temp) {
				s.log.Printf("AVC body no enough data:%d, %v, %x", (2 + temp), len(c.MsgData), c.MsgData)
				err = fmt.Errorf("AVC body no enough data")
				return err
			}
			ppsLen[i] = ByteToUint16(c.MsgData[temp:2+temp], BE) // 16bit, 0x0004
			if uint32(len(c.MsgData)) < (2 + uint32(ppsLen[i]) + temp) {
				s.log.Printf("AVC body no enough data:%d, %d, %x", (2 + uint32(ppsLen[i]) + temp), len(c.MsgData), c.MsgData)
				err = fmt.Errorf("AVC body no enough data")
				return err
			}
			ppsData[i] = c.MsgData[2+temp : 2+temp+uint32(ppsLen[i])]
			temp += 2 + uint32(ppsLen[i])
			totalPpsLen += uint32(ppsLen[i])
		}

		//use to snapshot
		AvcC.SpsSize = spsLen[0]
		AvcC.PpsSize = ppsLen[0]
		AvcC.SpsData = spsData[0]
		AvcC.PpsData = ppsData[0]
		s.AvcC = &AvcC
		if len(AvcC.SpsData) >= 3 && len(AvcC.PpsData) >= 3 {
			s.log.Printf("sps:%x, pps:%x", AvcC.SpsData[:3], AvcC.PpsData[:3])
			//sps:674d00, pps:68ee3c
		}
		s.log.Printf("video h264 sps %x, %x, %x, %x, %x, %x", AvcC.NumOfSps, AvcC.NumOfPps, spsLen[0], ppsLen[0], spsData[0], ppsData[0])
		sps, err := SpsParse0(s, AvcC.SpsData)
		if err != nil {
			s.log.Println(err)
			return err
		}
		s.log.Printf("%#v", sps)
		s.Width = int((sps.PicWidthInMbsMinus1 + 1) * 16)
		s.Height = int((sps.PicHeightInMapUnitsMinus1 + 1) * 16)
		s.log.Printf("video width=%d, height=%d", s.Width, s.Height)
		//pps := PpsParse(AvcC.PpsData)

		//上报流状态 为 直播开始
		/*
			s.log.Println(s.AmfInfo.PublishName, BackDoor)
			if !strings.Contains(s.AmfInfo.PublishName, BackDoor) {
				var streamStat StreamStateInfo
				streamStat.s = *s
				streamStat.state = 1
				if len(StreamStateChan) < conf.StreamStatekMax {
					StreamStateChan <- streamStat
				} else {
					s.log.Printf("overflow of stream state chan")
				}
			}
		*/

		//s.GopCache.VideoHeader = c
		s.GopCache.VideoHeader.Store(s.Key, c)
	} else if AVCPacketType == 1 {
		//s.log.Println("This frame is AVC NALU")
		if s.FirstVideoTs == 0 {
			s.FirstVideoTs = c.Timestamp
			s.PrevVideoTs = c.Timestamp
		} else {
			//视频帧率60fps, 帧间隔1000/60=16.7ms
			//视频帧率25fps, 帧间隔1000/25=  40ms
			//视频帧率20fps, 帧间隔1000/20=  50ms
			//视频帧率 2fps, 帧间隔1000/ 5= 500ms
			//视频帧率 1fps, 帧间隔1000/ 5=1000ms
			//音画相差400ms, 人类就能明显感觉到不同步
			if c.Timestamp >= s.PrevVideoTs {
				s.TotalVideoDelta += c.Timestamp - s.PrevVideoTs
			}

			s.VideoTsDifValue = c.Timestamp - s.PrevVideoTs
			if s.VideoTsDifValue > 500 {
				s.log.Printf("bigjump: c.Ts(%d) - s.Pvts(%d) = VtsDv(%d)", c.Timestamp, s.PrevVideoTs, s.VideoTsDifValue)
			}
			s.PrevVideoTs = c.Timestamp
		}

		//judge abnormal timestamp, don't use first A/V 250 packet to calculte, and only calculte 4 times
		s.PktNum++
		var deltaCal uint32
		if s.PktNum > conf.AdjustPktNum && s.CalNumVideo < AdjustSeqNum {
			var DurationVideo int64
			s.PktNumVideo++
			cTime := utils.GetTimestamp("ms")
			if s.StartTimeVideo == 0 {
				s.StartTimeVideo = cTime
			}
			DurationVideo = cTime - s.StartTimeVideo
			//use 10 seconds video packet to calculate and must receive key frame
			if DurationVideo >= 10000 && FrameType == 1 {
				s.CalNumVideo++
				deltaCal = uint32(DurationVideo / int64(s.PktNumVideo))

				//calculate time per frame compare to receive frame delta time
				if (s.TotalVideoDelta / s.PktNumVideo) < 2*deltaCal {
					//normal
					s.SeqNumVideo = 0
				} else {
					//abnormal timestamp, need adjust
					s.DeltaCalVideo[s.SeqNumVideo] = deltaCal
					s.SeqNumVideo++
					s.log.Printf("avc abnormal delta timestamp:%d, cal delta timestamp:%d, seq num:%d >= 3 wil adjust", s.TotalVideoDelta/s.PktNumVideo, deltaCal, s.SeqNumVideo)
				}

				s.StartTimeVideo = cTime
				s.PktNumVideo = 0
				s.TotalVideoDelta = 0
			}
		} else {
			s.TotalVideoDelta = 0
		}
		//when three successive times happened abnormal delta timestamp, should adjust
		if conf.AdjustDts == true && s.SeqNumVideo >= AdjustSeqNum {
			if s.FirstVideoAdust == 0 {
				s.PrevVideoAdust = c.Timestamp
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
				c.Timestamp = s.PrevVideoAdust + deltaCal
			}
			s.FirstVideoAdust++
			s.PrevVideoAdust = c.Timestamp
		}

		// h264的avcc格式: NaluLen(4字节) + NaluData
		// h264的annexB格式: startCode(4字节) + NaluData
		// startCode(4字节) 为 0x00000001
		// startCode(3字节) 为 0x000001
		// 此处是 h264的avcc格式, ts文件使用annexB格式
		//写ts文件的时候 每个nalu前都要有startCode
		// One or more NALUs
		//ffmpeg rtmp 推流 h264的时候, 每个视频关键帧消息里有2个nalu
		//nalu(sei) + nalu(idr)     NalUnitType: 6, 5
		//nalu(sei) + nalu(iframe)	NalUnitType: 6, 1
		c.NaluNum, _ = GetNaluNum(s, c, "h264")

		//每个包都会打印 要精简, 增加配置项, 测试环境每帧都打印, 线上每帧都不打印
		//打印bitrate时, 顺带打印最近一帧的NaluNum个数, 这个测试环境和线上都打印
		if c.NaluNum != 1 && conf.NaluNumPrintEnable == true {
			s.log.Printf("naluNum=%d", c.NaluNum)
		}
		s.NaluNum = c.NaluNum

		//c.Fmt = c.FmtFirst
		goplocks.Lock()
		s.GopCache.MediaData.PushBack(c)
		goplocks.Unlock()

		//KeyFrameNum	1	2	3	4	关键帧个数
		//CacheNum		0	1	2	3	Gop个数为1时要开始删Gop
		//CacheMax		1	1	1	1	Gop最大个数
		/*
			if FrameType == 1 {
				// 第1个关键帧存入时 GopCacheNum = 1
				// 第2个关键帧存入时 GopCacheNum = 2
				s.GopCache.GopCacheNum++
				GopCacheUpdate(s)
			}
		*/

		//关键帧来的时候 开始记录帧数, 帧数>=20 才刷新gop
		if FrameType == 1 {
			s.GopCache.GopCacheNum++
			VideoKeyFrame.Store(s.Key, c)
		}
		s.CountNum++
		//s.log.Printf("CountNum=%d", s.CountNum)
		if s.CountNum == conf.Rtmp.GopFrameNum {
			GopCacheUpdate(s)
		}
	} else {
		//ffmpeg 结束推流时 会发送 AVCPacketType=2 的视频关键帧
		//此时 MsgLength=5, 这帧数据不能往下发, 会引起hls生产崩溃
		//崩溃的函数是 PesDataCreateKeyFrame()
		err := fmt.Errorf("This frame is AVC end of sequence")
		s.log.Println(err)
		return err
	}

	//s.log.Printf("FrameType=%d, AVCPacketType=%d, Composition=%d, DataLen=%d", FrameType, AVCPacketType, CompositionTime, len(c.MsgData[5:]))
	return nil
}

/*************************************************/
/* video h265
/*************************************************/
func VideoHandleH265(s *RtmpStream, c *Chunk) error {
	var err error
	if len(c.MsgData) < 2 {
		err = fmt.Errorf("HEVC body no enough data")
		return err
	}
	FrameType := c.MsgData[0] >> 4 // 4bit

	//0: AVC sequence header
	//1: AVC NALU
	//2: AVC end of sequence
	AVCPacketType := c.MsgData[1] // 8bit
	//创作时间 int24
	//CompositionTime := ByteToInt32(c.MsgData[2:5], BE) // 24bit

	if AVCPacketType == 0 {
		s.log.Printf("This frame is HEVC sequence header:%v, %x", len(c.MsgData), c.MsgData)
		c.DataType = "VideoHeader"
		if len(c.MsgData) < 28 {
			err = fmt.Errorf("HEVC body no enough data")
			//s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return err
		}
		//len(c.MsgData) = 135
		//1c 00 00 00 00
		//前5个字节上面已经处理，HEVC sequence header从第6个字节开始
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
		var HevcC HEVCDecoderConfigurationRecord
		HevcC.ConfigurationVersion = c.MsgData[5] // 8bit, 0x01
		// 中间这些字段, 我们不关心
		HevcC.NumOfArrays = c.MsgData[27] // 8bit, 一般为3
		//s.log.Printf("hevc %x, %x", HevcC.ConfigurationVersion, HevcC.NumOfArrays)

		var i, j, k uint16 = 0, 28, 0
		var hn HVCCNALUnit
		for ; i < uint16(HevcC.NumOfArrays); i++ {
			if len(c.MsgData) < int(j+3) {
				err = fmt.Errorf("HEVC body no enough data")
				s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				return err
			}

			hn.ArrayCompleteness = c.MsgData[j] >> 7
			hn.Reserved0 = (c.MsgData[j] >> 6) & 0x1
			hn.NALunitType = c.MsgData[j] & 0x3f
			j++
			// TODO: hn.NumNalus > 1
			hn.NumNalus = ByteToUint16(c.MsgData[j:j+2], BE)
			j += 2
			for k = 0; k < hn.NumNalus; k++ {
				if len(c.MsgData) < int(j+2) {
					err = fmt.Errorf("HEVC body no enough data")
					s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
					return err
				}
				hn.NaluLen = ByteToUint16(c.MsgData[j:j+2], BE)
				j += 2
				if len(c.MsgData) < int(j+hn.NaluLen) {
					err = fmt.Errorf("HEVC body no enough data")
					s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
					return err
				}
				hn.NaluData = c.MsgData[j : j+hn.NaluLen]
				j += hn.NaluLen
				s.log.Printf("%#v", hn)

				switch hn.NALunitType {
				case 32: // 0x20
					s.log.Printf("NaluType=%d is VPS", hn.NALunitType)
					HevcC.Vps = append(HevcC.Vps, hn)
				case 33: // 0x21
					s.log.Printf("NaluType=%d is SPS", hn.NALunitType)
					HevcC.Sps = append(HevcC.Sps, hn)
				case 34: // 0x22
					s.log.Printf("NaluType=%d is PPS", hn.NALunitType)
					HevcC.Pps = append(HevcC.Pps, hn)
				case 39: // 0x27
					s.log.Printf("NaluType=%d is SEI", hn.NALunitType)
					HevcC.Sei = append(HevcC.Sei, hn)
				default:
					s.log.Printf("NaluType=%d untreated", hn.NALunitType)
				}
			}
		}

		//snapshot use vps, sps and pps
		s.HevcC = &HevcC
		if len(HevcC.Vps) > 0 && len(HevcC.Vps[0].NaluData) >= 3 &&
			len(HevcC.Sps) > 0 && len(HevcC.Sps[0].NaluData) >= 3 &&
			len(HevcC.Pps) > 0 && len(HevcC.Pps[0].NaluData) >= 3 {
			s.log.Printf("vps:%x, sps:%x, pps:%x", HevcC.Vps[0].NaluData[:3], HevcC.Sps[0].NaluData[:3], HevcC.Pps[0].NaluData[:3])
		}
		//vsp:40010c, sps:420101, pps:4401c1
		if len(HevcC.Sps) == 0 {
			err = fmt.Errorf("HEVC has no sps header")
			s.log.Printf("HEVC has no sps header")
			return err
		}
		spsH265, err := SpsParseH265(s, HevcC.Sps[0].NaluData)
		if err != nil {
			s.log.Println(err)
			return err
		}
		s.log.Printf("%#v", spsH265)
		s.Width = int(spsH265.PicWidthInLumaSamples)
		s.Height = int(spsH265.PicHeightInLumaSamples)
		s.log.Printf("video width=%d, height=%d", s.Width, s.Height)
		//ppsH265 := PpsParseH265(HevcC.Pps)

		//上报流状态 为 直播开始
		/*
			if !strings.Contains(s.AmfInfo.PublishName, BackDoor) {
				var streamStat StreamStateInfo
				streamStat.s = *s
				streamStat.state = 1
				if len(StreamStateChan) < conf.StreamStatekMax {
					StreamStateChan <- streamStat
				} else {
					s.log.Printf("overflow of stream state chan")
				}
			}
		*/

		//s.GopCache.VideoHeader = c
		s.GopCache.VideoHeader.Store(s.Key, c)
	} else if AVCPacketType == 1 {
		//s.log.Println("This frame is HEVC NALU")
		if s.FirstVideoTs == 0 {
			s.FirstVideoTs = c.Timestamp
			s.PrevVideoTs = c.Timestamp
		} else {
			//视频帧率60fps, 帧间隔1000/60=16.7ms
			//视频帧率25fps, 帧间隔1000/25=  40ms
			//视频帧率20fps, 帧间隔1000/20=  50ms
			//视频帧率 2fps, 帧间隔1000/ 5= 500ms
			//视频帧率 1fps, 帧间隔1000/ 5=1000ms
			//音画相差400ms, 人类就能明显感觉到不同步
			if c.Timestamp >= s.PrevVideoTs {
				s.TotalVideoDelta += c.Timestamp - s.PrevVideoTs
			}

			s.VideoTsDifValue = c.Timestamp - s.PrevVideoTs
			if s.VideoTsDifValue > 500 {
				s.log.Printf("bigjump: c.Ts(%d) - s.Pvts(%d) = VtsDv(%d)", c.Timestamp, s.PrevVideoTs, s.VideoTsDifValue)
			}
			s.PrevVideoTs = c.Timestamp
		}

		//judge abnormal timestamp, don't use first A/V 250 packet to calculte, and only calculte 4 times
		s.PktNum++
		var deltaCal uint32
		if s.PktNum > conf.AdjustPktNum && s.CalNumVideo < AdjustSeqNum {
			var DurationVideo int64
			s.PktNumVideo++
			cTime := utils.GetTimestamp("ms")
			if s.StartTimeVideo == 0 {
				s.StartTimeVideo = cTime
			}
			DurationVideo = cTime - s.StartTimeVideo
			if DurationVideo >= 10000 && FrameType == 1 {
				s.CalNumVideo++
				deltaCal = uint32(DurationVideo / int64(s.PktNumVideo))

				//calculate time per frame compare to receive frame delta time
				if (s.TotalVideoDelta / s.PktNumVideo) < 2*deltaCal {
					//normal
					s.SeqNumVideo = 0
				} else {
					//abnormal timestamp, need adjust
					s.DeltaCalVideo[s.SeqNumVideo] = deltaCal
					s.SeqNumVideo++
					s.log.Printf("hevc abnormal delta timestamp:%d, cal delta timestamp:%d, seq num:%d >= 3 wil adjust", s.TotalVideoDelta/s.PktNumVideo, deltaCal, s.SeqNumVideo)
				}

				s.StartTimeVideo = cTime
				s.PktNumVideo = 0
				s.TotalVideoDelta = 0
			}
		} else {
			s.TotalVideoDelta = 0
		}
		//when three successive times happened abnormal delta timestamp, should adjust
		if conf.AdjustDts == true && s.SeqNumVideo >= AdjustSeqNum {
			if s.FirstVideoAdust == 0 {
				s.PrevVideoAdust = c.Timestamp
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
				c.Timestamp = s.PrevVideoAdust + deltaCal
			}
			s.FirstVideoAdust++
			s.PrevVideoAdust = c.Timestamp
		}

		// One or more NALUs
		// 详细说明 见 VideoHandleH264()
		c.NaluNum, _ = GetNaluNum(s, c, "h265")

		//每个包都会打印 要精简, 增加配置项, 测试环境每帧都打印, 线上每帧都不打印
		//打印bitrate时, 顺带打印最近一帧的NaluNum个数, 这个测试环境和线上都打印
		if c.NaluNum != 1 && conf.NaluNumPrintEnable == true {
			s.log.Printf("naluNum=%d", c.NaluNum)
		}
		s.NaluNum = c.NaluNum

		//c.Fmt = c.FmtFirst
		goplocks.Lock()
		s.GopCache.MediaData.PushBack(c)
		goplocks.Unlock()

		//KeyFrameNum	1	2	3	4	关键帧个数
		//CacheNum		0	1	2	3	Gop个数为1时要开始删Gop
		//CacheMax		1	1	1	1	Gop最大个数
		/*
			if FrameType == 1 {
				// 第1个关键帧存入时 GopCacheNum = 1
				// 第2个关键帧存入时 GopCacheNum = 2
				s.GopCache.GopCacheNum++
				GopCacheUpdate(s)
			}
		*/

		//关键帧来的时候 开始记录帧数, 帧数>=20 才刷新gop
		if FrameType == 1 {
			s.GopCache.GopCacheNum++
			VideoKeyFrame.Store(s.Key, c)
		}
		s.CountNum++
		//s.log.Printf("CountNum=%d", s.CountNum)
		if s.CountNum == uint32(conf.Rtmp.GopFrameNum) {
			GopCacheUpdate(s)
		}
	} else {
		// ffmpeg 结束推流时 会发送 AVCPacketType=2 的视频关键帧
		// 此时 MsgLength=5, 这帧数据不能往下发, 会引起hls生产崩溃
		// 崩溃的函数是 PesDataCreateKeyFrame()
		err := fmt.Errorf("This frame is HEVC end of sequence")
		s.log.Println(err)
		return err
	}

	//s.log.Printf("AVCPacketType=%d, Composition=%d, DataLen=%d", AVCPacketType, CompositionTime, len(c.MsgData[5:]))
	return nil
}

/*************************************************/
/* video handle
/*************************************************/
// AVCDecoderConfigurationRecord 包含着是H.264解码相关比较重要的sps和pps信息，再给AVC解码器送数据流之前一定要把sps和pps信息送出，否则的话解码器不能正常解码。
// 而且在解码器stop之后再次start之前，如seek、快进快退状态切换等，都需要重新送一遍sps和pps的信息.
// AVCDecoderConfigurationRecord在FLV文件中一般情况也是出现1次，也就是第一个 video tag.
func VideoHandle(s *RtmpStream, c *Chunk) error {
	var err error
	if len(c.MsgData) < 1 {
		err = fmt.Errorf("video body has no data")
		return err
	}

	FrameType := c.MsgData[0] >> 4 // 4bit
	CodecId := c.MsgData[0] & 0xf  // 4bit
	//s.log.Printf("FrameType=%d, CodecId=%d", FrameType, CodecId)

	//1: keyframe (for AVC, a seekable frame), 关键帧(I帧)
	//2: inter frame (for AVC, a non-seekable frame), 非关键帧(P/B帧)
	//3: disposable inter frame (H.263 only)
	//4: generated keyframe (reserved for server use only)
	//5: video info/command frame
	if FrameType == 1 {
		//s.log.Println("FrameType is KeyFrame(I frame)")
		c.DataType = "VideoKeyFrame"
	} else if FrameType == 2 {
		//如何区分是 B帧 还是 P帧???
		//s.log.Println("FrameType is InterFrame(B/P frame)")
		c.DataType = "VideoInterFrame"
	} else {
		err = fmt.Errorf("untreated FrameType %d", FrameType)
		s.log.Println(err)
		return err
	}

	//1: JPEG (currently unused)
	//2: Sorenson H.263
	//3: Screen video
	//4: On2 VP6
	//5: On2 VP6 with alpha channel
	//6: Screen video version 2
	//7: AVC(h264), AVCVIDEOPACKET
	//12: HEVC(h265), RTMP头部信息封装并没有定义HEVC，我们采用CDN联盟的HEVC扩展标准，将HEVC定义为12
	switch CodecId {
	case 7:
		//s.log.Printf("CodecId=%d is AVC(h264)", CodecId)
		s.VideoCodecType = "H264"
		err = VideoHandleH264(s, c)
	case 12:
		//s.log.Printf("CodecId=%d is HEVC(h265)", CodecId)
		s.VideoCodecType = "H265"
		err = VideoHandleH265(s, c)
	default:
		err = fmt.Errorf("untreated CodecId %d", CodecId)
	}

	if err != nil {
		s.log.Println(err)
	}
	return err
}
