package main

import (
	"fmt"
	"io"
)

//|<------------------- Chunk Header ----------------->|
//+--------------+----------------+--------------------+--------------+
//| Basic Header | Message Header | Extended Timestamp |  Chunk Data  |
//+--------------+----------------+--------------------+--------------+
//Basic   Header: 1/2/3字节,
//	1字节时 fmt(2b) + csid(6b)
//	2字节时 fmt(2b) + 0(6b) + csid(8b)
//	3字节时 fmt(2b) + 1(6b) + csid(16b)
//rtmp协议最多支持65597个用户, 2的16次方=65536, 65536-3+64=65597
//csid范围为[3，65599], 0/1/2被协议规范直接使用
//csid=0, 代表Basic Header占用2个字节, CSID在 [64,   319] 之间
//csid=1, 代表Basic Header占用3个字节，CSID在 [64, 65599] 之间
//csid=2, 代表该chunk是控制信息和一些命令信息
//Message Header: 11/7/3/0字节
//	11字节时fmt=0
//	7字节时 fmt=1
//	3字节时 fmt=2
//	0字节时 fmt=3

// 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//|fmt|   csid    |					timestamp					  |
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//|                 message length                |  msg type id  |
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//|                       message stream id                       |
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//|                      extended timestamp						  |
//+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//Timestamp
//24bit, 最大值  0xffffff=  16777215单位毫秒 约为 4.7小时 约为0.20天
//32bit, 最大值0xffffffff=4294967295单位毫秒 约为1203小时 约为49.7天
//MsgTypeId
//协议控制消息(Protocol Control Message)	1, 2, 3, 5, 6
//	控制消息的接受端会忽略掉chunk中的时间戳
//	控制消息必须MsgStreamId=0, Csid=2, MsgType=1/2/3/5/6
//	MsgType=1, Set Chunk Size, 默认128, 建议4096
//	MsgType=2, Abort Message
//	MsgType=3, Acknowledgement
//	MsgType=5, Set Window Acknowledgement Size
//	MsgType=6, Set Peer Bandwidth
//用户控制消息(User Control Message)		4
//音频消息(Audio Message)					8
//视频消息(Video Message)					9
//命令消息(Command Message)					17, 20
//	17表示用AMF3编码, 20表示用AMF0编码
//  命令详细分两类: 连接相关命令, 流控相关命令
//	连接相关命令: Connect(建连), Call(调函数), CreateStream等
//	流控相关命令: play, play2(切码率), publish, seek, pause等
//  接收端收到命令后 需返回三种消息中的一种:
//	  _result    表示接受, 对端可以继续往下执行流程
//    _error     表示拒绝
//    MethodName 表示要发送端执行的函数名称
//    返回消息要携带接收消息中的TransactionId, 用于区分回应哪个命令
//数据消息(Data Message)					15, 18
//共享消息(Shared Object Message)			16, 19
//聚合消息(Aggregate Message)				22
//DataType
//"Metadata", "VideoHeader", "AudioHeader",
//"VideoKeyFrame", "VideoInterFrame", "AudioAacFrame"
type Chunk struct {
	//FmtFirst    uint32 // 2bit, 发送的时候要用
	Fmt         uint32 // 2bit, format (其实应该叫MsgHeadFmt)
	Csid        uint32 // 6/14/22bit, 小端字节序, chunk stream id
	Timestamp   uint32 // 24bit, Extended Timestamp 32bit 也用这个
	TimeExtend  bool   // 8bit
	TimeDelta   uint32 // 24bit
	MsgLength   uint32 // 24bit, MsgData的长度
	MsgTypeId   uint32 // 8bit
	MsgStreamId uint32 // 32bit, 小端字节序
	MsgData     []byte
	MsgIndex    uint32 // MsgData 已经写入数据的下标
	MsgRemain   uint32 // MsgData 还有多少数据需要接收
	Full        bool   // 8bit
	DataType    string
	MsgLenMax   uint32 // 音视频的chunk要复用, MsgLenght > MsgLenMax 要重新make
	NaluNum     uint32 //当前消息中, 有几个nalu, 写ts文件时, 每个nalu前都要加开始码
}

func ChunkHeaderAssemble(s *Stream, c *Chunk) error {
	var err error
	bh := c.Fmt << 6
	switch {
	case c.Csid < 64:
		bh |= c.Csid
		err = WriteUint32(s.Conn, BE, bh, 1)
	case c.Csid-64 < 320:
		bh |= 0
		err = WriteUint32(s.Conn, BE, bh, 1)
		if err != nil {
			s.log.Println(err)
			return err
		}
		err = WriteUint32(s.Conn, BE, c.Csid-64, 1)
	case c.Csid-64 < 65600:
		bh |= 1
		err = WriteUint32(s.Conn, BE, bh, 1)
		if err != nil {
			s.log.Println(err)
			return err
		}
		err = WriteUint32(s.Conn, BE, c.Csid-64, 2)
	}
	if err != nil {
		s.log.Println(err)
		return err
	}

	//fmt: 控制Message Header的类型, 0表示11字节, 1表示7字节, 2表示3字节, 3表示0字节
	//csid: 0表示2字节形式, 1表示3字节形式, 2用于协议控制消息和命令消息, 3-65599表示块流id
	if c.Fmt == 3 {
		goto END
	}

	//至少是3字节
	if c.Timestamp > 0xffffff {
		err = WriteUint32(s.Conn, BE, 0xffffff, 3)
	} else {
		err = WriteUint32(s.Conn, BE, c.Timestamp, 3)
	}
	if err != nil {
		s.log.Println(err)
		return err
	}

	if c.Fmt == 2 {
		goto END
	}

	//至少是7字节
	err = WriteUint32(s.Conn, BE, c.MsgLength, 3)
	if err != nil {
		s.log.Println(err)
		return err
	}
	err = WriteUint32(s.Conn, BE, c.MsgTypeId, 1)
	if err != nil {
		s.log.Println(err)
		return err
	}

	if c.Fmt == 1 {
		goto END
	}

	//就是11字节, 协议文档说StreamId用小端字节序
	err = WriteUint32(s.Conn, LE, c.MsgStreamId, 4)
	if err != nil {
		s.log.Println(err)
		return err
	}
END:
	if c.Timestamp > 0xffffff {
		WriteUint32(s.Conn, BE, c.Timestamp, 4)
	}
	return nil
}

// fmt: 控制Message Header的类型, 0表示11字节, 1表示7字节, 2表示3字节, 3表示0字节
// 音频的fmt顺序 一般是0 2 3 3 3 3 3 3 3 3 3 //理想状态 数据量和时间增量相同
// 音频的fmt顺序 一般是0 1 1 1 2 1 3 1 1 2 1 //实际情况 数据量偶尔同 时间增量偶尔同
// 视频的fmt顺序 一般是0 3 3 3 1 1 3 3 1 3 3 //理想状态和实际情况一样
// 块大小默认是128, 通常会设置为1024或2048, 音频消息约400字节, 视频消息约700-30000字节
// c.Fmt=0 肯定是新Message, 要分配buf, 初始化buf索引
// c.Fmt=1 肯定是新Message, 要分配buf, 初始化buf索引, MsgStreamId同上个
// c.Fmt=2 肯定是新Message, 要复用buf, 初始化buf索引, MsgLength, MsgTypeId, MsgStreamId同上个
// c.Fmt=3 可能是新Message, 要复用buf, 初始化buf索引, TimeDelta, MsgLength, MsgTypeId, MsgStreamId同上个
// c.Fmt=3 可能是某Message后续数据,无需初始化buf索引, TimeDelta, MsgLength, MsgTypeId, MsgStreamId同上个
func ChunkAssemble(s *Stream, c *Chunk) error {
	var err error
	switch c.Fmt {
	case 0:
		c.Timestamp, err = ReadUint32(s.Conn, 3, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		c.MsgLength, err = ReadUint32(s.Conn, 3, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		c.MsgTypeId, err = ReadUint32(s.Conn, 1, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		c.MsgStreamId, err = ReadUint32(s.Conn, 4, LE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		if c.Timestamp == 0xffffff {
			c.Timestamp, err = ReadUint32(s.Conn, 4, BE)
			if err != nil {
				s.log.Println(err)
				return err
			}
			c.TimeExtend = true
		} else {
			c.TimeExtend = false
		}

		c.MsgData = make([]byte, c.MsgLength)
		c.MsgIndex = 0
		c.MsgRemain = c.MsgLength
		c.Full = false
	case 1:
		c.TimeDelta, err = ReadUint32(s.Conn, 3, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		c.MsgLength, err = ReadUint32(s.Conn, 3, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		c.MsgTypeId, err = ReadUint32(s.Conn, 1, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		if c.TimeDelta == 0xffffff {
			c.TimeDelta, err = ReadUint32(s.Conn, 4, BE)
			if err != nil {
				s.log.Println(err)
				return err
			}
			c.TimeExtend = true
		} else {
			c.TimeExtend = false
		}
		c.Timestamp += c.TimeDelta

		c.MsgData = make([]byte, c.MsgLength)
		c.MsgIndex = 0
		c.MsgRemain = c.MsgLength
		c.Full = false
	case 2:
		c.TimeDelta, err = ReadUint32(s.Conn, 3, BE)
		if err != nil {
			s.log.Println(err)
			return err
		}
		if c.TimeDelta == 0xffffff {
			c.TimeDelta, err = ReadUint32(s.Conn, 4, BE)
			if err != nil {
				s.log.Println(err)
				return err
			}
			c.TimeExtend = true
		} else {
			c.TimeExtend = false
		}
		c.Timestamp += c.TimeDelta

		c.MsgData = make([]byte, c.MsgLength)
		c.MsgIndex = 0
		c.MsgRemain = c.MsgLength
		c.Full = false
	case 3:
		//可能有4字节的扩展时间戳
		if c.TimeExtend == true {
			c.TimeDelta, _ = ReadUint32(s.Conn, 4, BE)
		}

		// 通常 音频c.Fmt=3 为新Message, 要复用buf, 初始化buf索引
		// 通常 视频c.Fmt=3 为某Message后续数据,无需初始化buf索引
		// c.MsgRemain == 0 表示这是新Message
		// c.MsgRemain != 0 表示这是某Message后续数据
		if c.MsgRemain == 0 {
			//音频新Msg数据
			c.Timestamp += c.TimeDelta
			c.MsgData = make([]byte, c.MsgLength)
			c.MsgIndex = 0
			c.MsgRemain = c.MsgLength
			c.Full = false
		} else {
			//视频某Msg后续数据, 无需申请buf
		}
	default:
		return fmt.Errorf("Invalid fmt=%d", c.Fmt)
	}
	//s.log.Printf("fmt=%d, csid=%d, Timestamp=%d, TimeDelta=%d, MsgLength=%d, MsgTypeId=%d, MsgStreamId=%d", c.Fmt, c.Csid, c.Timestamp, c.TimeDelta, c.MsgLength, c.MsgTypeId, c.MsgStreamId)

	//srs(gateway)发送数据使用默认chunkSize=128, 已经改为1024
	//TODO: 发送方使用chunkSize=128, 不能引起cpu过高
	size := c.MsgRemain
	if size > s.ChunkSize {
		size = s.ChunkSize
	}

	buf := c.MsgData[c.MsgIndex : c.MsgIndex+size]
	//if _, err := s.Conn.Read(buf); err != nil {}
	_, err = io.ReadFull(s.Conn, buf)
	if err != nil {
		s.log.Println(err)
		return err
	}

	if c.MsgRemain < size {
		s.log.Printf("error: %d < %d", c.MsgRemain, size)
	}

	c.MsgIndex += size
	c.MsgRemain -= size
	if c.MsgRemain == 0 {
		c.Full = true
		/*
			// for test, 为了不打印大量的音视频数据
			d := c.MsgData
			c.MsgData = nil
			s.log.Printf("%#v", c)
			c.MsgData = d
		*/
	}
	return nil
}
