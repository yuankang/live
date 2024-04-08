package main

import (
	"fmt"
	"log"
	"math"
)

//去除防竞争码 检测到 00 00 03 要抛弃03
//数据结尾是 0x000003时, 0x03不应该被去掉
func PreventionCodeWipe(data []byte) []byte {
	l := len(data)
	if l < 3 {
		log.Println("data len must >= 3")
		return data
	}
	d := make([]byte, l)

	var i, j int
	for i = 0; i < l; i++ {
		d[j] = data[i]
		//开头不能是000003
		if i > 2 && data[i-2] == 0x00 && data[i-1] == 0x00 && data[i] == 0x03 {
			if i+1 < l {
				d[j] = data[i+1]
				i++
			} else {
				j++
				continue
			}
		}
		j++
	}
	return d[:j]
}

/*************************************************/
/* H264/H265 nalu, annexb 转 avcc
/*************************************************/
/*
h264的avcc格式		NaluLen(4字节) + NaluData
h264的annexB格式	startCode(3/4字节) + NaluData
startCode(3字节)	0x000001
startCode(4字节)	0x00000001
*/
type NaluInfo struct {
	Type    string //vps/sps/pps/sei/ifrm/pfrm
	Data    []byte //不包含开始码
	ByteNum int    //3表示0x000001, 4表示0x00000001
	BytePos int    //0x00000001 左边第一个00的下标
	ByteLen int    //0x00000001 后面的数据长度, 也就是Data长度
}

/*
2024/04/08 18:44:27 0000000167aaaa0000000168bbbb00000106cccc0000000165dddd
2024/04/08 18:44:27 Type:sps, Data:67aaaa, Num=4, Pos=0, Len=3, dLen=3
2024/04/08 18:44:27 Type:pps, Data:68bbbb, Num=4, Pos=7, Len=3, dLen=3
2024/04/08 18:44:27 Type:sei, Data:06cccc, Num=3, Pos=14, Len=3, dLen=3
2024/04/08 18:44:27 Type:ifrm, Data:65dddd, Num=4, Pos=20, Len=3, dLen=3

2024/04/08 18:43:53 6600000167aaaa0000000168bbbb00000106cccc0000000165dddd000001
2024/04/08 18:43:53 Type:sps, Data:67aaaa, Num=3, Pos=1, Len=3, dLen=3
2024/04/08 18:43:53 Type:pps, Data:68bbbb, Num=4, Pos=7, Len=3, dLen=3
2024/04/08 18:43:53 Type:sei, Data:06cccc, Num=3, Pos=14, Len=3, dLen=3
2024/04/08 18:43:53 Type:ifrm, Data:65dddd, Num=4, Pos=20, Len=3, dLen=3
2024/04/08 18:43:53 Type:unknow, Data:, Num=3, Pos=27, Len=0, dLen=0
*/
//找 0x00000001 或 0x000001
//海康摄像头sps/pps/sep尾部会加000001e0开头的数据, 这部分要去掉
//sps 00000001674d001f9da814016e9b808080a000000300200000065080000001e0000e8c0003fffffc
//pps 0000000168ee3c80000001e0000e8c0002fffc
//sei 0000000106e501ec80000001e0e5668c0003fffff8
//海康摄像头ifrm尾部会加000001bd开头的数据, 这部分要去掉 否则播放正常但报"Invalid NAL unit 0"
//ifrm 0000000165xxx + 000001bdxxx, 海康私有标识 丢弃后看不到视频里移动侦测的红框
//pfrm 后面没有加 000001bdxxx
func FindAnnexbStartCode(d []byte, ct string) ([]*NaluInfo, error) {
	var err error
	l := len(d)
	if l < 3 {
		err = fmt.Errorf("NaluDataLen Must >= 3, data:0x%x", d)
		return nil, err
	}

	var nis []*NaluInfo
	for i := 0; i < l-2; i++ {
		//找0x000001, 找到之后看左边是不是0x00
		if d[i] == 0x00 && d[i+1] == 0x00 && d[i+2] == 0x01 {
			ni := &NaluInfo{}
			ni.BytePos = i
			ni.ByteNum = 3
			if i > 0 && d[i-1] == 0x00 {
				ni.BytePos = i - 1
				ni.ByteNum = 4
			}

			//0x000001出现在尾部时, 赋默认值 否则为空
			ni.Type = "unknow"
			switch ct {
			case "H264":
				//0x000001出现在尾部时, 防止崩溃
				if l-i > 3 {
					ni.Type = GetNaluTypeH264(d[i+3 : i+4])
				}
			case "H265":
				//0x000001出现在尾部时, 防止崩溃
				if l-i > 4 {
					ni.Type = GetNaluTypeH265(d[i+3 : i+5])
				}
			default:
				err = fmt.Errorf("undefined VideoCodecType %s", ct)
				return nil, err
			}

			nis = append(nis, ni)
		}
	}

	var s, e int
	niNum := len(nis)
	for i := 0; i < niNum; i++ {
		if i+1 == niNum {
			nis[i].ByteLen = l - nis[i].BytePos - nis[i].ByteNum
		} else {
			nis[i].ByteLen = nis[i+1].BytePos - nis[i].BytePos - nis[i].ByteNum
		}
		s = nis[i].BytePos + nis[i].ByteNum
		e = s + nis[i].ByteLen
		nis[i].Data = d[s:e]
	}
	return nis, nil
}

func GetNaluTypeH264(d []byte) string {
	var nh NaluHeader
	nh.ForbiddenZeroBit = (d[0] >> 7) & 0x1
	nh.NalRefIdc = (d[0] >> 5) & 0x3
	nh.NaluType = (d[0] >> 0) & 0x1f
	//log.Printf("%#v", nh)

	switch nh.NaluType {
	case 1: //P帧
		return "pfrm"
	case 5: //IDR
		return "ifrm"
	case 6: //SEI
		return "sei"
	case 7: //SPS
		return "sps"
	case 8: //PPS
		return "pps"
	default:
		return "unknow"
	}
	return "unknow"
}

func GetNaluTypeH265(d []byte) string {
	nh := NaluHeaderH265{}
	nh.ForbiddenZeroBit = d[0] >> 7
	nh.NalUnitType = (d[0] >> 1) & 0x3f
	nh.NuhLayerId = ((d[0] & 0x1) << 5) | (d[1]>>3)&0x1f
	nh.NuhTemporalIdPlus1 = d[1] & 0x7
	//log.Printf("%#v", nh)

	switch nh.NalUnitType {
	case 1: //P帧
		return "pfrm"
	case 19: //IDR
		return "ifrm"
	case 32: //VPS
		return "vps"
	case 33: //SPS
		return "sps"
	case 34: //PPS
		return "pps"
	case 35: //AUD
		return "aud"
	case 39: //SEI
		return "seipre"
	case 40: //SEI
		return "seisuf"
	default:
		return "unknow"
	}
	return "unknow"
}

func Annexb2Avcc(data []byte, nis []NaluInfo, fl int) []byte {
	var bl, fi, s uint32
	fd := make([]byte, fl)
	for i := 0; i < len(nis); i++ {
		bl = uint32(nis[i].ByteLen)
		if bl == 0 {
			continue
		}
		Uint32ToByte(bl, fd[fi:fi+4], BE)
		fi += 4
		s = uint32(nis[i].BytePos + nis[i].ByteNum)
		copy(fd[fi:fi+bl], data[s:s+bl])
		fi += bl
	}
	return fd
}

/*************************************************/
/* H264 nalu
/*************************************************/
/*
NalUnitType      uint8 //5bit
0		没有定义
1		非IDR图像编码的片, 比如:普通I, P帧, B帧
2		片分区A
3		片分区B
4		片分区C
5		IDR图像中的片, IDR帧一定是I帧 但是 I帧不一定是IDR帧
6		SEI, 补充增强信息单元
7		SPS
8		PPS
9		AUD(Access Unit Delimiter), 分隔符
10		序列结束
11		码流结束
12		填充数据
13-23	保留

0x61 (0 11 00001) Type=1  I帧 重要
0x41 (0 10 00001) Type=1  P帧 重要
0x01 (0 00 00001) Type=1  B帧 不重要
0x65 (0 11 00101) Type=5  IDR 非常重要
0x06 (0 00 00110) Type=6  SEI 不重要
0x67 (0 11 00111) Type=7  SPS 非常重要
0x68 (0 11 01000) Type=8  PPS 非常重要
NalUnitType=1时 可以用NRI来区分I/P/B帧类型
NalUnitType=5/7/8时 NRI必须是11

1-5,12 是vcl_nal单元携带编码数据, 其他为non_vcl_nal单元携带控制数据
1-23 都是 单个NAL单元包
*/
//+---------------+
//|0|1|2|3|4|5|6|7|
//+-+-+-+-+-+-+-+-+
//|F|NRI|  Type   |
//+---------------+
type NaluHeader struct {
	ForbiddenZeroBit uint8 //1bit, 必须为0, 1表示语法错误, 整包将被丢弃
	NalRefIdc        uint8 //2bit, 值越大约重要, 00:解码器可以丢弃,
	NaluType         uint8 //5bit
	NalUnitType      uint8 //不用这个, 最后要删除
}

type NaluHeaderH264 struct {
	F    uint8 //1bit, 1表示错误 整包将被丢弃
	NRI  uint8 //2bit, 重要性
	Type uint8 //5bit
}

//1 rtmp一个msg里 可能有多个nalu(vps+sps+pps+iData) 且 AVCPacketType==1, 这种情况 ServerNew 是否要兼容? 不需要兼容.
//通常情况(vps+sps+pps)	在一个msg里 且AVCPacketType==0是header
//通常情况(data)		在一个msg里 且AVCPacketType==1是数据
//2 rtmp一个msg里 可能有多个nalu(sei+iData), 这种情况 生成ts的时候 4字节01+sei, 4字节01+iData, ffmepg推流就是这样
//3 rtmp一个msg里 可能有多个nalu(iDataSlice1+iDataSlice2), 这种情况 生成ts的时候 4字节01+iDataSlice1, 3字节01+iDataSlice2. 编码器并发编码就会出这样的msg
//p帧也会出现 这种情况, 跟帧处理逻辑一样就行
//目前代码里是 4字节01+iDataSlice1, 4字节01+iDataSlice2, 播放器和解码器也能正常处理
//4 rtmp一个msg里 可能有多个nalu(pData1+pData2), 这种情况 不会出现
/*
sLiveGateway用rtmp推流h264/h265 msg里都是只有1个nalu
ffmpeg rtmp推流h264, msg里有时1个nalu, 有时2个nalu(sei+idr), 并不是每个idr前都有sei
ffmpeg rtmp推流h265, ???
len=5, main.NaluHeader{ForbiddenZeroBit:0x0, NalRefIdc:0x0, NalUnitType:0x6}
len=17166, main.NaluHeader{ForbiddenZeroBit:0x0, NalRefIdc:0x2, NalUnitType:0x1}
MsgLen=17179
naluNum=2
*/
func GetNaluNum(rs *Stream, c *Chunk, vc string) (uint32, []byte) {
	//前5个字节上面已经处理，从第6个字节开始
	//0x00, 0x00, 0x60, 0x50 为naluLen的值
	//iframe naluLen=24656
	//0x17, 0x01, 0x00, 0x00, 0x2a, 0x00, 0x00, 0x60, 0x50, 0x65,
	//0x88, 0x84, 0x00, 0xff, 0xfe, 0x9e, 0xbb, 0xe0, 0x53, 0x31,
	//0x60, 0x4f, 0xb4, 0x1f, 0xe0, 0x63, 0x6f, 0xea, 0xe7, 0x5f,
	var naluNum, naluLen, s, e uint32
	s = 5
	e = s + 4
	var i int
	var vd []byte
	for e < c.MsgLength {
		naluLen = ByteToUint32(c.MsgData[s:e], BE)

		switch vc {
		case "h264":
			nh := NaluHeader{}
			nh.ForbiddenZeroBit = c.MsgData[e] >> 7
			nh.NalRefIdc = (c.MsgData[e] >> 5) & 0x3
			nh.NalUnitType = c.MsgData[e] & 0x1f
			if rs != nil {
				//rs.log.Printf("%d, len=%d, %#v", i, naluLen, nh)
			}
			if nh.NalUnitType == 0x5 || nh.NalUnitType == 0x1 {
				vd = c.MsgData[e : e+naluLen]
			}
		case "h265":
			nh := NaluHeaderH265{}
			nh.ForbiddenZeroBit = c.MsgData[e] >> 7
			nh.NalUnitType = (c.MsgData[e] >> 1) & 0x3f
			nh.NuhLayerId = ((c.MsgData[e] & 0x1) << 5) | (c.MsgData[e+1]>>3)&0x1f
			nh.NuhTemporalIdPlus1 = c.MsgData[e+1] & 0x7
			if rs != nil {
				//rs.log.Printf("%d, len=%d, %#v", i, naluLen, nh)
			}
			if nh.NalUnitType == 0x5 || nh.NalUnitType == 0x1 {
				vd = c.MsgData[e : e+naluLen]
			}
		}

		i++
		s = e + naluLen
		e = s + 4
		naluNum++
	}
	//rs.log.Printf("MsgLen=%d", c.MsgLength-5)
	return naluNum, vd
}

/*************************************************/
/* H264 sps
/*************************************************/
// 序列参数集 (Sequence Paramater Set, SPS)
// SPS 记录了编码的 Profile、level、图像宽高等
// 从SPS帧解析视频分辨率
// https://blog.csdn.net/dxpqxb/article/details/17140239
// Go语言解码h264 SPS
// https://blog.csdn.net/sstya/article/details/121960964
// SPS PPS详解
// https://www.zhihu.com/question/35044089/answer/2539947043

//ISO_IEC_14496-10_2020_H264.pdf
//结构定义      7.3.2.1.1, P71
//结构参数解释  7.4.2.1.1, P102
//ProfileIdc         uint8 // 8bit
//H.264中定义了三种常用的档次profile
//44 0x2c  CAVLC 4:4:4 Intra
//66 0x42  baseline profile, 基准档次
//77 0x4d  main profile, 主要档次
//88 0x58  extended profile, 扩展档次
//新版标准中 还有 High、High 10、High 4:2:2、High 4:4:4、
//High 10 Intra、High 4:2:2 Intra、High 4:4:4 Intra、
//CAVLC 4:4:4 Intra等, 每一种都由不同的profile_idc
type Sps struct {
	ProfileIdc                     uint8 // 8bit, 确定码流符合哪一种档次
	ConstraintSet0Flag             uint8 // 1bit, 对码流增加限制性条件
	ConstraintSet1Flag             uint8 // 1bit, 对码流增加限制性条件
	ConstraintSet2Flag             uint8 // 1bit, 对码流增加限制性条件
	ConstraintSet3Flag             uint8 // 1bit, 对码流增加限制性条件
	ConstraintSet4Flag             uint8 // 1bit, 对码流增加限制性条件
	ConstraintSet5Flag             uint8 // 1bit, 对码流增加限制性条件
	ReservedZeroBits               uint8 // 2bit, 通常为 00
	LevelIdc                       uint8 // 8bit, 编码的Level定义了某种条件下的最大视频分辨率、最大视频帧率等参数
	SeqParameterSetId              uint  // uev, 序列参数集的id 0-31, 图像参数集pps通过这个id找sps
	Log2MaxFrameNumMinus4          uint  // uev, 用于计算MaxFrameNum的值
	PicOrderCntType                uint  // uev, 表示解码picture order count(POC)的方法
	MaxNumRefFrames                uint  // uev, 表示参考帧的最大数目
	GapsInFrameNumValueAllowedFlag uint  // 1bit, 说明frame_num中是否允许不连续的值
	PicWidthInMbsMinus1            uint  // uev, 用于计算图像宽度=(该值+1)*16, 宏块个数
	PicHeightInMapUnitsMinus1      uint  // uev, 用于计算图像高度=(该值+1)*16, 帧编码或场编码
	FrameMbsOnlyFlag               uint  // 1bit, 说明宏块的编码方式
	Direct8x8InferenceFlag         uint  // 1bit, 用于B_Skip, B_Direct模式运动矢量的推导计算
	FrameCroppingFlag              uint  // 1bit, 说明是否需要对输出的图像帧进行裁剪
	VuiParametersPresentFlag       uint  // 1bit, 说明SPS中是否存在VUI信息
}

// Exp-Golomb 指数哥伦布编码 (必读)
// https://blog.csdn.net/easyhao007/article/details/109896846
// https://blog.csdn.net/sstya/article/details/121960964
// 无符号整数 经过 0阶指数Golomb编码 变为 二进制数
// 二进制数 经过 0阶指数Golomb解码 变为 无符号整数
// 描述子ue(v) 表示数据是 无符号0阶指数Golomb编码
func GolombDecodeUev(data []byte, sBit *uint) (uint, error) {
	byteLen := uint(len(data))
	if *sBit >= byteLen*8 {
		err := fmt.Errorf("The data has been processed")
		log.Println(err)
		return 0, err
	}

	// 1 按位 数0的个数 遇到1停止, 记做bit0Num
	//   TODO: 0的个数不能超过32个
	// 2 读取 1后面 bit0Num位, 得到无符号整数bit0NumUint
	// 3 结果 = 2^bit0Num - 1 + bit0NumUint
	var bit0Num uint
	for *sBit < byteLen*8 {
		if (data[*sBit/8] & (0x80 >> (*sBit % 8))) > 0 {
			break
		}
		bit0Num++
		*sBit++
	}
	*sBit++

	if (*sBit + bit0Num) > byteLen*8 {
		err := fmt.Errorf("The data not enough processed")
		log.Println(err)
		return 0, err
	}

	var bit0NumUint, i uint
	for i = 0; i < bit0Num; i++ {
		bit0NumUint <<= 1
		if (data[*sBit/8] & (0x80 >> (*sBit % 8))) > 0 {
			bit0NumUint += 1
		}
		*sBit++
	}
	return (1 << bit0Num) - 1 + bit0NumUint, nil
}

// 描述子se(v) 表示数据是 有符号0阶指数Golomb编码
func GolombDecodeSev(data []byte, sBit *uint) (int, error) {
	// 解析se(v), 需要先调用ue(v)解析出值uev, 然后调用se(v)解析
	uev, err := GolombDecodeUev(data, sBit)
	if err != nil {
		log.Println(err)
		return 0, err
	}

	// Ceil为向上取整, ceil(2)=ceil(1.2)=cei(1.5)=2.0
	val := (int)(math.Ceil((float64)(uev) / 2))
	if uev%2 == 0 {
		val = -val
	}
	return val, nil
}

func ScalingList(sizeOfScalingList int, data []byte, sBit *uint) {
	var deltaScale int
	var lastScale int = 8
	var nextScale int = 8
	var err error

	for j := 0; j < sizeOfScalingList; j++ {
		if nextScale != 0 {
			deltaScale, err = GolombDecodeSev(data, sBit)
			if err != nil {
				log.Println(err)
				return
			}
			nextScale = (lastScale + deltaScale + 256) % 256
		}
		if nextScale != 0 {
			lastScale = nextScale
		}
	}
}

// 描述子u(n) 表示 连续n个bit转为无符号整数
func ReadBit2Uint(data []byte, sBit *uint, n uint) (uint, error) {
	var u uint
	if (*sBit + n) > uint(len(data))*8 {
		err := fmt.Errorf("The data not enough processed")
		log.Println(err)
		return 0, err
	}

	var i uint
	for i = 0; i < n; i++ {
		u <<= 1
		if (data[*sBit/8] & (0x80 >> (*sBit % 8))) > 0 {
			u += 1
		}
		*sBit++
	}
	return u, nil
}

//ffmpeg推流, sps 为1个，长度为 0x1c=28
//0x67, 0x4d, 0x40, 0x1f, 0xe8, 0x80, 0x28, 0x02, 0xdd, 0x80,
//0xb5, 0x01, 0x01, 0x01, 0x40, 0x00, 0x00, 0x03, 0x00, 0x40,
//0x00, 0x00, 0x0c, 0x03, 0xc6, 0x0c, 0x44, 0x80
//srs推流,	  sps 为1个，长度为 0x17=23
//0x67, 0x4d, 0x40, 0x28, 0x95, 0xa0, 0x1e, 0x0,  0x89, 0xf9,
//0x66, 0xc8, 0x0,  0x0,  0x3,  0x0,  0x8,  0x0,  0x0,  0x3,
//0x1,  0x44, 0x20
func SpsParse(data []byte) (*Sps, error) {
	var err error
	if len(data) == 0 {
		err = fmt.Errorf("data len is 0")
		return nil, err
	}

	//log.Printf("SpsData1:%d, %x", len(data), data)
	d := PreventionCodeWipe(data)
	//log.Printf("SpsData2:%d, %x", len(d), d)

	//sps长度要判断, 否则可能会崩溃
	//SpsData1:4, 674d002a
	//SpsData1:8, 67ac006367e03501
	if len(d) < 4 {
		err = fmt.Errorf("avc sps shall at least 4 bytes")
		log.Println(err)
		return nil, err
	}
	var nh NaluHeader
	nh.ForbiddenZeroBit = d[0] >> 7
	nh.NalRefIdc = (d[0] >> 5) & 0x3
	nh.NalUnitType = d[0] & 0x1f

	if nh.NalRefIdc == 0 {
		err = fmt.Errorf("NalRefIdc=%d, must != 0", nh.NalRefIdc)
		log.Println(err)
		return nil, err
	}

	if nh.NalUnitType != 7 {
		err = fmt.Errorf("this nalu is not sps, NalUnitType=%d", nh.NalUnitType)
		log.Println(err)
		return nil, err
	}

	var sps Sps
	sps.ProfileIdc = d[1]
	sps.ConstraintSet0Flag = d[2] >> 7
	sps.ConstraintSet1Flag = (d[2] >> 6) & 0x1
	sps.ConstraintSet2Flag = (d[2] >> 5) & 0x1
	sps.ConstraintSet3Flag = (d[2] >> 4) & 0x1
	sps.ConstraintSet4Flag = (d[2] >> 3) & 0x1
	sps.ConstraintSet5Flag = (d[2] >> 2) & 0x1
	sps.ReservedZeroBits = d[2] & 0x3
	sps.LevelIdc = d[3]

	var sBit uint
	sps.SeqParameterSetId, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var nn int = 8
	switch sps.ProfileIdc {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		ChromaFormatIdc, err := GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		if ChromaFormatIdc == 3 {
			_, err = ReadBit2Uint(d[4:], &sBit, 1) //SeparateColourPlaneFlag
			if err != nil {
				log.Println(err)
				return nil, err
			}
			nn = 12
		}
		_, err = GolombDecodeUev(d[4:], &sBit) //bit_depth_luma_minus8
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit) //bit_depth_chroma_minus8
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //qpprime_y_zero_transform_bypass_flag
		if err != nil {
			log.Println(err)
			return nil, err
		}

		SeqScalingMatrixPresentFlag, err := ReadBit2Uint(d[4:], &sBit, 1)
		if err != nil {
			log.Println(err)
			return nil, err
		}

		if SeqScalingMatrixPresentFlag == 1 {
			for i := 0; i < nn; i++ {
				//seq_scaling_list_present_flag[i]
				SeqScalingListPresentFlag, err := ReadBit2Uint(d[4:], &sBit, 1)
				if err != nil {
					log.Println(err)
					return nil, err
				}
				if SeqScalingListPresentFlag == 1 {
					if i < 6 {
						ScalingList(16, d[4:], &sBit)
					} else {
						ScalingList(64, d[4:], &sBit)
					}
				}
			}
		}
	}

	sps.Log2MaxFrameNumMinus4, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sps.PicOrderCntType, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if sps.PicOrderCntType == 0 {
		_, err = GolombDecodeUev(d[4:], &sBit) //log2_max_pic_order_cnt_lsb_minus4
		if err != nil {
			log.Println(err)
			return nil, err
		}
	} else if sps.PicOrderCntType == 1 {
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //delta_pic_order_always_zero_flag
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_non_ref_pic
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_top_to_bottom_field
		if err != nil {
			log.Println(err)
			return nil, err
		}
		NumRefFramesInPicOrderCntCycle, err := GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		for i := uint(0); i < NumRefFramesInPicOrderCntCycle; i++ {
			_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_ref_frame[i]
			if err != nil {
				log.Println(err)
				return nil, err
			}
		}
	}

	sps.MaxNumRefFrames, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sps.GapsInFrameNumValueAllowedFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	sps.PicWidthInMbsMinus1, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sps.PicHeightInMapUnitsMinus1, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	sps.FrameMbsOnlyFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if sps.FrameMbsOnlyFlag == 0 {
		//mb_adaptive_frame_field_flag 指明本序列是否属于帧场自适应模式
		//该值=0 表示本序列中的图像如果不是场模式就是帧模式
		//该值=1 表示本序列中的图像如果不是场模式就是帧场自适应模式
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //mb_adaptive_frame_field_flag
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}
	sps.Direct8x8InferenceFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sps.FrameCroppingFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if sps.FrameCroppingFlag == 1 {
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}
	sps.VuiParametersPresentFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	// var vp VuiParameters, 有fps
	// ISO_IEC_14496-10_2020_H264 E.1.1, P457
	if sps.VuiParametersPresentFlag == 1 {
		//这里可以得到 fps
		//log.Println("need to do something")
	}
	return &sps, nil
}

func SpsParse0(s *Stream, data []byte) (*Sps, error) {
	//s.log.Printf("SpsData1:%d, %x", len(data), data)
	d := PreventionCodeWipe(data)
	//s.log.Printf("SpsData2:%d, %x", len(d), d)

	//sps长度要判断, 否则可能会崩溃
	//SpsData1:4, 674d002a
	//SpsData1:8, 67ac006367e03501
	var err error
	if len(d) < 4 {
		err = fmt.Errorf("avc sps shall at least 4 bytes")
		s.log.Println(err)
		return nil, err
	}
	var nh NaluHeader
	nh.ForbiddenZeroBit = d[0] >> 7
	nh.NalRefIdc = (d[0] >> 5) & 0x3
	nh.NalUnitType = d[0] & 0x1f
	//s.log.Printf("%#v", nh)

	if nh.NalRefIdc == 0 {
		err = fmt.Errorf("NalRefIdc=%d, must != 0", nh.NalRefIdc)
		s.log.Println(err)
		return nil, err
	}

	if nh.NalUnitType != 7 {
		err = fmt.Errorf("this nalu is not sps, NalUnitType=%d", nh.NalUnitType)
		s.log.Println(err)
		return nil, err
	}

	var sps Sps
	sps.ProfileIdc = d[1]
	sps.ConstraintSet0Flag = d[2] >> 7
	sps.ConstraintSet1Flag = (d[2] >> 6) & 0x1
	sps.ConstraintSet2Flag = (d[2] >> 5) & 0x1
	sps.ConstraintSet3Flag = (d[2] >> 4) & 0x1
	sps.ConstraintSet4Flag = (d[2] >> 3) & 0x1
	sps.ConstraintSet5Flag = (d[2] >> 2) & 0x1
	sps.ReservedZeroBits = d[2] & 0x3
	sps.LevelIdc = d[3]

	var sBit uint
	sps.SeqParameterSetId, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}

	var nn int = 8
	switch sps.ProfileIdc {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		ChromaFormatIdc, err := GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		if ChromaFormatIdc == 3 {
			_, err = ReadBit2Uint(d[4:], &sBit, 1) //SeparateColourPlaneFlag
			if err != nil {
				s.log.Println(err)
				return nil, err
			}
			nn = 12
		}
		_, err = GolombDecodeUev(d[4:], &sBit) //bit_depth_luma_minus8
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit) //bit_depth_chroma_minus8
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //qpprime_y_zero_transform_bypass_flag
		if err != nil {
			s.log.Println(err)
			return nil, err
		}

		SeqScalingMatrixPresentFlag, err := ReadBit2Uint(d[4:], &sBit, 1)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}

		if SeqScalingMatrixPresentFlag == 1 {
			for i := 0; i < nn; i++ {
				//seq_scaling_list_present_flag[i]
				SeqScalingListPresentFlag, err := ReadBit2Uint(d[4:], &sBit, 1)
				if err != nil {
					s.log.Println(err)
					return nil, err
				}
				if SeqScalingListPresentFlag == 1 {
					if i < 6 {
						ScalingList(16, d[4:], &sBit)
					} else {
						ScalingList(64, d[4:], &sBit)
					}
				}
			}
		}
	}

	sps.Log2MaxFrameNumMinus4, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	sps.PicOrderCntType, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	if sps.PicOrderCntType == 0 {
		_, err = GolombDecodeUev(d[4:], &sBit) //log2_max_pic_order_cnt_lsb_minus4
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
	} else if sps.PicOrderCntType == 1 {
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //delta_pic_order_always_zero_flag
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_non_ref_pic
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_top_to_bottom_field
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		NumRefFramesInPicOrderCntCycle, err := GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		for i := uint(0); i < NumRefFramesInPicOrderCntCycle; i++ {
			_, err = GolombDecodeSev(d[4:], &sBit) //offset_for_ref_frame[i]
			if err != nil {
				s.log.Println(err)
				return nil, err
			}
		}
	}

	sps.MaxNumRefFrames, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	sps.GapsInFrameNumValueAllowedFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}

	sps.PicWidthInMbsMinus1, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	sps.PicHeightInMapUnitsMinus1, err = GolombDecodeUev(d[4:], &sBit)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}

	sps.FrameMbsOnlyFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	if sps.FrameMbsOnlyFlag == 0 {
		//mb_adaptive_frame_field_flag 指明本序列是否属于帧场自适应模式
		//该值=0 表示本序列中的图像如果不是场模式就是帧模式
		//该值=1 表示本序列中的图像如果不是场模式就是帧场自适应模式
		_, err = ReadBit2Uint(d[4:], &sBit, 1) //mb_adaptive_frame_field_flag
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
	}
	sps.Direct8x8InferenceFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	sps.FrameCroppingFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	if sps.FrameCroppingFlag == 1 {
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[4:], &sBit)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
	}
	sps.VuiParametersPresentFlag, err = ReadBit2Uint(d[4:], &sBit, 1)
	if err != nil {
		s.log.Println(err)
		return nil, err
	}
	// var vp VuiParameters, 有fps
	// ISO_IEC_14496-10_2020_H264 E.1.1, P457
	if sps.VuiParametersPresentFlag == 1 {
		//这里可以得到 fps
		//s.log.Println("need to do something")
	}
	return &sps, nil
}

/*************************************************/
/* H264 pps
/*************************************************/
type Pps struct {
}

//ffmpeg推流, pps 为1个，长度为 0x4
//0x68, 0xeb, 0xef, 0x20
//srs推流,    pps 为1个，长度为 0x4
//0x68, 0xee, 0x3c, 0x80

// 图像参数集 (Picture Paramater Set, PPS)
// 每一帧编码数据的参数保存于 PPS 中, 解码要用
func PpsParse(data []byte) *Pps {
	var pps Pps
	return &pps
}

/*************************************************/
/* H265 nalu
/*************************************************/
/*
ISO_IEC_23008-2_2020_H265 7.3.1.2, P45; Table 7-1 NalUnitType
https://zhuanlan.zhihu.com/p/608942708?utm_id=0
0x0201, type=01(0x01)	P帧???
0x2601, type=19(0x13)	I帧 IDR_W_RADL
0x4001, type=32(0x20)	VPS(视频参数集)
0x4201, type=33(0x21)	SPS(序列参数集)
0x4401, type=34(0x22)	PPS(图像参数及)
xxxxxx, type=35(0x23)	AUD
0x4E01, type=39(0x27)	SEI(补充增强信息) PREFIX_SEI_NUT
0x5001, type=40(0x28)	SEI(补充增强信息) SUFFIX_SEI_NUT
0x6001, type=48(0x30)	组合帧封装方式, 当帧较小且多个帧合并后小于MTU时, 可多帧封装到一个RTP包中,
						比如(VPS/SPS/PPS)合并封装, 注意多帧合并后大小必须小于MTU, 不然会被IP分片
0x6201, type=49(0x31)	Fua分片封装模式, 当视频帧大于MTU, 需要对帧进行分包发送, 从而避免IP层分片
Type 6bit 确定NAL的类型, 其中VCL NAL和non-VCL NAL各有32类
0-31是vcl nal单元, 32-63 是非vcl nal单元
VCL是指携带编码数据的数据流, 而non-VCL则是控制数据流
通常情况F=0, LayerId=0, TID=1
*/
//+-------------------------------+
//|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//|F|   Type    |  LayerId  | TID |
//+-+-----------+-----------+-----+
type NaluHeaderH265 struct {
	F    uint8 //1bit, 必须为0, 1表示语法错误 整包将被丢弃
	Type uint8 //6bit,
	LID  uint8 //6bit, 目前都是0, 为了HEVC的继续扩展设置
	Tid  uint8 //3bit, 此字段指定nal单元加1的时间标识符, tid的值为0是非法的

	ForbiddenZeroBit   uint8 // 1bit, 简写为F, 必须为0, 1表示语法错误, 整包将被丢弃
	NalUnitType        uint8 // 6bit, 简写为Type
	NuhLayerId         uint8 // 6bit, 简写为LayerId, 该字段目前都是0, 为了HEVC的继续扩展设置
	NuhTemporalIdPlus1 uint8 // 3bit, 简写为TID, 此字段指定nal单元加1的时间标识符, tid的值为0是非法的
}

/*************************************************/
/* H265 sps
/*************************************************/
// ISO_IEC_23008-2_2020_H265 7.3.2.2.1, P46
// 音视频基础：H265/HEVC&码流结构
// https://zhuanlan.zhihu.com/p/458497037
// HEVC解码中的SPS解析
// https://blog.csdn.net/baidu_35812312/article/details/78630427
type SpsH265 struct {
	SpsVideoParameterSetId   uint8 // 4bit, 指定了当前活动的VPS的ID号
	SpsMaxSubLayersMinus1    uint8 // 3bit, 该值+1表示引用该SPS的CVS所包含的最大时域子层数，取值范围0-6；本例取值为0，即只有1个时域子层;
	SpsTemporalIdNestingFlag uint8 // 1bit, 标识时域可分级中的帧间预测参考帧的限制信息
	SpsSeqParameterSetId     uint  // uev, 本SPS的ID值，此处取0
	ChromaFormatIdc          uint  // uev, 色度采样格式，此处取值为1，代表采用4:2:0格式
	PicWidthInLumaSamples    uint  // uev, 宽
	PicHeightInLumaSamples   uint  // uev, 高
	ConformanceWindowFlag    uint  // uev
	//...
}

func ProfileTierLevel(data []byte, sBit *uint, sps SpsH265) error {
	var err error
	_, err = ReadBit2Uint(data, sBit, 2) //general_profile_space
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 1) //general_tier_flag
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 5) //general_profile_idc
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 32) //general_profile_compatibility_flag
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 1) //general_progressive_source_flag
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 1) //general_interlaced_source_flag
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 1) //general_non_packed_constraint_flag
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 1) //general_frame_only_constraint_flag
	if err != nil {
		log.Println(err)
		return err
	}

	// 44 = 32 + 12
	_, err = ReadBit2Uint(data, sBit, 32) //if general_profile_idc == 4 ...
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = ReadBit2Uint(data, sBit, 12) //if general_profile_idc == 4 ...
	if err != nil {
		log.Println(err)
		return err
	}

	_, err = ReadBit2Uint(data, sBit, 8) //general_level_idc
	if err != nil {
		log.Println(err)
		return err
	}

	SubLayerProfilePresentFlag := make([]uint, 7)
	SubLayerLevelPresentFlag := make([]uint, 7)
	var i uint8
	for i = 0; i < sps.SpsMaxSubLayersMinus1; i++ {
		SubLayerProfilePresentFlag[i], err = ReadBit2Uint(data, sBit, 1)
		if err != nil {
			log.Println(err)
			return err
		}
		SubLayerLevelPresentFlag[i], err = ReadBit2Uint(data, sBit, 1)
		if err != nil {
			log.Println(err)
			return err
		}
	}
	if sps.SpsMaxSubLayersMinus1 > 0 {
		for i = sps.SpsMaxSubLayersMinus1; i < 8; i++ {
			_, err = ReadBit2Uint(data, sBit, 2) // reserved_zero_2bits
			if err != nil {
				log.Println(err)
				return err
			}
		}
	}
	for i = 0; i < sps.SpsMaxSubLayersMinus1; i++ {
		if SubLayerProfilePresentFlag[i] != 0 {
			_, err = ReadBit2Uint(data, sBit, 2) //sub_layer_profile_space
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 1) //sub_layer_tier_flag
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 5) //sub_layer_profile_idc
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 32) //sub_layer_profile_compatibility_flag
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 1) //sub_layer_progressive_source_flag
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 1) //sub_layer_interlaced_source_flag
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 1) //sub_layer_non_packed_constraint_flag
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 1) //sub_layer_frame_only_constraint_flag
			if err != nil {
				log.Println(err)
				return err
			}
			// 44 = 32 + 12
			_, err = ReadBit2Uint(data, sBit, 32) //if sub_layer_profile_idc[i] == 4 ...
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = ReadBit2Uint(data, sBit, 12) //if sub_layer_profile_idc[i] == 4 ...
			if err != nil {
				log.Println(err)
				return err
			}
		}
		if SubLayerLevelPresentFlag[i] != 0 {
			_, err = ReadBit2Uint(data, sBit, 8) //sub_layer_level_idc
			if err != nil {
				log.Println(err)
				return err
			}
		}
	}

	return nil
}

// srs推流,	  sps 为1个，长度为 0x3c=60
//	42 01 01 01
// 60 00 00 03 00 80 00 00 03 00
// 00 03 00 78 a0 02 80 80 2d 1f
// e3 6b bb c9 2e b0 16 e0 20 20
// 20 80 00 01 f4 00 00 30 d4 39
// 0e f7 28 80 3d 30 00 44 de 00
// 7a 60 00 89 bc 40
// H265编码 SPS分析
// https://blog.csdn.net/sunlifeall/article/details/118437033
func SpsParseH265(s *Stream, data []byte) (*SpsH265, error) {
	s.log.Printf("SpsData1:%d, %x", len(data), data)
	d := PreventionCodeWipe(data)
	s.log.Printf("SpsData2:%d, %x", len(d), d)

	var err error
	if len(d) < 3 {
		err = fmt.Errorf("hevc sps shall at least 3 bytes")
		s.log.Println(err)
		return nil, err
	}

	var nh NaluHeaderH265
	nh.ForbiddenZeroBit = d[0] >> 7
	nh.NalUnitType = (d[0] >> 1) & 0x3f
	nh.NuhLayerId = ((d[0] & 0x1) << 5) | (d[1]>>3)&0x1f
	nh.NuhTemporalIdPlus1 = d[1] & 0x7
	//s.log.Printf("%#v", nh)

	if nh.NalUnitType != 33 {
		err = fmt.Errorf("this nalu is not sps, NalUnitType=%d", nh.NalUnitType)
		s.log.Println(err)
		return nil, err
	}

	var sps SpsH265
	sps.SpsVideoParameterSetId = (d[2] >> 4) & 0xf
	sps.SpsMaxSubLayersMinus1 = (d[2] >> 1) & 0x7
	sps.SpsTemporalIdNestingFlag = d[2] & 0x1

	var sBit uint
	err = ProfileTierLevel(d[3:], &sBit, sps)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	sps.SpsSeqParameterSetId, err = GolombDecodeUev(d[3:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sps.ChromaFormatIdc, err = GolombDecodeUev(d[3:], &sBit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if sps.ChromaFormatIdc == 3 {
		_, err = ReadBit2Uint(d[3:], &sBit, 1) //separate_colour_plane_flag
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}

	sps.PicWidthInLumaSamples, err = GolombDecodeUev(d[3:], &sBit)
	sps.PicHeightInLumaSamples, err = GolombDecodeUev(d[3:], &sBit)
	sps.ConformanceWindowFlag, err = ReadBit2Uint(d[3:], &sBit, 1)
	if sps.ConformanceWindowFlag == 1 {
		_, err = GolombDecodeUev(d[3:], &sBit) //conf_win_left_offset
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[3:], &sBit) //conf_win_right_offset
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[3:], &sBit) //conf_win_top_offset
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = GolombDecodeUev(d[3:], &sBit) //conf_win_bottom_offset
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}
	// ...
	return &sps, nil
}

/*************************************************/
/* H265 pps
/*************************************************/
type PpsH265 struct {
}

func PpsH265Parse(data []byte) *Pps {
	var pps Pps
	return &pps
}
