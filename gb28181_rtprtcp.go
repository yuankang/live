package main

import (
	"container/list"
	"fmt"
	"io"
	"log"
	"net"
)

/*************************************************/
/* gb28181中rtp数据封包说明
/*************************************************/
//idr帧(关键帧) 包结构
//rtpHeader+psHeader+psSysHeader+PgmStreamMap+PesHeader+ES(sps+pps+sei+iFrame)
//rtpHeader+ES(iFrame), 剩余的iFrame数据
//P帧 包结构
//rtpHeader+psHeader+PesHeader+ES(pFrame)
//rtpHeader+ES(pFrame), 剩余的pFrame数据
//音频帧 包结构
//rtpHeader+psHeader+PesHeader+ES(aFrame)
//rtpHeader+ES(aFrame), 剩余的aFrame数据
//一般 tcp的rtp放的下一个音频帧, udp的rtp可能放不下
//音视频 包结构 也可能是 (需观察确认)
//rtpHeader+psHeader+PesHeader+ES(vFrame)+PesHeader+ES(aFrame)
//vFrame可能是h264/h265
//aFrame可能是G711a/AAC

//rtp(tcp)包 一般长度为1412, 为uint16, 最大值为65536
//rtp(udp)包 一般长度为1400, 不能大于MTU(一般为1500)
//PesHeader中PesPacketLength为uint16, 最大值为65536, 0表示长度不限 通常为视频

//由于PES头的负载长度类型是short，最大为65536
//所以每65536字节的视频数据后都得加一个PES头, 如下:
//PsHeader+PsSysHeader+PgmStreamMap+PesHeader+Data+PesHeader+Data
//这样PS封装就完成了, 剩下的是分RTP包, 每1400字节数据前加一个RTP头

/*************************************************/
/* RtpData2RtmpMessage
/*************************************************/
//存放相同时间戳 一个视频帧 的多个rtp包
type FrameRtp struct {
	Type    string      //帧类型, 同FrameType
	DataLen int         //帧数据实际长度, 视频可能为0
	RecvLen int         //帧数据实收长度
	RtpPkgs []RtpPacket //多个时间戳相同的rtp包
}

//TODO: 有些ipc可能从P帧或音频开始发送 这些数据最好扔掉
func RtpPktList2PsPkt(s *Stream) (*PsPacket, error) {
	var err error
	var n *list.Element
	var rp *RtpPacket
	pp := &PsPacket{}

	for e := s.RtpPktList.Front(); e != nil; e = n {
		rp = (e.Value).(*RtpPacket)

		if rp.PayloadType != 0x60 {
			s.log.Printf("RtpPt=%d(%s) is not ps", rp.PayloadType, rp.PtStr)
			n = e.Next()
			s.RtpPktList.Remove(e)
			continue
		}

		if s.RtpPktCrtTs != int64(rp.Timestamp) {
			s.log.Printf("RtpPktCrtTs(%d) != RtpTs(%d)", s.RtpPktCrtTs, rp.Timestamp)
			break
		}

		pp.Timestamp = rp.Timestamp
		pp.Data = append(pp.Data, rp.Data[rp.UseNum:]...)

		n = e.Next()
		s.RtpPktList.Remove(e)
	}
	return pp, err
}

/*************************************************/
/* rtp udp
/*************************************************/
//udp的rtp包最大的长度是1400, 视频数据需要分包/合包处理
func RtpReceiverUdp(c *net.UDPConn) {
	buf := make([]byte, 100)
	if _, err := io.ReadFull(c, buf); err != nil {
		log.Println(err)
		return
	}
	log.Printf("RtpRecvData:%x", buf)

	var rh RtpHeader
	rh.Version = (buf[0] >> 6) & 0x3
	rh.Padding = (buf[0] >> 6) & 0x1
	rh.Extension = (buf[0] >> 4) & 0x1
	rh.CsrcCount = buf[0] & 0xf
	rh.Marker = (buf[1] >> 7) & 0x1
	rh.PayloadType = buf[1] & 0x7f
	rh.SeqNum = ByteToUint16(buf[2:4], BE)
	rh.Timestamp = ByteToUint32(buf[4:8], BE)
	rh.Ssrc = ByteToUint32(buf[8:12], BE)
	//rh.SsrcStr = strconv.Itoa(int(rh.Ssrc))
	var i uint8
	for ; i < rh.CsrcCount; i++ {
		log.Println("csrc need to do something")
	}

	switch rh.PayloadType {
	case 0x08: // 0x08 08 G.711a
		rh.PtStr = "G711a"
	case 0x60: // 0x60 96 PS, 时钟频率90kHz
		rh.PtStr = "PS"
	case 0x61: // 0x61 97 AAC
		rh.PtStr = "AAC"
	case 0x62: // 0x62 98 H264
		rh.PtStr = "H264"
	default:
		log.Println("RtpPayloadType is Undefined %d", rh.PayloadType)
	}

	log.Printf("%#v", rh)
	log.Printf("PT:%d(%s), SeqNum:%d, TS:%d, ssrc:%d, csrcNum:%d", rh.PayloadType, rh.PtStr, rh.SeqNum, rh.Timestamp, rh.Ssrc, rh.CsrcCount)
	//PT:96(PS), SeqNum:781, TS:3778690924, ssrc:3297314134, csrcNum:0
	//PT:97(AAC), SeqNum:33, TS:1723650452, ssrc:3180170775, csrcNum:0

	/*
		var nh NaluHeader
		nh.ForbiddenZeroBit = buf[12] >> 7
		nh.NalRefIdc = (buf[12] >> 5) & 0x3
		nh.NaluType = buf[12] & 0x1f
		log.Printf("%#v", nh)
	*/
}

func RtpServerUdp() {
	addr := fmt.Sprintf(":%d", conf.RtpRtcp.FixedRtpPort)
	log.Printf("listen rtp(udp) on %s", addr)

	laddr, _ := net.ResolveUDPAddr("udp", addr)
	l, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln(err)
	}

	RtpReceiverUdp(l)
}

/*************************************************/
/* rtp tcp
/*************************************************/
func GbRtpPktHandler(s *Stream) {
	var err error
	var ok bool
	var rp *RtpPacket
	var pp *PsPacket

	for {
		rp, ok = <-s.RtpPktChan
		if ok == false {
			s.log.Printf("GbRtpPktHandler() stop")
			break
		}
		//s.log.Printf("--> RtpLen=%d(0x%x), SeqNum=%d, Pt=%s(%d), Ts=%d, Mark=%d", rp.Len, rp.Len, rp.SeqNum, rp.PtStr, rp.PayloadType, rp.Timestamp, rp.Marker)
		//s.log.Printf("rtpData:%x", rp.Data)

		if s.RtpPktNeedSeq != rp.SeqNum {
			s.log.Printf("RtpPktNeedSeq(%d) != RtpSeq(%d)", s.RtpPktNeedSeq, rp.SeqNum)
		}
		s.RtpPktList.PushBack(rp) //从尾部插入, 绝大多数是这种
		s.RtpPktNeedSeq = rp.SeqNum + 1

		if s.RtpPktCrtTs == -1 {
			s.RtpPktCrtTs = int64(rp.Timestamp)
		}

		//rp.Marker==1 表示一帧画面的最后一个rtp包到了
		//rp.Timestamp不同 表示一帧画面的第一个rtp包到了
		if rp.Marker == 0 && s.RtpPktCrtTs == int64(rp.Timestamp) {
			continue
		}

		//PrintList(s, &s.RtpPktList)
		pp, err = RtpPktList2PsPkt(s)
		//PrintList(s, &s.RtpPktList)
		if err != nil {
			s.log.Println(err)
			continue
		}

		if rp.Marker == 1 {
			s.RtpPktCrtTs = -1
		} else {
			s.RtpPktCrtTs = int64(rp.Timestamp)
		}
		//s.log.Printf("PsPkt type=%s, dLen=%d, PsTs=%d, RtpPktCrtTs=%d ", pp.Type, len(pp.Data), pp.Timestamp, s.RtpPktCrtTs)

		//通过chan发送给GbNetPushRtmp()
		err = ParsePs(s, pp)
		if err != nil {
			s.log.Println(err)
		}
	}
}

func PrintList(s *Stream, l *list.List) {
	s.log.Println(">>>>>> print list <<<<<<")
	var rp *RtpPacket
	var i int

	for e := l.Front(); e != nil; e = e.Next() {
		rp = (e.Value).(*RtpPacket)
		s.log.Printf("Seq=%d, Mark=%d, Ts=%d", rp.SeqNum, rp.Marker, rp.Timestamp)
		i++
	}
	s.log.Printf("list node num is %d", i)
}

//1 接收rtp包, 使用list暂存rtp包
//2 rtp包组成ps包, 解析ps/pes提取音视频数据
//3 音视频数据按rtmp格式封装并发送
func RtpRecvTcp(c net.Conn) {
	var err error
	var l uint16
	var d []byte
	var rp *RtpPacket
	var s *Stream

	for {
		//tcp会作拆包和粘包的处理, RTP(TCP)有2字节长度信息
		l, err = ReadUint16(c, 2, BE)
		if err != nil {
			log.Println(err)
			if s != nil {
				s.log.Println(err)
				//TODO: 释放资源
				StreamMap.Delete(s.Key)
				SsrcMap.Delete(s.RtpSsrcUint)
			}
			break
		}

		//TODO: 性能优化, 参考sliveconnproxy
		d = make([]byte, int(l))
		_, err = io.ReadFull(c, d)
		if err != nil {
			log.Println(err)
			break
		}

		rp = RtpParse(d)
		if s != nil && rp.Ssrc != s.RtpSsrcUint {
			s.log.Printf("RtpSsrc=%.10d != MySsrc=%.10d, drop this RtpPkt", rp.Ssrc, s.RtpSsrcUint)
			continue
		}

		if s == nil {
			s, err = SsrcFindStream(rp.Ssrc)
			if err != nil {
				log.Println(err)
				break
			}

			s.Conn0 = c
			s.RemoteAddr = c.RemoteAddr().String()
			s.RtpPktChan = make(chan *RtpPacket, conf.RtpRtcp.RtpPktChanNum)
			s.PsPktChan = make(chan *PsPacket, conf.RtpRtcp.PsPktChanNum)
			s.RtpPktNeedSeq = rp.SeqNum
			s.RtpPktCrtTs = int64(rp.Timestamp)

			log.Printf("rAddr=%s, ssrc=%.10d, streamId=%s", s.RemoteAddr, rp.Ssrc, s.Key)
			s.log.Printf("rAddr=%s, ssrc=%.10d, streamId=%s", s.RemoteAddr, rp.Ssrc, s.Key)
			s.log.Printf("%#v", rp.RtpHeader)

			//go GbMemPushRtmp(s)
			go GbNetPushRtmp(s)
			go GbRtpPktHandler(s)
		}
		//s.log.Printf("RtpLen=%d(0x%x), SeqNum=%d, Pt=%s(%d), Ts=%d, Mark=%d", rp.Len, rp.Len, rp.SeqNum, rp.PtStr, rp.PayloadType, rp.Timestamp, rp.Marker)

		if len(s.RtpPktChan) < conf.RtpRtcp.RtpPktChanNum {
			s.RtpPktChan <- rp
		} else {
			s.log.Printf("RtpPktChanLen=%d, MaxLen=%d", len(s.RtpPktChan), conf.RtpRtcp.RtpPktChanNum)
		}
	}
}

func RtpServerTcp() {
	addr := fmt.Sprintf(":%d", conf.RtpRtcp.FixedRtpPort)
	log.Printf("listen rtp(tcp) on %s", addr)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalln(err)
	}

	var c net.Conn
	for {
		c, err = l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("------ new rtp(tcp) connect ------")
		log.Println("RemoteAddr:", c.RemoteAddr().String())

		//有些ipc的音频和视频数据是通过不同端口发送的,
		//音频端口建连后会马上断开, 音频数据还是通过视频端口过来
		//RemoteAddr: 10.3.214.236:15060 ipc发送视频地址
		//RemoteAddr: 10.3.214.236:15062 ipc发送音频地址
		go RtpRecvTcp(c)
	}
}

func RtcpRecvTcp(c net.Conn) {
	var err error
	var l uint16
	var d []byte
	var s *Stream

	for {
		//tcp会作拆包和粘包的处理, RTP(TCP)有2字节长度信息
		l, err = ReadUint16(c, 2, BE)
		if err != nil {
			log.Println(err)
			if s != nil {
				s.log.Println(err)
				//TODO: 释放资源
				StreamMap.Delete(s.Key)
				SsrcMap.Delete(s.RtpSsrcUint)
			}
			break
		}

		//TODO: 性能优化, 参考sliveconnproxy
		d = make([]byte, int(l))
		_, err = io.ReadFull(c, d)
		if err != nil {
			log.Println(err)
			break
		}
		log.Printf("recv %s rtcp, len=%d, data=%x", c.RemoteAddr().String(), len(d), d)
	}
}

func RtcpServerTcp() {
	addr := fmt.Sprintf(":%d", conf.RtpRtcp.FixedRtcpPort)
	log.Printf("listen rtcp(tcp) on %s", addr)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalln(err)
	}

	var c net.Conn
	for {
		c, err = l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("------ new rtcp(tcp) connect ------")
		log.Println("RemoteAddr:", c.RemoteAddr().String())

		//有些ipc的音频和视频数据是通过不同端口发送的,
		//音频端口建连后会马上断开, 音频数据还是通过视频端口过来
		//RemoteAddr: 10.3.214.236:15060 ipc发送视频地址
		//RemoteAddr: 10.3.214.236:15062 ipc发送音频地址
		go RtcpRecvTcp(c)
	}
}
