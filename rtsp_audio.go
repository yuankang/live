package main

import (
	"fmt"
	"log"
)

/*
rtsp_rtp中aac格式, 主要包含4部分, AuxiliarySection基本不用
               <-------------- Rtp Pakcet Payload -------------->
+---------------------------------------------------------------+
|  RtpHeader   |  AuHeader   |  Auxiliary   |  AccessUnitData   |
|              |  Section    |  Section     |  Section          |
+---------------------------------------------------------------+
MPEG Access Units 简称AU, 可以理解成可解码的最小数据单元,
在audio stream 中就是一个 audio frame

AuHeaderSection格式
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|        AuHeadersLength        |          AuHeader(1)          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|          AuHeader(2)          |          AuHeader(n)          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           AuData(1)                      |      AuData(2)     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       AuData(n)                       |
+--------------------------------------------------------
RtpHeader(12字节) + AuHeadersLength(2字节) + AuHeader(2字节) + AuData

https://blog.csdn.net/mo4776/article/details/103864169
https://blog.csdn.net/u010140427/article/details/127773028
rfc3640_rtp(mpeg4_aac).pdf
Low  Bit-rate AAC 中AuData 最大长度  63字节(6bit)
High Bit-rate AAC 中AuData 最大长度8191字节(13bit)

AuHeadersLength 单位是bit, 16/8/2=1 表示后面有1个AuHeader
AuHeader, 一般是高13bit(由sdp中sizelength=13决定, sdp中indexlength=3)表示AuData的长度, 第1个字节8bit + 第2个字节高位5bit
通常情况, ⼀个rtp中只有⼀个aac包, 不需要加再AuHeadersLength和AuHeader
AuHeader = AuDataSize(13bit) + AuIndex(序号)/AuIndexDelta(序号差)
AuData 是不带adts的aac数据
AuData 有多个时 每个AuData都是一个AvPacket, 这样可以减少发送次数
每个AuData 的时间增量 是1024份 约为1024*1000/11025=92.88毫秒
*/

//sdp中sizelength=13 + indexlength=3 决定 AuHeader的字节数
type AuHeader struct {
	AuSize       int //一般13bit, sdp中sizelength=13决定
	AuIndex      int //一般 3bit, sdp中indexlength=3决定
	AuIndexDelta int //一般 3bit, sdp中indexdeltalength=3决定
	CTSflag      int //一般没有
	CTSdelta     int //一般没有
	DTSflag      int //一般没有
	DTSdelta     int //一般没有
	RAPflag      int //一般没有
	StreamState  int //一般没有
}

func Rtp2AacPacket(rs *RtspStream, rp *RtpPacket) ([]*AvPacket, error) {
	//log.Printf("%d, RtpAacData:%x", len(rp.Data[12:]), rp.Data[12:])
	var n uint32 = 12
	var err error
	var AacLen []uint16
	var ps []*AvPacket

	l := ByteToUint16(rp.Data[n:n+2], BE)
	n += 2

	if l%16 != 0 {
		err = fmt.Errorf("RtpAac AuHeaderLen=%d error", l)
		log.Println(err)
		return ps, err
	}

	AuHeaderNum := l / 16
	for i := 0; i < int(AuHeaderNum); i++ {
		m := ByteToUint16(rp.Data[n:n+2], BE)
		//idx := m & 0x7
		m = m >> 3
		AacLen = append(AacLen, m)
		n += 2
		//log.Printf("rtpLen=%d rtpTs=%d, AacData%d len=%d idx=%d", rp.Len, rp.Timestamp, i, m, idx)
	}

	for i := 0; i < int(AuHeaderNum); i++ {
		m := uint32(AacLen[i])
		//log.Printf("RtpAacHeader %d len=%d", i, m)
		pp := &AvPacket{}
		pp.Type = "AudioAacFrame"
		pp.Timestamp = (rp.Timestamp + uint32(i)*1024) / 11
		pp.Data = rp.Data[n : n+m]
		n += m
		ps = append(ps, pp)
	}
	//log.Printf("RtpAacRawDataLen=%d", len(p.Data))
	return ps, nil
}

func Rtp2G711aPacket(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	err := fmt.Errorf("audio g711a need handle")
	return err
}

func Rtp2G711uPacket(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	err := fmt.Errorf("audio g711u need handle")
	return err
}
