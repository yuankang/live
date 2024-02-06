package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"livegateway/utils"
	"log"
	"net"
	"syscall"

	"github.com/libp2p/go-reuseport"
	"golang.org/x/sys/unix"
)

/*************************************************/
/* RtspPlayer
/*************************************************/
func RtspNet2RtspPlayers(rs *RtspStream, rp *RtpPacket) {
	var player *RtspStream
	var p *RtpPacket
	var d []byte
	var err error
	var n int

	//m := utils.SyncMapLen(&rs.Players)
	//rs.log.Printf("key=%s, sid=%s PlayerNum=%d", rs.Key, rs.StreamId, m)
	rs.Players.Range(func(k, v interface{}) bool {
		player, _ = v.(*RtspStream)

		if player.NewPlayer == true {
			rs.log.Printf("Send RtpCache to NewPlayer %s", player.Key)
			player.NewPlayer = false

			/*
				//发送 缓存SpsPpsRtpPkg + 缓存AvRtpPkg(含当前rtp包)
				p, _ = GetRtpSpsPpsPkg(rs)
				//依据RtpAvPktCache中第1个rtp包的信息, 修改RtpSpsPpsPkg的SeqNumber和Timestamp
				//RtpSpsPpsPkg的SeqNumber=RtpAvPktCache1.SeqNumber-1, 注意回绕
				//RtpSpsPpsPkg的Timestamp=RtpAvPktCache1.Timestamp
				UpdateRtpSpsPpsPkg(rs, p)
				player.Conn.Write(p.Data)
			*/

			for e := rs.RtpGopCache.Front(); e != nil; e = e.Next() {
				p = (e.Value).(*RtpPacket)
				d, _ = AddInterleavedMode(p)
				n, err = player.Conn.Write(d)
				player.log.Printf("Send RtpSeq=%d(%s) DL=%d SL=%d to Player %s", p.SeqNumber, p.PtStr, len(p.Data), n, player.Key)
				if err != nil {
					player.log.Printf("delete %s player", player.Key)
					rs.log.Printf("delete %s player", player.Key)
					rs.Players.Delete(player.Key)
					break
				}
			}
		} else {
			d, _ = AddInterleavedMode(rp)
			n, err = player.Conn.Write(d)
			player.log.Printf("Send V=%d, P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, Len=%d", rp.Version, rp.Padding, rp.Extension, rp.CsrcCount, rp.Marker, rp.PayloadType, rp.PtStr, rp.SeqNumber, rp.Timestamp, rp.Ssrc, rp.Len)
			if err != nil {
				player.log.Printf("delete %s player", player.Key)
				rs.log.Printf("delete %s player", player.Key)
				rs.Players.Delete(player.Key)
			}
		}
		return true
	})
}

func RtspMem2RtspPlayers(s *RtspStream) {
	//接收发送数据 比 rtsp播放加入puber要快
	//FIXME 需要做同步处理 不能简单sleep
	//time.Sleep(1 * time.Second)

	var rp *RtpPacket
	var ok bool
	//var SendGop bool
	for {
		rp, ok = <-s.Rtp2RtspChan
		if ok == false {
			s.log.Printf("%s RtspMem2RtspPlayers() stop", s.StreamId)
			return
		}
		//s.RtpGopCache.PushBack(rp)

		l := len(rp.Data)
		if l > 10 {
			l = 10
		}
		//s.log.Printf("pType:%d(%s), pTs:%d, pDataLen:%d pData:%x", rp.PayloadType, rp.PtStr, rp.Timestamp, len(rp.Data), rp.Data[:l])

		/*
			if SendGop == false {
				RtspSendRtmpGop(rs, s)
				SendGop = true
			} else {
				RtspNet2RtspPlayers(s, rp)
			}
		*/
		RtspNet2RtspPlayers(s, rp)
	}
}

func RtspPlayer(rs *RtspStream) {
	rs.NewPlayer = false
	//log.Printf("PubKey:%s, Sid=%s", rs.Puber.Key, rs.Puber.StreamId)
	n := utils.SyncMapLen(&rs.Puber.Players)
	rs.Puber.Players.Store(rs.Key, rs)
	m := utils.SyncMapLen(&rs.Puber.Players)
	log.Printf("Puber %s BeforePlayerNum=%d, AfterPlayerNum=%d", rs.StreamId, n, m)
}

/*************************************************/
/* RtspPuber
/*************************************************/
//rtsp_rtp_tcp中rtsp协议和音视频数据使用同一个端口, 所以增加interleaved结构 用于区分协议和数据, 详见 RFC2326
type Interleaved struct {
	Sign    uint8  //1Byte, 固定值0x24
	CID     uint8  //1Byte, 一般情况 0x00:video_rtp, 0x01:video_rtcp, 0x02:audio_rtp, 0x03:audio_rtcp, 具体值由sdp信息中的参数来确定
	DataLen uint16 //2Byte
	Data    []byte
}

func ReadInterleavedData(r *bufio.Reader) (*Interleaved, error) {
	var err error
	i := &Interleaved{}

	d, err := ReadUint32(r, 4, BE)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	i.Sign = uint8((d >> 24) & 0xff)
	i.CID = uint8((d >> 16) & 0xff)
	i.DataLen = uint16(d & 0xffff)

	if i.Sign != 0x24 {
		err = fmt.Errorf("RtpTcp Interleaved Mode Sign(%x) != 0x24", i.Sign)
		//log.Println(err)
		return nil, err
	}

	i.Data = make([]byte, i.DataLen)
	_, err = io.ReadFull(r, i.Data)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//log.Printf("Sign=%x, CID=%d, DataLen=%d(%d)", i.Sign, i.CID, i.DataLen, len(i.Data))
	return i, nil
}

func AddInterleavedMode0(rp *RtpPacket) (*Interleaved, error) {
	il := &Interleaved{}
	il.Sign = 0x24
	//0:videoRtp, 1:videoRtcp, 2:audioRtp, 3:audioRtcp
	il.CID = 0x2
	if rp.PayloadType == 0x60 {
		il.CID = 0x0
	}
	il.DataLen = uint16(len(rp.Data))
	il.Data = rp.Data
	return il, nil
}

func AddInterleavedMode(rp *RtpPacket) ([]byte, error) {
	il := Interleaved{}
	il.Sign = 0x24
	//0:videoRtp, 1:videoRtcp, 2:audioRtp, 3:audioRtcp
	il.CID = 0x2
	if rp.PayloadType == 0x60 {
		il.CID = 0x0
	}
	dl := len(rp.Data)
	il.DataLen = uint16(dl)

	l := 4 + dl
	d := make([]byte, l)
	n := 0

	d[n] = il.Sign
	n += 1
	d[n] = il.CID
	n += 1
	Uint16ToByte(il.DataLen, d[n:n+2], BE)
	n += 2
	copy(d[n:], rp.Data)
	n += dl
	return d, nil
}

func RtspPuber(rs *RtspStream) {
	//rtsp_rtp_tcp共用一个端口, 必须是interleaved封包
	//rtsp_rtp_udp都是独立端口, 在RtspHandshake()已监听
	if rs.IsInterleaved == false {
		rs.log.Println("rtsp_rtp_tcp must interleaved mode")
		return
	}

	//go RtspMem2RtmpServer(rs)
	go RtspNet2RtmpServer(rs)
	go RtspMem2RtspPlayers(rs)
	//go RtspNet2RtspServer(rs)
	go RtspRtpCacheSort(rs)

	var i int
	var r = bufio.NewReader(rs.Conn)
	var id *Interleaved
	var err error
	for {
		//rs.log.Printf("======== RtpTcpData %d ========", i)
		i++

		id, err = ReadInterleavedData(r)
		if err != nil {
			rs.log.Println(err)
			//TODO 回收资源
			break
		}

		switch int(id.CID) {
		case rs.AudioRtpChanId, rs.VideoRtpChanId:
			_ = RtspRtpHandler(rs, id.Data, true)
		case rs.AudioRtcpChanId, rs.VideoRtcpChanId:
			_ = RtspRtcpHandler(rs, id.Data)
		default:
			rs.log.Printf("undefined ChannelId=%d", id.CID)
		}
	}
}

func RtspUdpHandle(rs *RtspStream) {
	//go RtspMem2RtmpServer(rs)
	go RtspNet2RtmpServer(rs)
	go RtspMem2RtspPlayers(rs)
	//go RtspNet2RtspServer(rs)
	go RtspRtpCacheSort(rs)

	rs.log.Printf("vRtpPort=%d, vRtcpPort=%d, aRtpPort=%d, aRtcpPort=%d", rs.VideoRtpUdpPort, rs.VideoRtcpUdpPort, rs.AudioRtpUdpPort, rs.AudioRtcpUdpPort)

	var i int
	var p *RtpUdpPkt
	var ok bool
	var StartRtmp = true
	for {
		//rs.log.Printf("======== RtpUdpData %d ========", i)

		p, ok = <-rs.RtpUdpChan
		if ok == false {
			rs.log.Printf("%s RtspUdpHandle() stop", rs.StreamId)
			return
		}

		//rs.log.Printf("ip=%s, port=%d, len=%d", p.Ip, p.Port, len(p.Data))
		switch int(p.Port) {
		case rs.AudioRtpUdpPort, rs.VideoRtpUdpPort:
			_ = RtspRtpHandler(rs, p.Data[:p.Len], StartRtmp)
		case rs.AudioRtcpUdpPort, rs.VideoRtcpUdpPort:
			_ = RtspRtcpHandler(rs, p.Data[:p.Len])
		default:
			rs.log.Printf("undefined RtpUdpPort=%d", p.Port)
		}
		i++
	}
}

func RtspPuberStop(rs *RtspStream) {
	log.Printf("rtsp puber %s stop", rs.Key)
	if rs == nil {
		return
	}
	rs.log.Printf("rtsp puber %s stop", rs.Key)

	rs.Conn.Close()
	RtspPuberMap.Delete(rs.Key)
}

/*************************************************/
/* RtspServer
/*************************************************/
func RtspHandler(c net.Conn) {
	rs := NewRtspStream(c)

	err := RtspHandshakeServer(rs)
	if err != nil {
		rs.log.Println(err)
		return
	}

	fn := fmt.Sprintf("%s/%s/play_rtsp_%s.log", conf.Log.StreamLogPath, rs.StreamId, rs.RAddr)
	if rs.IsPuber == true {
		fn = fmt.Sprintf("%s/%s/publish_rtsp_%s.log", conf.Log.StreamLogPath, rs.StreamId, utils.GetYMD())
	}
	StreamLogRename(rs.LogFn, fn)
	rs.LogFn = fn

	if rs.IsPuber == true {
		//RtspAnnounceHandle() 已检查必不在
		log.Printf("PuberKey=%s, NetTlp=%s", rs.Key, rs.NetProtocol)
		if rs.NetProtocol == "tcp" {
			RtspPuber(rs)
		} else {
			RtspUdpHandle(rs)
		}
		RtspPuberStop(rs)
	} else {
		//RtspDescribeHandle() 已检查定存在
		log.Printf("PlayerKey=%s", rs.Key)
		RtspPlayer(rs)
	}
}

func RtspServerTcp() {
	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Rtsp.Port)
	log.Printf("==> rtsp listen on %s tcp", addr)

	l, err := reuseport.Listen("tcp", addr)
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
		log.Println("------ new rtsp connect ------")
		log.Printf("lAddr:%s, rAddr:%s", c.LocalAddr().String(), c.RemoteAddr().String())

		//有些ipc的音频和视频数据是通过不同端口发送的
		//音频端口建连后会马上断开, 音频数据还是通过视频端口过来
		//RemoteAddr: 10.3.214.236:15060 ipc发送视频地址
		//RemoteAddr: 10.3.214.236:15062 ipc发送音频地址
		go RtspHandler(c)
	}
}

func RtspServerUdp() {
	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Rtsp.Port)
	log.Printf("==> rtsp listen on %s udp", addr)

	pc, err := reuseport.ListenPacket("udp", addr)
	if err != nil {
		log.Fatalln(err)
	}
	l := pc.(*net.UDPConn)

	var n int
	var raddr *net.UDPAddr
	var rs *RtspStream
	for {
		p := &RtpUdpPkt{}
		p.Data = make([]byte, 1600)

		n, raddr, err = l.ReadFromUDP(p.Data)
		if err != nil {
			log.Println(err)
			continue
		}
		p.Ip = raddr.IP.String()
		p.Port = raddr.Port
		p.Len = n

		//log.Println("------ new rtsp UdpRtpPkt ------")
		//log.Printf("rAddr:%s:%d, len=%d, data=%x", p.Ip, p.Port, n, p.Data[:10])

		if rs == nil {
			v, ok := RtspRtpPortMap.Load(p.Port)
			if ok == false {
				log.Printf("rtsp rtp port %d is not exist", p.Port)
				continue
			}
			rs = v.(*RtspStream)
		}

		l := len(rs.RtpUdpChan)
		if l < conf.Rtsp.Rtp2RtspChanNum {
			rs.RtpUdpChan <- p
		} else {
			log.Println("%s, l=%d, cn=%d", rs.Key, l, conf.Rtsp.Rtp2RtspChanNum)
		}
	}
}

func RtspServerTcp1(lc net.ListenConfig) {
	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Rtsp.Port)
	log.Printf("==> rtsp listen on %s tcp", addr)

	ctx := context.Background()
	l, err := lc.Listen(ctx, "tcp", addr)
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
		log.Println("------ new rtsp connect ------")
		log.Printf("lAddr:%s, rAddr:%s", c.LocalAddr().String(), c.RemoteAddr().String())

		//有些ipc的音频和视频数据是通过不同端口发送的
		//音频端口建连后会马上断开, 音频数据还是通过视频端口过来
		//RemoteAddr: 10.3.214.236:15060 ipc发送视频地址
		//RemoteAddr: 10.3.214.236:15062 ipc发送音频地址
		go RtspHandler(c)
	}
}

func RtspServerUdp1(lc net.ListenConfig) {
	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Rtsp.Port)
	log.Printf("==> rtsp listen on %s udp", addr)

	ctx := context.Background()
	pc, err := lc.ListenPacket(ctx, "udp", addr)
	if err != nil {
		log.Fatalln(err)
	}
	l := pc.(*net.UDPConn)

	var n int
	var raddr *net.UDPAddr
	var rs *RtspStream
	for {
		p := &RtpUdpPkt{}
		p.Data = make([]byte, 1600)

		n, raddr, err = l.ReadFromUDP(p.Data)
		if err != nil {
			log.Println(err)
			continue
		}
		p.Ip = raddr.IP.String()
		p.Port = raddr.Port
		p.Len = n

		//log.Println("------ new rtsp UdpRtpPkt ------")
		//log.Printf("rAddr:%s:%d, len=%d, data=%x", p.Ip, p.Port, n, p.Data[:10])

		if rs == nil {
			v, ok := RtspRtpPortMap.Load(p.Port)
			if ok == false {
				log.Printf("rtsp rtp port %d is not exist", p.Port)
				continue
			}
			rs = v.(*RtspStream)
		}

		l := len(rs.RtpUdpChan)
		//rs.log.Printf("%s, l=%d, cn=%d", rs.Key, l, conf.Rtsp.Rtp2RtspChanNum)
		if l < conf.Rtsp.Rtp2RtspChanNum {
			rs.RtpUdpChan <- p
		} else {
			log.Printf("%s, l=%d, cn=%d", rs.Key, l, conf.Rtsp.Rtp2RtspChanNum)
		}
	}
}

func FdSet(fd uintptr) {
	err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	if err != nil {
		log.Println(err)
	}
	err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	if err != nil {
		log.Println(err)
	}
	err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF, 100*1024*1024)
	if err != nil {
		log.Println(err)
	}
}

func FdControl(network, address string, c syscall.RawConn) error {
	return c.Control(FdSet)
}

func RtspServer() {
	lc := net.ListenConfig{
		Control: FdControl,
	}

	go RtspServerTcp1(lc)
	go RtspServerUdp1(lc)

	//rtsp交互只用tcp, rtp可以使用tcp或udp
	//go RtspServerTcp() //rtsp支持RtpTcp单端口
	//go RtspServerUdp() //rtsp支持RtpUdp单端口

	log.Printf("rtsp RtpRtcp tcp&udp multiple port use [%d-%d)", conf.Rtsp.RtpPortMin, conf.Rtsp.RtpPortMax)
	for i := conf.Rtsp.RtpPortMin; i < conf.Rtsp.RtpPortMax; i++ {
		//log.Printf("rtsp rtp listen on %d tcp&udp", i)
	}
	select {}
}
