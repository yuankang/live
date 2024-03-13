package main

import (
	"fmt"
	"log"
	"time"
	"utils"
)

//1 发送sps, pps; 2 发送缓存音视频; 3 发送实时数据;
func RtspSendRtmpGop(rs *Stream, s *RtspStream) {
	s.log.Println(">>> send Rtp Stapa(聚合包) sps_pps <<<")
	e := rs.GopCache.MediaData.Front()
	var c *Chunk
	c = (e.Value).(*Chunk)
	s.log.Printf("type:%d(%s), ts=%d, len=%d, naluNum:%d", c.MsgTypeId, c.DataType, c.Timestamp, c.MsgLength, c.NaluNum)

	s.log.Printf("sps:%x", rs.AvcC.SpsData)
	s.log.Printf("pps:%x", rs.AvcC.PpsData)
	rtp, err := RtpStapaPktCreate(s, rs.AvcC.SpsData, rs.AvcC.PpsData, c.Timestamp)
	if err != nil {
		s.log.Println(err)
		return
	}
	//1 发送sps, pps
	RtspSendMediaData(rs, s, rtp)

	//2 发送缓存音视频, rtp包最大1476字节 4+12+1460
	s.log.Println("--- send RtmpGopCacheData start ---")
	var i int
	for e := rs.GopCache.MediaData.Front(); e != nil; e = e.Next() {
		c = (e.Value).(*Chunk)
		i++

		s.log.Printf("%d: type:%d(%s), ts=%d, len=%d, naluNum:%d", i, c.MsgTypeId, c.DataType, c.Timestamp, c.MsgLength, c.NaluNum)

		rps, err := RtpPkgCreate(rs, s, c)
		if err != nil {
			s.log.Println(err)
			continue
		}
		//s.log.Printf("rpsLen=%d", len(rps))
		for j := 0; j < len(rps); j++ {
			RtspSendMediaData(rs, s, rps[j])
		}
	}
	s.log.Println("--- send RtmpGopCacheData stop ---")
}

func RtspSendMediaData(rs *Stream, s *RtspStream, rp *RtpPacket) error {
	//s.log.Printf("%#v", rp.RtpHeader)

	id, err := AddInterleavedMode0(rp)
	if err != nil {
		s.log.Println(err)
		return err
	}
	//s.log.Printf("%x", id.Data)

	//s.log.Printf("vRtpCID=%d, vRtcpCID=%d, aRtpCID=%d, aRtcpCID=%d, thisCID=%d", s.VideoRtpChanId, s.VideoRtcpChanId, s.AudioRtpChanId, s.AudioRtcpChanId, id.CID)
	switch int(id.CID) {
	case s.AudioRtpChanId, s.VideoRtpChanId:
		_ = RtspRtpHandler(s, id.Data, false)
	case s.AudioRtcpChanId, s.VideoRtcpChanId:
		_ = RtspRtcpHandler(s, id.Data)
	default:
		s.log.Printf("undefined ChannelId=%d", id.CID)
	}
	return nil
}

func RtmpMem2RtspServer(rs *Stream) {
	s := NewRtspStream(nil)
	s.log.Println("----------")
	s.Key = rs.Key
	s.log.Printf("PuberKey:%s", s.Key)

	fn := fmt.Sprintf("%s/%s/publish_rtsp_%d.log", conf.Log.StreamLogPath, rs.StreamId, utils.GetTimestamp("ns"))
	StreamLogRename(s.LogFn, fn)
	s.LogFn = fn

	i := 0
	for i = 0; i < 10; i++ {
		if rs.AvcC != nil && rs.AacC != nil {
			break
		}
		s.log.Printf("rs.Avcc or rs.AacC == nil, wait%d for moment", i)
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		s.log.Printf("%s RtmpMem2RtspServer stop", s.Key)
		return
	}

	s.log.Printf("Sps:%x", rs.AvcC.SpsData)
	s.log.Printf("Pps:%x", rs.AvcC.PpsData)
	s.log.Printf("%#v", rs.AacC)

	var err error
	s.Sdp, err = CreateSdpUseSpsPps(rs.AvcC.SpsData, rs.AvcC.PpsData)
	if err != nil {
		s.log.Println(err)
		return
	}
	s.log.Printf("width:%d, height:%d, RawSdp:%s", s.Sdp.Width, s.Sdp.Height, string(s.Sdp.RawSdp))

	log.Printf("PuberKey=%s(rtsp)", s.Key)
	s.log.Printf("PuberKey=%s(rtsp)", s.Key)
	RtspPuberMap.Store(s.Key, s)

	go RtspMem2RtspPlayers(s)
	//go RtspRtpCacheSort(s)

	//开始通过chan接收数据并发送给rtsp播放者
	var ok bool
	i = 0
	for {
		var p Chunk
		p, ok = <-rs.AvPkg2RtspChan
		if ok == false {
			s.log.Printf("%s RtmpMem2RtspServer() stop", rs.StreamId)
			close(s.Rtp2RtspChan)
			close(s.Rtp2RtmpChan)
			return
		}
		MessageTypeCheck(&p)

		if p.DataType == "DataAmfx" || p.DataType == "VideoHeader" || p.DataType == "AudioHeader" {
			continue
		}

		rps, err := RtpPkgCreate(rs, s, &p)
		if err != nil {
			s.log.Println(err)
			continue
		}
		s.log.Printf("%d, type:%d(%s), ts=%d, len=%d, naluNum:%d, rpsLen=%d", i, p.MsgTypeId, p.DataType, p.Timestamp, p.MsgLength, p.NaluNum, len(rps))
		i++

		for j := 0; j < len(rps); j++ {
			RtspSendMediaData(rs, s, rps[j])
		}
	}
}
