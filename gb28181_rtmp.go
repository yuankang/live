package main

import (
	"fmt"
	"net"
	"time"
)

func Frame2Chunk(s *RtmpStream, p AvPacket) Chunk {
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
func Gb281812Mem2RtmpServer(s *RtmpStream) {
}

/*************************************************/
/* Gb28181媒体数据走网络发送给别的RtmpServer
/*************************************************/
func Gb28181Net2RtmpServer(s *RtmpStream) {
	c, err := net.Dial("tcp", "127.0.0.1:1935")
	if err != nil {
		s.log.Println(err)
		return
	}
	//defer c.Close()

	sm, err := NewRtmpStream(c)
	if err != nil {
		sm.log.Println(err)
		return
	}

	/*
		s.StreamType = "RtmpPusher"
		s.RemoteAddr = addr
		s.RemoteIp = ip
		s.RemotePort = port
		s.App = app
		s.StreamId = sid
	*/

	sm.log.Println("==============================")
	sm.log.Printf("RecFile:%s", sm.RecRtmpFn)

	if err := RtmpHandshakeClient(sm); err != nil {
		sm.log.Println(err)
		return
	}
	sm.log.Println("RtmpServerHandshake ok")

	err = SendConnMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	sm.log.Println("SendConnMsg() ok")

	err = SendCreateStreamMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	sm.log.Println("SendCreateStreamMsg() ok")

	err = SendPublishMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	err = RecvMsg(sm)
	if err != nil {
		sm.log.Println(err)
		return
	}
	sm.log.Println("SendPublishMsg() ok")

	//下面就要接收并发送数据了
	//先启动播放转发协程, 再添加到发布者的players里
	//sm.FrameChan = make(chan Frame, conf.PlayStockMax)
	sm.FrameChan = make(chan Chunk, 100)
	sm.NewPlayer = true
	//go RtmpTransmit(sm)
	sm.log.Printf("%s RtmpTransmit() start", s.AmfInfo.StreamId)

	sm.Key = fmt.Sprintf("%s_%s_%s", s.App, s.StreamId, sm.RemoteAddr)
	sm.log.Println("player key is", sm.Key)
	s.Players.Store(sm.Key, sm)
}
