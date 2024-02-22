package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"utils"
)

/*************************************************/
/* Sip Register1, 见 J.1 注册信令消息示范
/*************************************************/
type SipRqst struct {
	Method        string
	Uri           string
	Version       string
	Via           string
	ViaProtocol   string
	ViaAddr       string
	ViaIp         string
	ViaPort       string
	ViaBranch     string
	From          string
	FromSip       string
	FromTag       string
	To            string
	ToSip         string
	CallId        string
	CSeq          string
	Contact       string
	MaxForwards   string
	UserAgent     string
	Expires       string
	ContentType   string
	ContentLength int
	AuthInfo
}

type Notify struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       string   `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Status   string   `xml:"Status"`
}

type AuthInfo struct {
	Username  string
	Realm     string
	Nonce     string
	Uri       string
	Response  string
	Algorithm string
	Opaque    string
}

/*
2022/04/03 20:51:17 sip.go:215: sendLen: 293, sendData:
SIP/2.0 200 OK
Via: SIP/2.0/TCP 10.3.220.151:42341;rport=42341;received=172.20.25.20;branch=z9hG4bK581882449
From: <sip:11010000121310000034@1100000012>;tag=57873780
To: <sip:11010000121310000034@1100000012>;tag=z9hG4bK360295268
Call-ID: 934168825
CSeq: 20 MESSAGE
Content-Length:  0
*/
type SipRsps struct {
	Version string
	Code    string
	Msg     string
}

//REGISTER sip:11010800122000000000@11010800122 SIP/2.0
func SipParseRequestLine(s string, sr *SipRqst) error {
	ss := strings.Split(s, " ")
	if len(ss) != 3 {
		return nil
	}

	sr.Method = ss[0]
	sr.Uri = ss[1]
	sr.Version = strings.Replace(ss[2], "\r", "", -1)
	return nil
}

//Via: SIP/2.0/UDP 10.3.220.142:6061;rport;branch=z9hG4bK2140626694
func SipParseVia(s string, sr *SipRqst) error {
	ss := strings.Split(s, " ")
	if len(ss) != 3 {
		return nil
	}
	sr.Via = strings.Replace(s, "\r", "", -1)
	sr.ViaProtocol = ss[1]

	s1 := strings.Split(ss[2], ";")
	if len(ss) < 3 {
		return nil
	}
	sr.ViaAddr = s1[0]
	x := strings.Replace(s1[2], "\r", "", -1)
	sr.ViaBranch = x[7:]

	s2 := strings.Split(s1[0], ":")
	if len(s2) != 2 {
		return nil
	}
	sr.ViaIp = s2[0]
	sr.ViaPort = s2[1]
	return nil
}

//From: <sip:11010800121320000013@11010800122>;tag=1337027167
func SipParseFrom(s string, sr *SipRqst) error {
	ss := strings.Split(s, " ")
	if len(ss) != 2 {
		return nil
	}
	sr.From = strings.Replace(s, "\r", "", -1)

	s1 := strings.Split(ss[1], ";")
	if len(ss) < 2 {
		return nil
	}
	sr.FromSip = s1[0]
	x := strings.Replace(s1[1], "\r", "", -1)
	sr.FromTag = x[4:]
	return nil
}

//To: <sip:11010800121320000013@11010800122>
func SipParseTo(s string, sr *SipRqst) error {
	ss := strings.Split(s, " ")
	if len(ss) != 2 {
		return nil
	}
	sr.To = strings.Replace(s, "\r", "", -1)
	sr.ToSip = strings.Replace(ss[1], "\r", "", -1)
	return nil
}

func SipRqstSplit(s string) (string, string) {
	ss := strings.Split(s, "\r\n\r\n")
	if len(ss) != 2 {
		return "", ""
	}
	return ss[0], ss[1]
}

func SipHeadParse(s string) *SipRqst {
	line := strings.Split(s, "\n")
	lineNum := len(line)

	var sr SipRqst
	SipParseRequestLine(line[0], &sr)

	for i := 1; i < lineNum; i++ {
		if strings.Contains(line[i], "Via:") {
			SipParseVia(line[i], &sr)
		} else if strings.Contains(line[i], "From:") {
			SipParseFrom(line[i], &sr)
		} else if strings.Contains(line[i], "To:") {
			SipParseTo(line[i], &sr)
		} else if strings.Contains(line[i], "Authorization:") {
			SipParseAuth(line[i], &sr)
		} else if strings.Contains(line[i], "Call-ID:") {
			sr.CallId = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "CSeq:") {
			sr.CSeq = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "Contact:") {
			sr.Contact = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "Max-Forwards:") {
			sr.MaxForwards = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "User-Agent:") {
			sr.UserAgent = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "Expires:") {
			sr.Expires = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "Content-Type:") {
			sr.ContentType = SipGetHeader(line[i])
		} else if strings.Contains(line[i], "Content-Length:") {
			ss := SipGetHeader(line[i])
			sr.ContentLength, _ = strconv.Atoi(ss)
		}
	}
	return &sr
}

func SipBodyParse(s string) error {
	var n Notify
	err := xml.Unmarshal([]byte(s), &n)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("%#v", n)
	return nil
}

//Call-ID: 726423896
func SipGetHeader(s string) string {
	ss := strings.Split(s, ":")
	if len(ss) != 2 {
		return ""
	}

	r := strings.TrimSpace(ss[1])
	return strings.Replace(r, "\r", "", -1)
}

func SipRegister1Rsps(sr *SipRqst) []byte {
	rsps := "SIP/2.0 401 Unauthorized\r\n" +
		"Via: SIP/2.0/UDP %s:%s;rport=%s;received=%s;branch=%s\r\n" +
		"From: %s;tag=%s\r\n" +
		"To: %s;tag=%s\r\n" +
		"Call-ID: %s\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"1100000012\",nonce=\"%s\",opaque=\"%s\",algorithm=md5\r\n" +
		"Content-Length: 0\r\n\r\n"

	s := fmt.Sprintf(rsps, sr.ViaIp, sr.ViaPort, sr.ViaPort, conf.GB28181.SipIp, sr.ViaBranch, sr.FromSip, sr.FromTag, sr.ToSip, "z9hG4bK2078339622", sr.CallId, "43b4f4162cfa5a35", "040feeef38b042e6")
	return []byte(s)
}

/*************************************************/
/* Sip Register2, 见 J.1 注册信令消息示范
/*************************************************/
//Authorization: Digest username="11010800121320000013", realm="1100000012", nonce="43b4f4162cfa5a35", uri="sip:11010800122000000000@11010800122", response="2b84926fd3a78ac411abb16b6e6fa774", algorithm=MD5, opaque="040feeef38b042e6"
func SipGetAuth(s, key string) string {
	ss := len(key) + 1
	ee := len(s) - 2
	return s[ss:ee]
}

func SipParseAuth(s string, sr *SipRqst) error {
	line := strings.Split(s, " ")
	lineNum := len(line)

	var ai AuthInfo
	for i := 1; i < lineNum; i++ {
		if strings.Contains(line[i], "username=") {
			ai.Username = SipGetAuth(line[i], "username=")
		} else if strings.Contains(line[i], "realm=") {
			ai.Realm = SipGetAuth(line[i], "realm=")
		} else if strings.Contains(line[i], "nonce=") {
			ai.Nonce = SipGetAuth(line[i], "nonce=")
		} else if strings.Contains(line[i], "uri=") {
			ai.Uri = SipGetAuth(line[i], "uri=")
		} else if strings.Contains(line[i], "respone=") {
			ai.Response = SipGetAuth(line[i], "respone=")
		} else if strings.Contains(line[i], "algorithm=") {
			ai.Algorithm = SipGetAuth(line[i], "algorithm=")
		} else if strings.Contains(line[i], "opaque=") {
			ai.Opaque = SipGetAuth(line[i], "opaque=")
		}
	}
	sr.AuthInfo = ai
	return nil
}

func SipRegister2Rsps(sr *SipRqst) []byte {
	rsps := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP %s:%s;rport=%s;received=%s;branch=%s\r\n" +
		"From: %s;tag=%s\r\n" +
		"To: %s;tag=%s\r\n" +
		"Call-ID: %s\r\n" +
		"CSeq: 2 REGISTER\r\n" +
		"Date: %s\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n"

	//2022-04-03T20:51:12.413
	date := utils.GetYMDHMS1()

	s := fmt.Sprintf(rsps, sr.ViaIp, sr.ViaPort, sr.ViaPort, conf.GB28181.SipIp, sr.ViaBranch, sr.FromSip, sr.FromTag, sr.ToSip, "z9hG4bK360295267", sr.CallId, date)
	return []byte(s)
}

/*************************************************/
/* keepalive 心跳, 见 J.12 设备状态信息报送消息示范
/*************************************************/
func SipMessageRsps(sr *SipRqst) []byte {
	rsps := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP %s:%s;rport=%s;received=%s;branch=%s\r\n" +
		"From: %s;tag=%s\r\n" +
		"To: %s;tag=%s\r\n" +
		"Call-ID: %s\r\n" +
		"CSeq: %s\r\n" +
		"Content-Length: 0\r\n\r\n"

	s := fmt.Sprintf(rsps, sr.ViaIp, sr.ViaPort, sr.ViaPort, conf.GB28181.SipIp, sr.ViaBranch, sr.FromSip, sr.FromTag, sr.ToSip, "z9hG4bK360295267", sr.CallId, sr.CSeq)
	return []byte(s)
}

/*************************************************/
/* 邀请设备推流, 见 9.2 实时视音频点播
/* 见 J.4 客户端发起的实时点播消息示范
/*************************************************/
func IpcInvite() {

}

/*************************************************/
/* sip tcp server
/*************************************************/
func SipHandlerTcp(c net.Conn) {
	buf := make([]byte, 1024)
	n, err := c.Read(buf)
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("len:%d, SipData:\n%s", n, string(buf))
}

func SipServerTcp() {
	addr := fmt.Sprintf(":%s", conf.GB28181.SipPort)
	log.Printf("listen sip(tcp) on %s", addr)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalln(err)
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println("---------->> new sip(tcp) connect")
		log.Println("RemoteAddr:", c.RemoteAddr().String())

		go SipHandlerTcp(c)
	}
}

/*************************************************/
/* sip udp server
/*************************************************/
func SipHandlerUdp(l *net.UDPConn) {
	var n int
	var r *net.UDPAddr
	var err error
	var sr *SipRqst
	var rqst, head, body string
	var rsps []byte
	buf := make([]byte, 1024)
	i := 0

	for {
		n, r, err = l.ReadFromUDP(buf)
		if err != nil {
			log.Println(err)
			return
		}
		rqst = string(buf[:n])
		log.Printf("recvFrom %s:%d, SipMsgSeq %d, len=%d, data:\n%s", r.IP, r.Port, n, rqst)
		log.Printf("--> SipRqstNum %d", i)
		i++

		head, body = SipRqstSplit(rqst)
		sr = SipHeadParse(head)

		if sr.ContentLength > 0 {
			//Content-Type: Application/MANSCDP+xml
			SipBodyParse(body)
		}

		switch sr.CSeq {
		case "CSeq: 1 REGISTER":
			rsps = SipRegister1Rsps(sr)
		case "2 REGISTER":
			rsps = SipRegister2Rsps(sr)
		case "20 MESSAGE": //心跳
			rsps = SipMessageRsps(sr)
		default:
			log.Printf("undefined sip CSeq %s", sr.CSeq)
			continue
		}

		// 发送给对方
		n, err = l.WriteToUDP(rsps, r)
		if err != nil {
			log.Println(err)
			return
		}
		log.Printf("sendTo %s:%d, len=%d, data:\n%s", r.IP, r.Port, n, string(rsps))
	}
}

func SipServerUdp() {
	addr := fmt.Sprintf(":%s", conf.GB28181.SipPort)
	log.Printf("listen sip(udp) on %s", addr)

	laddr, _ := net.ResolveUDPAddr("udp", addr)
	l, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln(err)
	}

	SipHandlerUdp(l)
}
