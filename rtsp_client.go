package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"time"
	"utils"
)

/*************************************************/
/* rtsp client
/*************************************************/
func RtspClient(ip, port string) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%s", ip, port)
	//log.Printf("rtsp conn raddr=%s", addr)

	c, err := net.Dial("tcp", addr)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//log.Printf("rtsp conn laddr=%s, raddr=%s", c.LocalAddr().String(), c.RemoteAddr().String())
	return c, nil
}

/*************************************************/
/* 我们推流给别人, 我们是rtsp客户端 是发布者
/*************************************************/
func RtspPusher() {

}

/*************************************************/
/* 我们拉别人的流, 我们是rtsp客户端 是接收者
/*************************************************/
func PlayInterleavedData(rs *RtspStream, d []byte, l int, n *int) (*Interleaved, error) {
	var err error
	i := &Interleaved{}

	i.Sign = d[*n]
	*n += 1
	i.CID = d[*n]
	*n += 1
	i.DataLen = ByteToUint16(d[*n:*n+2], BE)
	*n += 2

	if i.Sign != 0x24 {
		err = fmt.Errorf("RtpTcp Interleaved Mode Sign(%x) != 0x24", i.Sign)
		rs.log.Println(err)
		return nil, err
	}

	i.Data = make([]byte, i.DataLen)
	div := l - *n
	if i.DataLen <= uint16(div) {
		copy(i.Data, d[*n:*n+int(i.DataLen)])
		*n += int(i.DataLen)
		return i, nil
	}

	//数据不够, 需要从net.Conn里读取
	ll := i.DataLen - uint16(div)
	dd := make([]byte, ll)
	_, err = io.ReadFull(rs.Conn, dd)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	copy(i.Data[:div], d[*n:*n+div])
	*n += div
	copy(i.Data[div:], dd)
	*n += int(ll)

	/*
		//仅用于测试 下个字节是不是0x24
		dd = make([]byte, 10)
		_, err = io.ReadFull(rs.Conn, dd)
		if err != nil {
			rs.log.Println(err)
			return nil, err
		}
		rs.log.Printf("xxxData:%x", dd)
	*/
	return i, nil
}

func PlayRecvDataHandle(rs *RtspStream, d []byte) (int, error) {
	var il *Interleaved
	var err error
	var i, n int
	l := len(d)

	for {
		rs.log.Printf("======== rtp_tcp_data %d ========", i)

		il, err = PlayInterleavedData(rs, d, l, &n)
		if err != nil {
			rs.log.Println(err)
			return i, err
		}
		rs.log.Printf("Sign=%x, CID=%d, DataLen=%d(%d)", il.Sign, il.CID, il.DataLen, len(il.Data))

		switch int(il.CID) {
		case rs.VideoRtpChanId, rs.AudioRtpChanId:
			_ = RtspRtpHandler(rs, il.Data, true)
		case rs.VideoRtcpChanId, rs.AudioRtcpChanId:
			_ = RtspRtcpHandler(rs, il.Data)
		default:
			rs.log.Printf("undefined ChannelId=%d", il.CID)
		}

		if n >= l {
			break
		}
		i++
	}
	return i, nil
}

//1 建立rtcp协议的tcp连接, 失败有限重连
//2 handshake, 确定rtp数据走tcp还是udp
//3 接收RtpPakcet, 转化为AvPacket
//4 通过网络rtmp推流出去, 失败无限重推
func RtspPuller(rs *RtspStream) {
	fn := fmt.Sprintf("%s/%s/publish_rtsp_%s.log", conf.Log.StreamLogPath, rs.StreamId, utils.GetYMD())
	StreamLogRename(rs.LogFn, fn)
	rs.LogFn = fn

	rs.log.Printf("PullUrl:%s", rs.Rqst.PullUrl)
	rs.log.Printf("PushUrl:%s", rs.Rqst.PushUrl)

	var err error
	rs.Conn, err = RtspClient(rs.Rqst.PullIp, rs.Rqst.PullPort)
	if err != nil {
		rs.log.Println(err)
		return
	}
	rs.log.Printf("rtsp conn laddr=%s, raddr=%s", rs.Conn.LocalAddr().String(), rs.Conn.RemoteAddr().String())

	//握手play阶段, 对方可能直接发送数据过来,
	//如果接收buff比较大, 可能收到多个rtp/rtcp包
	//d中最后一个rtp包 大概率不完整, 需补全
	d, err := RtspHandshakeClient(rs)
	if err != nil {
		rs.log.Println(err)
		rs.Conn.Close() //回收资源
		return
	}

	//Rtsp媒体数据走内存发送给自己RtmpServer
	//go RtspMem2RtmpServer(rs)
	//Rtsp媒体数据走网络发送给别的RtmpServer
	go RtspNet2RtmpServer(rs)
	//go RtspNet2RtspServer(rs)
	go RtspMem2RtspPlayers(rs)
	go RtspRtpCacheSort(rs)
	time.Sleep(200 * time.Millisecond)

	var n, i int
	if d != nil {
		n, err = PlayRecvDataHandle(rs, d)
		if err != nil {
			rs.log.Println(err)
			rs.Conn.Close() //回收资源
			return
		}
		rs.log.Printf("PlayRecvData have %d rtprtcp packet", n+1)
		i = n + 1
	}

	var r = bufio.NewReader(rs.Conn)
	var il *Interleaved
	for {
		//rs.log.Printf("======== rtp_tcp_data %d ========", i)
		i++

		il, err = ReadInterleavedData(r)
		if err != nil {
			rs.log.Println(err)
			break
		}

		switch int(il.CID) {
		case rs.AudioRtpChanId, rs.VideoRtpChanId:
			_ = RtspRtpHandler(rs, il.Data, true)
		case rs.AudioRtcpChanId, rs.VideoRtcpChanId:
			_ = RtspRtcpHandler(rs, il.Data)
		default:
			rs.log.Printf("undefined ChannelId=%d", il.CID)
		}
	}
	rs.Conn.Close()
	rs.log.Printf("RtspPuller() stop")
	return
}
