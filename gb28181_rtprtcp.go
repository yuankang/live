package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"utils"
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

//TODO: I/P帧 丢一部分rtp数据 该如何处理
//TODO: 有些ipc发送的首包 不一定是头信息+IDR, 可能是P帧或音频帧, 这些数据要扔掉
//1 合并rtp数据并转为RtmpMsg; 2 分发RtmpMsg给播放者;
func RtpData2RtmpMsg(s *Stream) (*Chunk, error) {
	var rp *RtpPacket
	var buf bytes.Buffer
	rtpNum := len(s.FrameRtp.RtpPkgs)
	n, l := 0, 0

	//把多个rtp包中的载荷 拼接成EsData
	for i := 0; i < rtpNum; i++ {
		rp = &(s.FrameRtp.RtpPkgs[i])
		n = len(rp.Data[rp.EsIdx:])
		buf.Write(rp.Data[rp.EsIdx:])
		l += n
	}
	d := buf.Bytes() //EsData

	//实际接收到的数据 可能比EsData数据 多, 也就是一个Es里有多个nalu, 比如
	//000001e0 0ca2, 视频, 000001c0 xxxx, 音频
	//000001bd 006a, 海康私有标识, 丢弃后看不到视频里移动侦测的红框
	pos := s.FrameRtp.DataLen //协议总EsData的实际长度
	if s.FrameRtp.RecvLen != s.FrameRtp.DataLen {
		s.log.Printf("DataHead:%x", d[:10])
		s.log.Printf("DataXxxx:%x", d[pos:pos+4]) //不属于本帧的特殊数据
	}
	//私有数据的长度
	PrivDataLen := s.FrameRtp.RecvLen - pos

	s.log.Printf("RtpNum:%d, FrameLen:%d, PrivLen:%d, RecvLen=%d, CalcFrameLen=%d", rtpNum, s.FrameRtp.DataLen, PrivDataLen, s.FrameRtp.RecvLen, l)

	d = CreateVideoPacket(d[:pos], s.FrameRtp.Type, "h264")
	c := CreateMessage(MsgTypeIdVideo, uint32(len(d)), d)
	c.Csid = 3
	c.Timestamp = rp.Timestamp

	//帧数据写入到rtmp的gop中, 用于rtmp快速启播
	s.GopCache.MediaData.PushBack(&c)
	if s.FrameRtp.Type == "VideoKeyFrame" {
		s.GopCache.GopCacheNum++
		s.FrameNum = 0
	}
	s.FrameNum++
	if uint32(s.FrameNum) == conf.Rtmp.GopFrameNum {
		GopCacheUpdate(s)
	}
	//GopCacheShow(s)

	s.log.Printf("<== send DataType:%s, DataLen:%d", s.FrameRtp.Type, len(d))

	//轮询发送数据给所有播放者 通过每个播放者的chan
	//播放者到播放器的网络差 也不会引起这个循环阻塞
	//详细说明见 RtmpTransmit()
	var p *Stream //player
	s.Players.Range(func(k, v interface{}) bool {
		p, _ = v.(*Stream)
		s.log.Printf("<== send %s to %s", s.FrameRtp.Type, p.Key)
		if p.PlayClose == true {
			s.log.Printf("<== player %s is stop", p.Key)
			//RtmpStop(p)
			s.Players.Delete(p.Key)
			return true
		}

		if p.NewPlayer == true {
			//发送metadata, videoheader, audioheader, gop
			GopCacheSendRtmp(s, p, &(s.GopCache))
			p.NewPlayer = false
			return true
		}

		if len(p.FrameChan) < 100 {
			p.FrameChan <- c
		} else {
			s.log.Printf("<== player %s ChanLen=100")
		}
		return true
	})
	return &c, nil
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
	addr := fmt.Sprintf(":%s", conf.RtpRtcp.FixedRtpPort)
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
func RtpHandler(s *Stream) {
	var rps []RtpPacket
	var rp RtpPacket
	var ok bool
	var err error

	for {
		rp, ok = <-s.RtpChan
		if ok == false {
			s.log.Println(err)
			break
		}
		s.log.Printf("==> SeqNum=%d, RtpPktListHeadSeq=%d, RtpPktListTailSeq", rp.SeqNum, s.RtpPktListHeadSeq, s.RtpPktListTailSeq)

		//0, 1, ..., 65534, 65535, 0, 1, ..., 65535, 0, ...
		//SeqNum=0, HeadSeq=0, TailSeq=0
		//SeqNum=1, HeadSeq=0, TailSeq=1
		//SeqNum=4, HeadSeq=0, TailSeq=2
		//SeqNum=2, HeadSeq=0, TailSeq=5
		//SeqNum=3, HeadSeq=0, TailSeq=5
		//SeqNum=5, HeadSeq=0, TailSeq=5
		//SeqNum=6, HeadSeq=0, TailSeq=6
		//......
		//SeqNum=65534, HeadSeq=0, TailSeq=65534
		//SeqNum=65535, HeadSeq=0, TailSeq=65535
		//SeqNum=0, HeadSeq=0, TailSeq=0
		if rp.SeqNum >= s.RtpPktListTailSeq {
			s.RtpPktList.PushBack(rp)
			s.RtpPktListTailSeq = rp.SeqNum + 1
		} else {
			if rp.SeqNum < s.RtpPktListHeadSeq {
				continue //直接扔掉
			} else {
				//cache这个RtpPacket, 然后记录等待次数
				s.RtpPkgCache.Store(rp.SeqNum, rp)

				if s.RtpSeqWait < 5 {
					s.RtpSeqWait += 1
					s.log.Printf("RtpSeqThis=%d, RtpSeqNeed=%d, RtpSeqWait=%d", rp.SeqNum, s.RtpSeqNeed, s.RtpSeqWait)
					continue
				}
				s.RtpSeqWait = 0

				//已经等了5个包, 还是没有来 需要的rtp包
				//此时不在等了, 并且需要处理s.RtpPkgCache中的所有rtp包
				s.log.Printf("RtpSeqNeed=%d lost", s.RtpSeqNeed)
				s.RtpSeqNeed += 1

				RtpPkgCacheLen := utils.SyncMapLen(&s.RtpPkgCache)
				for i := 0; i < RtpPkgCacheLen; i++ {
					v, ok := s.RtpPkgCache.Load(s.RtpSeqNeed)
					if ok == false { //rtp包序号不连续, 停止查找
						s.log.Printf("RtpPkgCache haven't RtpSeq=%d", s.RtpSeqNeed)
						break
					}
					s.log.Printf("RtpPkgCache have RtpSeq=%d", s.RtpSeqNeed)

					rp, _ = v.(RtpPacket)
					rps = append(rps, rp)
					s.RtpPkgCache.Delete(s.RtpSeqNeed)
					s.RtpSeqNeed++
				}
			}
		}

		rpsLen := len(rps)
		for i := 0; i < rpsLen; i++ {
			rp = rps[i]
			s.log.Printf("%d, Len=%d, UseNum=%d, SeqNum=%d, ssrc=%.10d, PT=%d(%s), ts=%d", i, rp.Len, rp.UseNum, rp.SeqNum, rp.Ssrc, rp.PayloadType, rp.PtStr, rp.Timestamp)

			switch rp.PayloadType {
			case 0x08: //08 G.711a
				s.log.Printf("PT=%d, %s", rp.PayloadType, rp.PtStr)
			case 0x60: //96 PS
				//s.log.Printf("PT=%d, %s", rp.PayloadType, rp.PtStr)
				err = ParsePs(s, &rp)
			case 0x61: //97 AAC
				s.log.Printf("PT=%d, %s", rp.PayloadType, rp.PtStr)
			case 0x62: //98 H264
				s.log.Printf("PT=%d, %s", rp.PayloadType, rp.PtStr)
			default:
				s.log.Println("RtpPayloadType is Undefined %d", rp.PayloadType)
			}

			if err != nil && err != io.EOF {
				s.log.Println(err)
			}
		}
		rps = nil //清空切片
	}
}

/*
1 接收并缓存rtp包, 如何缓存chan/map/list
2 rtp包组成视频帧, 每个rtp包触发一次检查, 每次尽可能多组成视频帧
3 视频帧以rtmp方式发送, 每次尽可能多发送
*/
func RtpReceiverTcp(c net.Conn) {
	var l uint16
	var d []byte
	var rp *RtpPacket
	var err error
	var s *Stream

	for {
		//因为tcp会作拆包和粘包的处理, 所以RTP(TCP)比RTP(UDP)多2字节长度信息
		l, err = ReadUint16(c, 2, BE)
		if err != nil {
			log.Println(err)
			if s != nil {
				s.log.Println(err)
				StreamMap.Delete(s.Key)
				SsrcMap.Delete(s.RtpSsrcUint32)
			}
			break
		}

		d = make([]byte, int(l))
		_, err = io.ReadFull(c, d)
		if err != nil {
			log.Println(err)
			break
		}

		n := l - 10
		if n < 0 {
			n = 0
		}
		log.Println("RtpPkg tcp, Len=%d, Data=%x", l, d[n:])

		rp = RtpParse(d)
		//首个rtp包, 进不去; 首个rtp会进入SsrcFindStream();
		if s != nil && rp.Ssrc != s.RtpSsrcUint32 {
			s.log.Printf("RtpSsrc=%.10d != MySsrc=%.10d, drop RtpPacket", rp.Ssrc, s.RtpSsrcUint32)
			continue
		}

		if s == nil {
			s, err = SsrcFindStream(rp.Ssrc)
			if err != nil {
				//不是cc下发的任务, 不接收媒体数据
				log.Println(err)
				break
			}

			s.Conn0 = c
			s.RemoteAddr = c.RemoteAddr().String()
			s.RtpChan = make(chan RtpPacket, 100)
			//s.RtpRecChan = make(chan RtpPacket, 100)
			//s.FrameChan = make(chan Frame, 100)
			s.RtpSeqNeed = rp.SeqNum

			//s.RtpPktList
			s.RtpPktListHeadSeq = rp.SeqNum
			s.RtpPktListTailSeq = rp.SeqNum
			//s.RtpPktListMutex   sync.Mutex //rtplist锁, 防止插入和删除并发

			//go Gb281812Mem2RtmpServer(s)
			//go Gb28181Net2RtmpServer(s)
			go RtpHandler(s)
			//go RtpRec(s)
		}
		s.log.Printf("oIp=%s, rtpLen=%d(%x), seqNum=%d, %#v", s.RemoteAddr, rp.Len, rp.Len, rp.SeqNum, rp.RtpHeader)

		if len(s.RtpChan) < 100 {
			s.RtpChan <- *rp
		} else {
			s.log.Printf("RtpChanLen=%d", len(s.RtpChan))
		}

		if len(s.RtpRecChan) < 100 {
			s.RtpRecChan <- *rp
		} else {
			s.log.Printf("RtpRecChanLen=%d", len(s.RtpRecChan))
		}
	}
	//TODO 回收资源
	c.Close()
}

func RtpServerTcp() {
	addr := fmt.Sprintf(":%s", conf.RtpRtcp.FixedRtpPort)
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
		go RtpReceiverTcp(c)
	}
}
