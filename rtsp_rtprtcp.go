package main

import (
	"fmt"
)

/*************************************************/
/* rtcp
/*************************************************/
func RtspRtcpHandler(rs *RtspStream, d []byte) error {
	//rs.log.Printf("rtcp data:%x", d)
	rh := RtcpParse(d)
	rs.log.Printf("%#v", rh)
	return nil
}

/*************************************************/
/* rtp RTP协议封装H264/H265/AAC
/* https://zhuanlan.zhihu.com/p/608570240?utm_id=0
/*************************************************/
func DeleteMapValue(rs *RtspStream, rps []*RtpPacket, isVideo bool) {
	rpq := rs.VideoRtpPkgs
	//rpt := "VideoRtpPkgMap"
	if isVideo == false {
		rpq = rs.AudioRtpPkgs
		//rpt = "AudioRtpPkgMap"
	}

	l := len(rps)
	for i := 0; i < l; i++ {
		rpq.PkgMap.Delete(rps[i].SeqNumber)
		//rs.log.Printf("%s delete seqnum %d", rpt, rps[i].SeqNumber)
	}
}

func RtspRtps2VideoPacket(rs *RtspStream, rps []*RtpPacket) (*AvPacket, error) {
	var err error
	var rp *RtpPacket
	p := &AvPacket{}
	defer DeleteMapValue(rs, rps, true)

	l := len(rps)
	for i := 0; i < l; i++ {
		rp = rps[i]
		//rs.log.Printf("P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, len=%d", rp.Padding, rp.Extension, rp.CsrcCount, rp.Marker, rp.PayloadType, rp.PtStr, rp.SeqNumber, rp.Timestamp, rp.Ssrc, rp.Len)

		//1000毫秒分成90000份, 每份时长1/90毫秒
		//fps=20, 两帧时间差=1000/20=50毫秒, 两帧份数差=50/(1/90)=4500份
		//fps=25, 两帧时间差=1000/25=40毫秒, 两帧份数差=40/(1/90)=3600份
		//两帧份数差=3600, 两帧时间差=3600*(1/90)=40毫秒
		//两帧份数差=4500, 两帧时间差=4500*(1/90)=50毫秒
		p.Timestamp = rp.Timestamp / 90

		switch rs.Sdp.VideoPayloadTypeStr {
		case "H264", "h264":
			err = Rtp2H264Packet(rs, p, rp)
		case "H265", "h265":
			err = Rtp2H265Packet(rs, p, rp)
		default:
			err = fmt.Errorf("undefined VideoPayloadType %d(%s)", rs.Sdp.VideoPayloadTypeInt, rs.Sdp.VideoPayloadTypeStr)
		}
		if err != nil {
			rs.log.Println(err)
			return p, err
		}
	}

	l = len(p.Data)
	if l > 10 {
		l = 10
	}
	rs.log.Printf("pType:%s, pTs:%d, pDataLen:%d, pData:%x", p.Type, p.Timestamp, len(p.Data), p.Data[:l])
	return p, nil
}

func RtspRtps2AudioPacket(rs *RtspStream, rps []*RtpPacket) ([]*AvPacket, error) {
	var err error
	var rp *RtpPacket
	var ps []*AvPacket
	//p := &AvPacket{}
	defer DeleteMapValue(rs, rps, false)

	l := len(rps)
	for i := 0; i < l; i++ {
		rp = rps[i]
		rs.log.Printf("P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, len=%d", rp.Padding, rp.Extension, rp.CsrcCount, rp.Marker, rp.PayloadType, rp.PtStr, rp.SeqNumber, rp.Timestamp, rp.Ssrc, rp.Len)

		//1000毫秒分 8000份, 每份时长1000/ 8000=0.1250毫秒(1/ 8=0.1250)
		//1000毫秒分11025份, 每份时长1000/11025=0.0907毫秒(1/11=0.0909)
		//aac数据 固定1024个采样点为一帧, fps=11025/1024=10.7666
		//两帧时间差=1000/10.7666=92.88毫秒, 两针份数差=92.88/0.0907=1024
		//两帧份数差=1024, 两帧时间差=1024*0.0907=92.88毫秒
		//p.Timestamp = rp.Timestamp / 11

		switch rs.Sdp.AudioPayloadTypeStr {
		case "mpeg4-generic", "MPEG4-GENERIC", "AAC":
			//p.Type = "AudioAacFrame"
			ps, err = Rtp2AacPacket(rs, rp)
		//case "PCMA":
		//p.Type = "AudioG711aFrame"
		//err = Rtp2G711aPacket(rs, p, rp)
		//case "PCMU":
		//p.Type = "AudioG711uFrame"
		//err = Rtp2G711aPacket(rs, p, rp)
		default:
			err = fmt.Errorf("undefined AudioPayloadType %d(%s)", rs.Sdp.AudioPayloadTypeInt, rs.Sdp.AudioPayloadTypeStr)
		}
		if err != nil {
			rs.log.Println(err)
			return ps, err
		}
	}

	l = len(ps)
	for i := 0; i < l; i++ {
		j := len(ps[i].Data)
		if j > 10 {
			j = 10
		}
		rs.log.Printf("pType:%s, pTs:%d, pDataLen:%d, pData:%x", ps[i].Type, ps[i].Timestamp, len(ps[i].Data), ps[i].Data[:j])
	}
	return ps, nil
}

//多个rtp包合成一个音视或视频帧, 通过chan发送给rtmp处理函数
func RtspRtps2AvPacket(rs *RtspStream, rps []*RtpPacket, isVideo bool) error {
	var err error
	var s string
	var p *AvPacket
	var ps []*AvPacket

	//rtp包合成音视频帧
	if isVideo == true {
		//rs.log.Println("------ new video frame ------")
		s = "video"
		p, err = RtspRtps2VideoPacket(rs, rps)
	} else {
		//rs.log.Println("------ new audio frame ------")
		s = "audio"
		ps, err = RtspRtps2AudioPacket(rs, rps)
	}
	if err != nil {
		rs.log.Println(err)
		return err
	}

	//RtpGopCacheUpdate(rs, rps, p.Type)

	//通过chan发送给rtmp处理函数
	l := len(rs.AvPkt2RtmpChan)
	if l < conf.Rtsp.AvPkt2RtmpChanNum {
		if s == "video" {
			rs.AvPkt2RtmpChan <- p
			return nil
		}
		for i := 0; i < len(ps); i++ {
			rs.AvPkt2RtmpChan <- ps[i]
		}
	} else {
		rs.log.Printf("%s AvPkt2RtmpChanNum=%d(%d) drop %s data", rs.StreamId, l, conf.Rtsp.AvPkt2RtmpChanNum, s)
	}
	return nil
}

//尝试把多个rtp包组成一帧, RtpPackets -> AvPacket(一帧数据)
//通常 marker=1表示当前帧结束 时间戳变化表示新帧的开始
//实际 marker=1发送方可能不用 时间戳变化表示新帧的开始
//特殊 marker=1表示当前帧结束 时间戳变化不表示新帧的开始(每个rtp包时间戳都不同)
func RtspRtps2AvPacketCheck(rs *RtspStream, isVideo bool) {
	rpq := rs.VideoRtpPkgs
	if isVideo == false {
		rpq = rs.AudioRtpPkgs
	}

	v, ok := rpq.PkgMap.Load(rpq.PkgMapMaxSeq)
	if ok == false {
		//每次都能找到, 应该不会走到这里
		rs.log.Printf("MaxSeq=%d not exist", rpq.PkgMapMaxSeq)
		return
	}
	p, _ := v.(*RtpPacket)

	//判断是否够一帧视频或一帧音频数据
	if p.Len == 0 || rpq.NeedTs == p.Timestamp {
		//rs.log.Printf("NeedTs=%d, pTs=%d", rpq.NeedTs, p.Timestamp)
		return
	}
	rpq.NeedTs = p.Timestamp

	//走到这里 至少有一个新的AvPakcet(帧)可以生成
	var rps []*RtpPacket //组成一帧数据的多个rtp包
	//RtpSeq回绕: 65533 65534 65535 0 1 2
	l := int(rpq.PkgMapMaxSeq - rpq.PkgMapMinSeq)
	if rpq.PkgMapMaxSeq < rpq.PkgMapMinSeq {
		l = int(65535 - rpq.PkgMapMinSeq + rpq.PkgMapMaxSeq + 1)
	}
	for i := 0; i < l; i++ {
		v, ok = rpq.PkgMap.Load(rpq.PkgMapMinSeq)
		if ok == false {
			rs.log.Printf("RtpSeqNum=%d not exist", rpq.PkgMapMinSeq)
			return
		}
		p, _ := v.(*RtpPacket)
		//rs.log.Printf("FindRps, RtpSeq=%d, Ts=%d, M=%d", rpq.PkgMapMinSeq, p.Timestamp, p.Marker)
		rps = append(rps, p)
		rpq.PkgMapMinSeq++
	}

	RtspRtps2AvPacket(rs, rps, isVideo)
}

func RtspRtpSort(rs *RtspStream, rp *RtpPacket, isVideo bool) error {
	rpq := rs.VideoRtpPkgs
	rpt := "VideoRtpPkgMap"
	if isVideo == false {
		rpq = rs.AudioRtpPkgs
		rpt = "AudioRtpPkgMap"
	}
	rs.log.Printf("==> %s, RtpNeedSeq=%d, RtpSeq=%d ts=%d", rpt, rpq.NeedSeq, rp.SeqNumber, rp.Timestamp)

	var err error
	if rp.SeqNumber < rpq.NeedSeq {
		//需要10, 来的9, 9直接扔掉, 因为已经等了5次
		err = fmt.Errorf("%s, RtpNeedSeq=%d, RtpSeq=%d drop it", rpt, rpq.NeedSeq, rp.SeqNumber)
		return err
	} else if rp.SeqNumber == rpq.NeedSeq {
		//需要10, 来的10, 10放入map缓存中
		rpq.PkgMap.Store(rp.SeqNumber, rp)
		rpq.NeedSeq += 1
	} else {
		//需要10, 来的12, 12放入map缓存中
		rpq.PkgMap.Store(rp.SeqNumber, rp)

		//判断10等了几次, 超过5次就放弃
		if rpq.NeedSeqWaitNum < 5 {
			rpq.NeedSeqWaitNum += 1
			err = fmt.Errorf("%s, RtpNeedSeq=%d, RtpSeq=%d, NeedSeqWaitNum=%d", rpt, rpq.NeedSeq, rp.SeqNumber, rpq.NeedSeqWaitNum)
			return err
		}
		rs.log.Printf("%s, RtpNeedSeq=%d lost, RtpSeq=%d", rpt, rpq.NeedSeq, rp.SeqNumber)
		rpq.NeedSeqWaitNum = 0

		//RtpUdp的seq可能出现连续异常增长, 比如: 3710后是3810
		//所以这里要 查找map缓存中 3710之后首先出现的seq
		l := int(rp.SeqNumber - rpq.NeedSeq)
		for i := 0; i < l; i++ {
			_, ok := rpq.PkgMap.Load(rpq.NeedSeq)
			if ok == true {
				rs.log.Printf("RtpNeedSeq=%d exist", rpq.NeedSeq)
				if rpt == "AudioRtpPkgMap" {
					rpq.PkgMapMinSeq = rpq.NeedSeq
				}
				break
			}
			rs.log.Printf("RtpNeedSeq=%d not exist", rpq.NeedSeq)

			//放入map缓存中一个空rtp数据包, 便于后续处理
			//p := &RtpPacket{}
			//p.SeqNumber = rpq.NeedSeq
			//rpq.PkgMap.Store(p.SeqNumber, p)
			rpq.NeedSeq += 1
		}
	}

	//RtpSeq回绕: 65533 65534 65535 0 1 2
	//需要10, 先来13, 后来10, 需判断11, 12是否已在map缓存中
	for i := 0; i < 10; i++ {
		_, ok := rpq.PkgMap.Load(rpq.NeedSeq)
		if ok == false {
			//rs.log.Printf("RtpNeedSeq=%d not exist", rpq.NeedSeq)
			break
		}
		rs.log.Printf("RtpNeedSeq=%d exist", rpq.NeedSeq)
		rpq.NeedSeq += 1
	}
	return nil
}

func RtspRtpCache(rs *RtspStream, rp *RtpPacket, isVideo bool) error {
	rpq := rs.VideoRtpPkgs
	rpt := "VideoRtpPkgMap"
	if isVideo == false {
		rpq = rs.AudioRtpPkgs
		rpt = "AudioRtpPkgMap"
	}

	if rpq.Ssrc == 0 {
		rpq.Ssrc = rp.Ssrc
		rpq.NeedSeq = rp.SeqNumber
		rpq.PkgMapMinSeq = rp.SeqNumber
		rpq.NeedTs = rp.Timestamp
		rs.log.Printf("%s ssrc is %d", rpt, rpq.Ssrc)
	}
	//RtpSeq回绕: 65533 65534 65535 0 1 2
	//需要10, 先来13, 后来10
	if rpq.PkgMapMaxSeq == 65535 || rpq.PkgMapMaxSeq < rp.SeqNumber {
		rpq.PkgMapMaxSeq = rp.SeqNumber
	}

	var err error
	if rpq.Ssrc != rp.Ssrc {
		err = fmt.Errorf("ssrc(%d) != first ssrc(%d)", rp.Ssrc, rpq.Ssrc)
		rs.log.Println(err)
		return err
	}

	//视频和音频的pkt分开存吗? 是的 因为RtpSeqNum可能不同
	//ffmpeg推rtsp流时, 音视频的ssrc不同, 音视频的seq不同
	err = RtspRtpSort(rs, rp, isVideo)
	if err != nil {
		rs.log.Println(err)
		return err
	}

	//rtp包合成音视频帧 并 通过chan发送给rtmp处理函数
	RtspRtps2AvPacketCheck(rs, isVideo)
	return nil
}

func RtspRtpCacheSort(rs *RtspStream) {
	var p *RtpPacket
	var ok bool
	for {
		p, ok = <-rs.Rtp2RtmpChan
		if ok == false {
			rs.log.Printf("%s RtspRtpCacheSort() stop", rs.StreamId)
			return
		}
		//rs.log.Printf("P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d", p.Padding, p.Extension, p.CsrcCount, p.Marker, p.PayloadType, p.PtStr, p.SeqNumber, p.Timestamp, p.Ssrc)

		switch int(p.PayloadType) {
		case rs.Sdp.VideoPayloadTypeInt:
			RtspRtpCache(rs, p, true)
		case rs.Sdp.AudioPayloadTypeInt:
			RtspRtpCache(rs, p, false)
		default:
		}
	}
}

func RtspRtpHandler(rs *RtspStream, d []byte, RtmpSend bool) error {
	var err error
	if len(d) < 12 {
		err = fmt.Errorf("dataLen=%d < 12(rtpHeaderLen)", len(d))
		return err
	}

	pt := int(d[1] & 0x7F)
	if pt != rs.Sdp.VideoPayloadTypeInt && pt != rs.Sdp.AudioPayloadTypeInt {
		err = fmt.Errorf("vPt=%d(%s), aPt=%d(%s), rtpPt=%d undefined", rs.Sdp.VideoPayloadTypeInt, rs.Sdp.VideoPayloadTypeStr, rs.Sdp.AudioPayloadTypeStr, rs.Sdp.AudioPayloadTypeInt, pt)
		return err
	}

	p := RtpParse(d)
	p.PtStr = RtpPayload2Str("RTSP", int(p.PayloadType))
	//rs.log.Printf("V=%d, P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, Len=%d", p.Version, p.Padding, p.Extension, p.CsrcCount, p.Marker, p.PayloadType, p.PtStr, p.SeqNumber, p.Timestamp, p.Ssrc, p.Len)

	l := len(rs.Rtp2RtmpChan)
	if RtmpSend == true {
		//rs.log.Printf("l=%d, Rtp2RtmpChanNum=%d", l, conf.Rtsp.Rtp2RtmpChanNum)
		if l < conf.Rtsp.Rtp2RtmpChanNum {
			rs.Rtp2RtmpChan <- p
		} else {
			rs.log.Printf("%s Rtp2RtmpChanNum=%d(%d) drop seq=%d(%s) data", rs.StreamId, l, conf.Rtsp.Rtp2RtmpChanNum, p.SeqNumber, p.PtStr)
		}
	}

	l = len(rs.Rtp2RtspChan)
	if l < conf.Rtsp.Rtp2RtspChanNum {
		rs.Rtp2RtspChan <- p
	} else {
		rs.log.Printf("%s Rtp2RtspChanNum=%d(%d) drop seq=%d(%s) data", rs.StreamId, l, conf.Rtsp.Rtp2RtspChanNum, p.SeqNumber, p.PtStr)
	}
	return nil
}
