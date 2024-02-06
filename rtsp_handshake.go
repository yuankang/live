package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

/*************************************************/
/* RtspHandshakeServer
/*************************************************/
type RtspHsRqst struct {
	Method   string
	Uri      string
	Version  string
	Cseq     string
	Body     []byte
	StreamId string
}

func ParseRtspHandshakeRqst(d []byte) *RtspHsRqst {
	rqst := &RtspHsRqst{}
	p := strings.Split(string(d), "\r\n")

	for i := 0; i < len(p); i++ {
		//log.Printf("i=%d, %s", i, p[i])
		//ANNOUNCE rtsp://192.168.16.148:2995/live/test001 RTSP/1.0
		if strings.Contains(p[i], "ANNOUNCE") {
			s := strings.Split(p[i], " ")
			pos := strings.LastIndex(s[1], "/") + 1
			b := []byte(s[1])
			rqst.StreamId = string(b[pos:])
		}
	}
	pp := strings.Split(string(p[0]), " ")

	rqst.Method = pp[0]
	rqst.Uri = pp[1]
	rqst.Version = pp[2]
	rqst.Cseq = GetRtspHsRqstArg(d, "CSeq")
	return rqst
}

func GetRtspHsRqstArg(d []byte, key string) string {
	p := strings.Split(string(d), "\r\n")
	for i := 1; i < len(p); i++ {
		if strings.Contains(string(p[i]), key) {
			pp := strings.Split(string(p[i]), ":")
			return strings.TrimSpace(pp[1])
		}
	}
	return ""
}

//Transport: RTP/AVP/TCP;unicast;interleaved=0-1;mode=record
func ParseRtpRtcpChannelId(s string) (int, int, error) {
	var v string
	items := strings.Split(s, ";")
	for _, item := range items {
		if strings.HasPrefix(item, "interleaved") {
			kv := strings.Split(item, "=")
			if len(kv) != 2 {
				continue
			}
			v = kv[1]
		}
	}
	items = strings.Split(v, "-")
	if len(items) != 2 {
		return 0, 0, nil
	}
	r1, err := strconv.Atoi(items[0])
	if err != nil {
		return 0, 0, err
	}
	r2, err := strconv.Atoi(items[1])
	if err != nil {
		return 0, 0, err
	}
	return r1, r2, nil
}

//Transport: RTP/AVP/UDP;unicast;client_port=28318-28319;mode=record
func ParseRtpRtcpPort(s string) (int, int, error) {
	var v string
	items := strings.Split(s, ";")
	for _, item := range items {
		if strings.HasPrefix(item, "client_port") {
			kv := strings.Split(item, "=")
			if len(kv) != 2 {
				continue
			}
			v = kv[1]
		}
	}
	items = strings.Split(v, "-")
	if len(items) != 2 {
		return 0, 0, nil
	}
	r1, err := strconv.Atoi(items[0])
	if err != nil {
		return 0, 0, err
	}
	r2, err := strconv.Atoi(items[1])
	if err != nil {
		return 0, 0, err
	}
	return r1, r2, nil
}

//解析RtspUrl rtsp://192.168.16.160:2995/live/test001, 拼接出key
func RtspOptionsResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	var err error
	rs.UrlArgs, err = UrlParse(rqst.Uri)
	if err != nil {
		rs.log.Println(err)
		RtspErrorResponse(rs, rqst)
		rs.Conn.Close() //回收rs
		return err
	}
	rs.log.Printf("%#v", rs.UrlArgs)
	//rs.Key = rs.UrlArgs.Key
	app := rs.UrlArgs.Path[0]
	sid := ""
	if len(rs.UrlArgs.Path) > 1 {
		sid = rs.UrlArgs.Path[1]
	}

	rs.StreamId = sid
	//对于发布者 key用于唯一标识 不能存在, 见 RtspAnnounceResponse()
	//对于播放者 key用于找发布者 必须存在, 见 RtspDescribeResponse()
	rs.Key = fmt.Sprintf("%s_%s", app, sid)

	rsps := fmt.Sprintf(RtspOptionsRsps, rqst.Cseq)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err = rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspAnnounceResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	var err error
	rs.log.Printf("PuberKey=%s", rs.Key)
	_, ok := RtspPuberMap.Load(rs.Key)
	if ok == true {
		err = fmt.Errorf("rtsp %s is exist", rs.UrlArgs.Url)
		rs.log.Println(err)
		RtspErrorResponse(rs, rqst)
		rs.Conn.Close() //回收rs
		return err
	}
	RtspPuberMap.Store(rs.Key, rs)

	var d [1024]byte
	n, err := rs.Conn.Read(d[:])
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Printf("read len=%d\n%s", n, d[:n])

	rs.Sdp, _ = ParseSdp(d[:n])
	RawSdp := rs.Sdp.RawSdp
	rs.Sdp.RawSdp = nil
	rs.log.Printf("%#v", rs.Sdp)
	rs.Sdp.RawSdp = RawSdp

	rsps := fmt.Sprintf(RtspAnnounceRsps, rqst.Cseq)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err = rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

//rtsp播放, 发布者不存在 直接断开
func RtspDescribeResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	//要先找到发布者, 否则sdp内容无法确定
	var err error
	//rs.log.Printf("PlayerKey=%s", rs.Key)
	v, ok := RtspPuberMap.Load(rs.Key)
	if ok == false {
		//err = fmt.Errorf("rtsp %s is not exist", rs.UrlArgs.Url)
		err = fmt.Errorf("rtsp %s is not exist", rs.Key)
		rs.log.Println(err)
		//RtspErrorResponse(rs, rqst)
		//rs.Conn.Close() //回收rs
		return err
	}
	rs.log.Printf("rtsp puber %s is exist", rs.Key)
	rs.Puber = v.(*RtspStream)
	rs.Sdp = rs.Puber.Sdp

	sdp := rs.Puber.Sdp.RawSdp
	date := time.Now().Format(time.RFC1123)
	rsps := fmt.Sprintf(RtspDescribeRspsOk, rqst.Cseq, date, len(sdp), sdp)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err = rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

//rtsp播放, 发布者不存在 触发拉rtmp流, rtmp拉流尝试5次 每次间隔1秒
//rtsp播放请求(需返回sdp)->rtmp拉流(获得spspps)->mem2rtsp发布(生成sdp)->rtsp播放查询rtsp发布(使用sdp)
func RtspDescribeResponse0(rs *RtspStream, rqst *RtspHsRqst) error {
	var err error
	var app, sid, url string
	for i := 0; i < 5; i++ {
		err = RtspDescribeResponse(rs, rqst)
		if err == nil {
			return nil
		}
		//rs.log.Println(err)

		//触发rtmp拉流
		app = rs.UrlArgs.Path[0]
		sid = rs.UrlArgs.Path[1]
		url = fmt.Sprintf("rtmp://127.0.0.1:1935/%s/%s", app, sid)
		//rs.log.Printf("try%d to pull %s", i, url)

		//拉rtmp流并mem2rtsp
		cc := make(chan bool)
		go RtmpPuller("127.0.0.1", "1935", app, sid, 1, cc)
		//等待从cc读取数据 或者 cc被关闭, 否则一直阻塞
		tf := <-cc
		if tf == true {
			rs.log.Printf("pull%d %s succ", i, url)
			//等sps和pps
			//time.Sleep(200 * time.Millisecond)
		} else {
			rs.log.Printf("pull%d %s fail", i, url)
		}
		time.Sleep(1 * time.Second)
	}

	RtspErrorResponse(rs, rqst)
	rs.Conn.Close() //回收rs
	return err
}

func RtspSetupTcpResponse(rs *RtspStream, rqst *RtspHsRqst, s string) error {
	rs.log.Printf("RtpTcp 使用rtsp的端口接收数据")

	RtpChanId, RtcpChanId, err := ParseRtpRtcpChannelId(s)
	if err != nil {
		rs.log.Println(err)
		return err
	}

	if strings.HasSuffix(rqst.Uri, rs.Sdp.VideoAControl) {
		rs.VideoRtpChanId = RtpChanId
		rs.VideoRtcpChanId = RtcpChanId
		rs.log.Printf("Video RtpChanId=%d, RtcpChanId=%d", RtpChanId, RtcpChanId)
	}
	if strings.HasSuffix(rqst.Uri, rs.Sdp.AudioAControl) {
		rs.AudioRtpChanId = RtpChanId
		rs.AudioRtcpChanId = RtcpChanId
		rs.log.Printf("Audio RtpChanId=%d, RtcpChanId=%d", RtpChanId, RtcpChanId)
	}

	date := time.Now().Format(time.RFC1123)
	rsps := fmt.Sprintf(RtspSetupRsps, rqst.Cseq, date, rs.Session, s)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err = rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspSetupUdpResponse(rs *RtspStream, rqst *RtspHsRqst, s string) error {
	rs.log.Printf("RtpUdp 单独开端口接收数据")

	RtpPort, RtcpPort, err := ParseRtpRtcpPort(s)
	if err != nil {
		rs.log.Println(err)
		return err
	}

	if strings.HasSuffix(rqst.Uri, rs.Sdp.VideoAControl) {
		rs.VideoRtpUdpPort = RtpPort
		rs.VideoRtcpUdpPort = RtcpPort
		rs.log.Printf("Video RtpPort=%d, RtcpPort=%d", RtpPort, RtcpPort)
		RtspRtpPortMap.Store(RtpPort, rs)
	}
	if strings.HasSuffix(rqst.Uri, rs.Sdp.AudioAControl) {
		rs.AudioRtpUdpPort = RtpPort
		rs.AudioRtcpUdpPort = RtcpPort
		rs.log.Printf("Audio RtpPort=%d, RtcpPort=%d", RtpPort, RtcpPort)
		RtspRtpPortMap.Store(RtpPort, rs)
	}

	date := time.Now().Format(time.RFC1123)
	s = fmt.Sprintf("%s;server_port=%s-%s", s, conf.Rtsp.Port, conf.Rtsp.Port)
	rsps := fmt.Sprintf(RtspSetupUdpRsps, rqst.Cseq, date, rs.Session, s)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err = rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspSetupResponse(rs *RtspStream, rqst *RtspHsRqst, d []byte) error {
	s := GetRtspHsRqstArg(d[:], "Transport")
	//传输数据的网络协议udp或tcp
	rs.NetProtocol = "udp"
	if strings.Contains(s, "TCP") {
		rs.NetProtocol = "tcp"
	}
	//是否为interleaved模式
	rs.IsInterleaved = false
	if strings.Contains(s, "interleaved") {
		rs.IsInterleaved = true
	}
	rs.log.Printf("NetProtocol=%s, IsInterleaved=%t", rs.NetProtocol, rs.IsInterleaved)

	var err error
	if rs.NetProtocol == "udp" {
		err = RtspSetupUdpResponse(rs, rqst, s)
	} else {
		err = RtspSetupTcpResponse(rs, rqst, s)
	}
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspRecordResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	rsps := fmt.Sprintf(RtspRecordRsps, rqst.Cseq, rs.Session)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err := rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspPlayResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	date := time.Now().Format(time.RFC1123)
	rsps := fmt.Sprintf(RtspPlayRsps, rqst.Cseq, date)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err := rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

func RtspTeardownResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	return nil
}

func RtspGetParameterResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	return nil
}

func RtspErrorResponse(rs *RtspStream, rqst *RtspHsRqst) error {
	rsps := fmt.Sprintf(RtspErrorRsps, rqst.Cseq)
	rs.log.Printf("write len=%d\n%s", len(rsps), rsps)

	_, err := rs.Conn.Write([]byte(rsps))
	if err != nil {
		rs.log.Println(err)
		return err
	}
	return nil
}

//rtsp推流: OPTIONS, ANNOUNCE, SETUP, SETUP, RECORD
//rtsp播放: OPTIONS, DESCRIBE, SETUP, SETUP, PLAY
func RtspHandshakeServer(rs *RtspStream) error {
	var stop bool
	for {
		rs.log.Println("========== rtsp handshake ==========")
		var d [1024]byte
		n, err := rs.Conn.Read(d[:])
		if err != nil {
			rs.log.Println(err)
			return err
		}
		rs.log.Printf("read len=%d\n%s", n, d[:n])

		rqst := ParseRtspHandshakeRqst(d[:n])
		rs.log.Printf("%#v", rqst)

		if rqst.StreamId != "" {
			rs.StreamId = rqst.StreamId
		}

		rs.log.Printf("---------- %s ----------", rqst.Method)
		switch rqst.Method {
		case "OPTIONS":
			err = RtspOptionsResponse(rs, rqst)
		case "ANNOUNCE":
			err = RtspAnnounceResponse(rs, rqst)
			rs.IsPuber = true
		case "DESCRIBE":
			err = RtspDescribeResponse0(rs, rqst)
			rs.IsPuber = false
		case "SETUP":
			err = RtspSetupResponse(rs, rqst, d[:n])
		case "RECORD":
			err = RtspRecordResponse(rs, rqst)
			stop = true //后续就是推流过来的音视频数据了
		case "PLAY":
			err = RtspPlayResponse(rs, rqst)
			stop = true //后续应该发送音视频数据给对方了
		case "TEARDOWN":
			err = RtspTeardownResponse(rs, rqst)
		case "GET_PARAMETER":
			err = RtspGetParameterResponse(rs, rqst)
		default:
			err = fmt.Errorf("undefined rtsp method=%s", rqst.Method)
		}

		if err != nil {
			rs.log.Println(err)
			return err
		}
		if stop == true {
			break
		}
	}
	return nil
}

/*************************************************/
/* RtspHandshakeClient
/*************************************************/
type RtspHsRsps struct {
	Version string
	Code    string
	Message string
	Server  string
	Cseq    string
	Public  string
	WWWAuth string
	Session string
	RtpInfo string
	Body    []byte
}

func ParseRtspHsRsps(d []byte, rsps *RtspHsRsps) {
	p := strings.Split(string(d), "\r\n")
	var str string
	var s []string

	for i := 0; i < len(p); i++ {
		if strings.Contains(p[i], "RTSP") {
			s = strings.Split(p[i], " ")
			rsps.Version = s[0]
			rsps.Code = s[1]
			rsps.Message = s[2]
		}
		if strings.Contains(p[i], "Server") {
			str = strings.TrimSpace(p[i])
			str = strings.Replace(str, " ", "", -1)
			s = strings.Split(str, ":")
			rsps.Server = s[1]
		}
		if strings.Contains(p[i], "CSeq") {
			str = strings.TrimSpace(p[i])
			str = strings.Replace(str, " ", "", -1)
			s = strings.Split(str, ":")
			rsps.Cseq = s[1]
		}
		if strings.Contains(p[i], "Public") {
			str = strings.TrimSpace(p[i])
			str = strings.Replace(str, " ", "", -1)
			s = strings.Split(str, ":")
			rsps.Public = s[1]
		}
		if strings.Contains(p[i], "WWW-Authenticate") {
			str = strings.TrimSpace(p[i])
			str = strings.Replace(str, " ", "", -1)
			s = strings.Split(str, ":")
			rsps.WWWAuth = s[1]
		}
		if strings.Contains(p[i], "Session") {
			str = strings.TrimSpace(p[i])
			str = strings.Replace(str, " ", "", -1)
			s = strings.Split(str, ":")
			rsps.Session = s[1]
		}
		if strings.Contains(p[i], "RTP-Info") {
			str = strings.TrimSpace(p[i])
			s = strings.Split(str, ": ")
			rsps.RtpInfo = s[1]
		}
	}
}

func RtspOptionsRequest(rs *RtspStream) error {
	rqst := fmt.Sprintf(RtspOptionsRqst, rs.Rqst.PullUrl, AppName)
	rs.log.Printf("write len=%d\n%s", len(rqst), rqst)

	n, err := rs.Conn.Write([]byte(rqst))
	if err != nil {
		rs.log.Println(err)
		return err
	}

	var d [1024]byte
	n, err = rs.Conn.Read(d[:])
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Printf("read len=%d\n%s", n, d[:n])

	ParseRtspHsRsps(d[:n], rs.HsRsps)
	return nil
}

func RtspDescribeRequest(rs *RtspStream) error {
	rqst := fmt.Sprintf(RtspDescribeRqst, rs.Rqst.PullUrl, AppName)
	rs.log.Printf("write len=%d\n%s", len(rqst), rqst)

	n, err := rs.Conn.Write([]byte(rqst))
	if err != nil {
		rs.log.Println(err)
		return err
	}

	var d [1024]byte
	n, err = rs.Conn.Read(d[:])
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Printf("read len=%d\n%s", n, d[:n])

	ParseRtspHsRsps(d[:n], rs.HsRsps)

	if strings.Contains(string(d[:]), "200") == true {
		ss := strings.Split(string(d[:n]), "\r\n\r\n")
		if len(ss) < 2 {
			err = fmt.Errorf("have not sdp info")
			rs.log.Println(err)
			return err
		}

		rs.Sdp, _ = ParseSdp([]byte(ss[1]))
		rs.log.Printf("%#v", rs.Sdp)
		return nil
	}
	rs.log.Println("401 UnAuth")
	return nil
}

func RtspSetupRequest(rs *RtspStream, isAudio bool) error {
	//TODO: 判断是udp还是tcp
	rs.IsInterleaved = true
	rs.VideoRtpChanId = 0
	rs.VideoRtcpChanId = 1
	rs.AudioRtpChanId = 2
	rs.AudioRtcpChanId = 3

	rqst := fmt.Sprintf(RtspSetupRqst, rs.Rqst.PullUrl, rs.Sdp.VideoAControl, "0-1", 3, AppName, rs.HsRsps.Session)
	if isAudio == true {
		rqst = fmt.Sprintf(RtspSetupRqst, rs.Rqst.PullUrl, rs.Sdp.AudioAControl, "2-3", 4, AppName, rs.HsRsps.Session)
	}
	rs.log.Printf("write len=%d\n%s", len(rqst), rqst)

	n, err := rs.Conn.Write([]byte(rqst))
	if err != nil {
		rs.log.Println(err)
		return err
	}

	var d [1024]byte
	n, err = rs.Conn.Read(d[:])
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Printf("read len=%d\n%s", n, d[:n])

	ParseRtspHsRsps(d[:n], rs.HsRsps)
	return nil
}

func RtspPlayRequest(rs *RtspStream) ([]byte, error) {
	rqst := fmt.Sprintf(RtspPlayRqst, rs.Rqst.PullUrl, AppName, rs.HsRsps.Session)
	rs.log.Printf("write len=%d\n%s", len(rqst), rqst)

	n, err := rs.Conn.Write([]byte(rqst))
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	var d [1024]byte
	n, err = rs.Conn.Read(d[:])
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	ss := strings.Split(string(d[:n]), "\r\n\r\n")
	rs.log.Printf("read len=%d", n)
	rs.log.Printf("head len=%d\n%s", len(ss[0]), ss[0])
	rs.log.Printf("body len=%d\n%x", len(ss[1]), ss[1])

	ParseRtspHsRsps([]byte(ss[0]), rs.HsRsps)

	if len(ss[1]) == 0 {
		err = fmt.Errorf("have not data")
		rs.log.Println(err)
		return nil, nil
	}
	return []byte(ss[1]), nil
}

//pkg/rtsp/pack.go
//pkg/rtsp/server_command_session.go
//pkg/rtsp/client_command_session.go
//rtsp推流: OPTIONS, ANNOUNCE, SETUP, SETUP, RECORD
//rtsp播放: OPTIONS, DESCRIBE, SETUP, SETUP, PLAY
func RtspHandshakeClient(rs *RtspStream) ([]byte, error) {
	err := RtspOptionsRequest(rs)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	err = RtspDescribeRequest(rs)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	err = RtspSetupRequest(rs, false)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	err = RtspSetupRequest(rs, true)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	//play回应body里 可能有rtp数据(可能多个rtp包)
	d, err := RtspPlayRequest(rs)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}

	rs.log.Printf("%#v", rs.HsRsps)
	return d, nil
}

/*************************************************/
/* RtspHandshake Template
/*************************************************/
//OPTIONS rtsp://192.168.16.160:2995/live/test001 RTSP/1.0
var RtspOptionsRqst = "OPTIONS %s RTSP/1.0\r\n" +
	"CSeq: 1\r\n" +
	"User-Agent: %s\r\n" +
	"\r\n"

//rfc2326 10.1 OPTIONS
var RtspOptionsRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Server: " + AppName + "\r\n" +
	"Public: DESCRIBE, ANNOUNCE, SETUP, PLAY, PAUSE, RECORD, TEARDOWN\r\n" +
	"\r\n"

//ANNOUNCE rtsp://192.168.16.160:2995/live/test001 RTSP/1.0
var RtspAnnounceRqst = "ANNOUNCE %s RTSP/1.0\r\n" +
	"Content-Type: application/sdp\r\n" +
	"CSeq: 2\r\n" +
	"User-Agent: %s\r\n" +
	"Content-Length: %d\r\n" +
	"\r\n"

//rfc2326 10.3 ANNOUNCE
var RtspAnnounceRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Server: " + AppName + "\r\n" +
	"\r\n"

//DESCRIBE rtsp://192.168.16.160:2995/live/test001 RTSP/1.0
var RtspDescribeRqst = "DESCRIBE %s RTSP/1.0\r\n" +
	"Accept: application/sdp\r\n" +
	"CSeq: 2\r\n" +
	"User-Agent: %s\r\n" +
	"\r\n"

var RtspDescribeRspsAu = "RTSP/1.0 401 Unauthorized\r\n" +
	"CSeq: %s\r\n" +
	"Date: %s\r\n" +
	"WWW-Authenticate: %s\r\n" +
	"\r\n"

var RtspDescribeRspsOk = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Date: %s\r\n" +
	"Content-type: application/sdp\r\n" +
	"Content-length: %d\r\n" +
	"\r\n" +
	"%s" //sdp data

/*
SETUP rtsp://192.168.16.160:2995/live/test001/trackID=0 RTSP/1.0
SETUP rtsp://192.168.16.160:2995/live/test001/streamid=0 RTSP/1.0
Transport: RTP/AVP/TCP;unicast;interleaved=0-1
CSeq: 3
User-Agent: Lavf59.27.100
SETUP rtsp://192.168.16.160:2995/live/test001/streamid=1 RTSP/1.0
Transport: RTP/AVP/TCP;unicast;interleaved=2-3
CSeq: 4
User-Agent: Lavf59.27.100
Session: 66334873
*/
var RtspSetupRqst = "SETUP %s/%s RTSP/1.0\r\n" +
	"Transport: RTP/AVP/TCP;unicast;interleaved=%s\r\n" +
	"CSeq: %d\r\n" +
	"User-Agent: %s\r\n" +
	"Session: %s\r\n" +
	"\r\n"

/*
RTSP/1.0 200 OK
CSeq: 3
Date: Thu, 14 Sep 2023 14:58:27 CST
Session: 66334873
Transport: RTP/AVP/TCP;unicast;interleaved=0-1
RTSP/1.0 200 OK
CSeq: 4
Date: Thu, 14 Sep 2023 14:58:27 CST
Session: 66334873
Transport: RTP/AVP/TCP;unicast;interleaved=2-3
*/
//rfc2326 10.4 SETUP
var RtspSetupRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Date: %s\r\n" +
	"Session: %s\r\n" +
	"Transport: %s\r\n" +
	"\r\n"

/*
SETUP rtsp://192.168.16.172:2995/live/test001/streamid=0 RTSP/1.0
Transport: RTP/AVP/UDP;unicast;client_port=8328-8329;mode=record
CSeq: 3
User-Agent: Lavf59.27.100
SETUP rtsp://192.168.16.172:2995/live/test001/streamid=1 RTSP/1.0
Transport: RTP/AVP/UDP;unicast;client_port=8330-8331;mode=record
CSeq: 4
User-Agent: Lavf59.27.100
*/
var RtspSetupUdpRqst = "SETUP %s/%s RTSP/1.0\r\n" +
	"Transport: RTP/AVP/UDP;unicast;client_port=%s-%s;mode=record\r\n" +
	"CSeq: %d\r\n" +
	"User-Agent: %s\r\n" +
	"\r\n"

/*
RTSP/1.0 200 OK
Cseq: 3
Date: Tue, 28 Jul 2015 10:48:34 GMT
Cache-Control: no-cache
Session: 7960110611306097900
Transport: RTP/AVP/UDP;unicast;mode=record;source=xxx.xxx.xxx.xxx;client_port=17600-17601;server_port=6976-6977
RTSP/1.0 200 OK
Cseq: 4
Date: Tue, 28 Jul 2015 10:48:34 GMT
Cache-Control: no-cache
Session: 7960110611306097900
Transport: RTP/AVP/UDP;unicast;mode=record;source=xxx.xxx.xxx.xxx;client_port=55560-55561;server_port=6978-6979
*/
var RtspSetupUdpRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Date: %s\r\n" +
	"Cache-Control: no-cache\r\n" +
	"Session: %s\r\n" +
	"Transport: %s\r\n" +
	"\r\n"

/*
RECORD rtsp://192.168.16.162:2995/live/test002 RTSP/1.0^M
Range: npt=0.000-^M
CSeq: 5^M
User-Agent: Lavf59.27.100^M
Session: 66334873
*/
var RtspRecordRqst = ""

/*
RTSP/1.0 200 OK
Cseq: 5
Session: 7960110611306097900
RTP-Info: url=rtsp://xxx.xxx.xxx.xxx:554/udp.sdp/streamid=0,url=rtsp://xxx.xxx.xxx.xxx:554/udp.sdp/streamid=1
*/
//rfc2326 10.11 RECORD
var RtspRecordRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Session: %s\r\n" +
	"\r\n"

//PLAY rtsp://192.168.16.160:2995/live/test001 RTSP/1.0
var RtspPlayRqst = "PLAY %s RTSP/1.0\r\n" +
	"Range: npt=0.000-\r\n" +
	"CSeq: 5\r\n" +
	"User-Agent: %s\r\n" +
	"Session: %s\r\n" +
	"\r\n"

/*
RTSP/1.0 200 0K
CSeq: 6
Date: Sat, Oct 05 2013 05:02:12 GMT
Range: npt=0.000-60.511
Session: 5DBD3416
RTP-info: url=rtsp://192.168.1.107/test.mkv/track1;seg=32252;rtptime=2700761571,url=rtsp://192168.1.107/test.mkv/track2;seg=22188;rtptime=648106684
*/
//RTP-info必须有, 否则ffplay无法完成play交互 无法播放
//url=rtsp://192.168.16.162:2995/live/test001/streamid=0,url=rtsp://192.168.16.162:2995/live/test001/streamid=1
//url=rtsp://192.168.16.162:2995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa/streamid=0,url=rtsp://192.168.16.162:2995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa/streamid=1
var RtspPlayRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"Date: %s\r\n" +
	"RTP-Info: url=rtsp://192.168.16.162:2995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa/streamid=0,url=rtsp://192.168.16.162:2995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa/streamid=1\r\n" +
	"\r\n"

var RtspTeardownRqst = ""

var RtspTeardownRsps = "RTSP/1.0 200 OK\r\n" +
	"CSeq: %s\r\n" +
	"\r\n"

var RtspGetParameterRqst = ""

var RtspGetParameterRsps = ""

var RtspErrorRsps = "RTSP/1.0 400 ERROR\r\n" +
	"CSeq: %s\r\n" +
	"\r\n"
