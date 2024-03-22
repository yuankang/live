package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"utils"
)

type Rsps struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func GetRsps(code int, msg string) []byte {
	r := Rsps{code, msg}
	d, err := json.Marshal(r)
	if err != nil {
		log.Println(err)
		return d
	}
	log.Println(string(d))
	return d
}

func GetVersion(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	s := fmt.Sprintf("%s %s", AppName, AppVersion)
	d := GetRsps(200, s)
	return d, nil
}

type PlayAddrTime struct {
	PlayIpPort string `json:"playIpPort"`
	Timestamp  int64  `json:"timestamp"`
}

// POST, PUT, DELETE
func HttpRequest(method, url string, data []byte, to time.Duration, redoNum int) ([]byte, error) {
	client := &http.Client{Timeout: to * time.Second}

	r := strings.NewReader(string(data))
	rqst, err := http.NewRequest(method, url, r)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	ts := fmt.Sprintf("%d", utils.GetTimestamp("ms"))
	ss := fmt.Sprintf("%s%s", "SPb4nd5anEKs", ts)
	md5 := utils.Md5Sum(ss)
	rqst.Header.Add("Content-Type", "application/json")
	rqst.Header.Add("timestamp", ts)
	rqst.Header.Add("sign", md5)

	redo := 1
REDO:
	rsps, err := client.Do(rqst)
	if err != nil {
		//log.Println(err)
		if redo < redoNum {
			redo++
			goto REDO
		}
		return nil, err
	}
	defer rsps.Body.Close()

	d, err := ioutil.ReadAll(rsps.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return d, nil
}

type StreamStateInfo struct {
	s     Stream
	state int
}

/*************************************************/
/* Stream Publish Authenticate
/*************************************************/
type PubAuthRqst struct {
	IpPusher   string `json:"clientIp"`   // 推流端ip
	IpOuter    string `json:"serverIp"`   // 本服务公网ip
	IpInner    string `json:"innerIp"`    // 本服务内网ip
	StreamId   string `json:"streamName"` //
	AppName    string `json:"appName"`    //
	PushDomain string `json:"pushDomain"` //
	Sign       string `json:"sign"`       // 从url里获取, 可以为空
	DomainKey  string `json:"domainKey"`  // 常量 "domainKey"
	Timestamp  uint32 `json:"timestamp"`  // 从url里获取, 没有为0
	Tm         int64  `json:"tm"`         // 接口鉴权用
	Auth       string `json:"auth"`       // 接口鉴权用
}

type PubAuthRsps struct {
	Code int           `json:"code"`
	Msg  string        `json:"message"`
	Data PubAuthResult `json:"data"`
}

//RecordType
//0:不录制
//1:实时录制	mqtt使用 "TopicRec":"hlsTsInfo"
//2:打点录制	没有对应的topic
//3:AI移动侦测	mqtt使用 "TopicAi":"aIHlsTsInfo"
//4:补录录制	mqtt使用 "TopicRec":"hlsTsInfo"
type PubAuthResult struct {
	StreamId   string `json:"streamId"`
	HlsUpload  int    `json:"hlsUpload"`  // 是否录制, 0不录, 1录制
	ResultCode int    `json:"resultCode"` // 鉴权结果, 0失败, 1成功
	AesKey     string `json:"aesKey"`     // 空为不加密, "Encry_i9oD5fFqnE"
	RecordType int    `json:"recordType"` // 录制类型
}

func PublishAuth(s *Stream) {
	var par PubAuthRqst
	par.IpPusher = s.RemoteIp
	par.IpOuter = conf.IpOuter
	par.IpInner = conf.IpInner
	par.StreamId = s.AmfInfo.StreamId
	par.AppName = s.AmfInfo.App
	par.PushDomain = s.RemoteIp
	par.Sign = ""
	par.DomainKey = "domainKey"
	par.Timestamp = 0
	par.Tm = utils.GetTimestamp("ms")

	ss := fmt.Sprintf("%s%d%s%s", par.IpOuter, par.Tm, par.StreamId, "d5c4171992ed422eab3086d7fb6b4fdc")
	par.Auth = utils.Md5Sum(ss)

	//http://172.20.25.29:20093/api/stream/pushAuth
	url := fmt.Sprintf("%s%s", conf.Cc.Server, conf.Cc.ApiAuth)
	s.log.Printf("PubAuthUrl: %s", url)
	s.log.Printf("PubAuthData: %#v", par)

	d, err := json.Marshal(par)
	if err != nil {
		s.log.Println(err)
		return
	}

	d, err = HttpRequest("POST", url, d, 5, 3)
	if err != nil {
		s.log.Println(err)
		s.TransmitSwitch = "off"
		return
	}
	s.log.Printf("PubAuthRsps: %s", string(d))

	var paRsps PubAuthRsps
	err = json.Unmarshal(d, &paRsps)
	if err != nil {
		s.log.Println(err)
		s.TransmitSwitch = "off"
		return
	}
	s.log.Printf("PubAuthRsps: %#v", paRsps)

	if len(paRsps.Data.AesKey) != 16 {
		s.log.Println("AesKeyLen != 16")
	}

	// 鉴权返回失败, 要断开推流
	if paRsps.Data.ResultCode == 0 {
		s.TransmitSwitch = "off"
	} else {
		s.PubAuth = paRsps
	}
}

/*************************************************/
/* Stream State Report
/*************************************************/
type StreamStateRqst struct {
	IpPusher    string `json:"clientIp"`    // 推流端ip
	IpOuter     string `json:"serverIp"`    // 本服务公网ip
	IpInner     string `json:"innerIp"`     // 本服务内网ip
	StreamId    string `json:"streamId"`    // "GSP3bnx69BgxI-gCec0oMfJT"
	StreamState int    `json:"streamState"` // 1直播开始, 2直播结束
	ReportTime  string `json:"reportTime"`  // "20221108115326"
	Width       int    `json:"width"`       // 1920
	Height      int    `json:"height"`      // 1080
	CodeType    string `json:"codeType"`    // "H264" or "H265"
}

type StreamStateRsps struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
}

func StreamStateReport(s *Stream, state int) {
	var ssr StreamStateRqst
	ssr.IpPusher = s.RemoteIp
	ssr.IpOuter = conf.IpOuter
	ssr.IpInner = conf.IpInner
	ssr.StreamId = s.AmfInfo.StreamId
	ssr.StreamState = state
	ssr.ReportTime = utils.GetYMDHMS()
	ssr.Width = s.Width
	ssr.Height = s.Height
	ssr.CodeType = s.VideoCodecType

	//http://172.20.25.29:20093/api/stream/streamStateChange
	url := fmt.Sprintf("%s%s", conf.Cc.Server, conf.Cc.ApiReport)
	s.log.Printf("StreamStateUrl: %s", url)
	s.log.Printf("StreamStateData: %#v", ssr)

	d, err := json.Marshal(ssr)
	if err != nil {
		s.log.Println(err)
		return
	}

	var to time.Duration
	var redoNum int
	if state == 1 {
		to = 4
		redoNum = 1
	} else if state == 2 {
		to = 5
		redoNum = 3
	}

	d, err = HttpRequest("POST", url, d, to, redoNum)
	if err != nil {
		s.log.Println(err)
		return
	}
	s.log.Println(string(d))

	var ssRsps StreamStateRsps
	err = json.Unmarshal(d, &ssRsps)
	if err != nil {
		s.log.Println(err)
		return
	}
	s.log.Printf("StreamStateRsps: %#v", ssRsps)
}

/*************************************************/
/* GB28181业务请求
/*************************************************/
type GbRqst struct {
	App             string `json:"app"`
	StreamId        string `json:"streamId"`
	TransType       string `json:"transType"` //tcp/udp
	ConnType        string `json:"connType"`  //主被动
	RtpSsrc         string `json:"rtpSsrc"`
	RtpSsrcTmp      string //长度为10的字符串
	RtpSsrcUint     uint32 //要能存放下10位数
	RtpSsrcUintTmp  uint32 //要能存放下10位数
	RemoteIp        string `json:"remoteIp"`
	RemoteVideoPort int    `json:"remoteRtpVideoPort"`
	RemoteAudioPort int    `json:"remoteRtpAudioPort"`
}

type GbRsps struct {
	Code     int       `json:"code"`
	Msg      string    `json:"message"`
	StreamId string    `json:"streamId"`
	Ip       string    `json:"ip,omitempty"`
	TcpPort  *DataPort `json:"tcpPort,omitempty"`
	UdpPort  *DataPort `json:"udpPort,omitempty"`
}

type DataPort struct {
	Rtp  int `json:"rtp"`
	Rtcp int `json:"rtcp"`
}

//POST /api/v1/gb28181?app=slivegateway&action=create_pullChannel&ts=1710498633346&token=29f230208992a5a6f521e279788a973d
//{"app":"SPq4nchhCcSk","streamId":"GSPq4nchhCcSk-acuv6p3262","transType":"tcp","connType":"passive","rtpSsrc":"0000003182","type":0}
//{"code":200,"message":"SUCCESS","ip":"0.0.0.0","tcpPort":{"rtp":9020,"rtcp":9021}}
func GB28181Create(w http.ResponseWriter, r *http.Request, d []byte) ([]byte, error) {
	var rqst GbRqst
	err := json.Unmarshal(d, &rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	rqst.RtpSsrcTmp = rqst.RtpSsrc
	n, _ := strconv.ParseUint(rqst.RtpSsrc, 10, 0)
	rqst.RtpSsrcUint = uint32(n)
	rqst.RtpSsrcUintTmp = uint32(n)
	//key := fmt.Sprintf("%s_%s", rqst.App, rqst.StreamId)
	key := fmt.Sprintf("%s", rqst.StreamId)
	log.Printf("stream key %s", key)

	_, ok := StreamMap.Load(key)
	if ok == true { //流id已存在, 断开连接并返回错误
		err = fmt.Errorf("streamId %s exist", key)
		log.Println(err)
		return nil, err
	}

	s, _ := NewGb28181Stream(key, rqst)
	log.Printf("log %s", s.LogFn)
	s.log.Println("==============================")
	s.log.Printf("%#v", rqst)

	StreamMap.Store(key, s)
	SsrcMap.Store(rqst.RtpSsrcUint, s)

	var rsps GbRsps
	rsps.Code = 200
	rsps.Msg = "ok"
	rsps.StreamId = rqst.StreamId
	rsps.Ip = conf.IpOuter

	var tcp, udp DataPort
	tcp.Rtp = conf.RtpRtcp.FixedRtpPort
	tcp.Rtcp = conf.RtpRtcp.FixedRtcpPort
	udp.Rtp = conf.RtpRtcp.FixedRtpPort
	udp.Rtcp = conf.RtpRtcp.FixedRtcpPort
	rsps.TcpPort = &tcp
	rsps.UdpPort = &udp

	dd, _ := json.Marshal(rsps)
	log.Println(string(dd))
	return dd, nil
}

//POST /api/v1/gb28181?app=slivegateway&action=start_pullChannel&ts=1710499535067&token=4ef9f1b00ddca8e29e20cabfc7045fcc
//{"remoteIp":"172.16.20.110","remoteRtpAudioPort":0,"remoteRtpVideoPort":15060,"streamId":"GSPq4nchhCcSk-acuv6p3262","rtpSsrc":"0000000842"}
//{"code":200,"message":"SUCCESS","streamId":"GSP3bnx69BgxI-gCec0oMfJT"}
func GB28181Start(w http.ResponseWriter, r *http.Request, d []byte) ([]byte, error) {
	var rqst GbRqst
	err := json.Unmarshal(d, &rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//key := fmt.Sprintf("%s_%s", rqst.App, rqst.StreamId)
	key := fmt.Sprintf("%s", rqst.StreamId)
	log.Printf("stream key %s", key)

	v, ok := StreamMap.Load(key)
	if ok == false { //流id不存在, 断开连接并返回错误
		err = fmt.Errorf("streamId %s is't exist", key)
		log.Println(err)
		return nil, err
	}
	s := v.(*Stream)

	s.GbRqst.RtpSsrc = rqst.RtpSsrc
	n, _ := strconv.ParseUint(rqst.RtpSsrc, 10, 0)
	s.GbRqst.RtpSsrcUint = uint32(n)
	s.GbRqst.RemoteIp = rqst.RemoteIp
	s.GbRqst.RemoteVideoPort = rqst.RemoteVideoPort
	s.GbRqst.RemoteAudioPort = rqst.RemoteAudioPort
	s.log.Printf("%#v", rqst)
	//s.log.Printf("%#v", s.GbRqst)

	if s.GbRqst.RtpSsrcTmp != s.GbRqst.RtpSsrc {
		SsrcMap.Delete(s.GbRqst.RtpSsrcUintTmp)
		SsrcMap.Store(s.GbRqst.RtpSsrcUint, s)
	}

	var rsps GbRsps
	rsps.Code = 200
	rsps.Msg = "ok"
	rsps.StreamId = rqst.StreamId

	dd, _ := json.Marshal(rsps)
	log.Println(string(dd))
	return dd, nil
}

//POST /api/v1/gb28181?app=slivegateway&action=delete_pullChannel
//{"app":"SP3bnx69BgxI","streamId":"GSP3bnx69BgxI-avEc0oE4C4"}
//{"code":200,"message":"SUCCESS","streamId":"GSP3bnx69BgxI-avEc0oE4C4"}
//{"code":6002,"message":"stream unexist"}
func GB28181Delete(w http.ResponseWriter, r *http.Request, d []byte) ([]byte, error) {
	var rqst GbRqst
	err := json.Unmarshal(d, &rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	//log.Printf("%#v", rqst)

	//key := fmt.Sprintf("%s_%s", rqst.App, rqst.StreamId)
	key := fmt.Sprintf("%s", rqst.StreamId)
	log.Println("stream key", key)

	var rsps GbRsps
	rsps.Code = 200
	rsps.Msg = "SUCCESS"
	rsps.StreamId = rqst.StreamId

	v, ok := StreamMap.Load(key)
	if ok == false {
		rsps.Code = 6002
		rsps.Msg = "stream nunexist"
	} else {
		s := v.(*Stream)
		SsrcMap.Delete(s.GbRqst.RtpSsrcUint)
		StreamMap.Delete(key)
	}

	dd, _ := json.Marshal(rsps)
	log.Println(string(dd))
	return dd, nil
}

type PushStreamList struct {
	Code int      `json:"code"`
	Msg  string   `json:"message"`
	Ts   int64    `json:"timestamp"`
	List []string `json:"streamList"`
}

//GET /api/v1/gb28181?action=get_pushChannels
//{"code":200,"message":"success","streamList":[],"timestamp":1710499227164}
func GB28181StreamList(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var rsps PushStreamList
	rsps.Code = 200
	rsps.Msg = "success"
	rsps.Ts = utils.GetTimestamp("ms")

	var i int
	var s *Stream
	SsrcMap.Range(func(k, v interface{}) bool {
		s, _ = v.(*Stream)
		log.Printf("%d, ssrc=%.10d, streamid=%s", i, k, s.GbRqst.StreamId)
		i++
		return true
	})

	dd, _ := json.Marshal(rsps)
	log.Println(string(dd))
	return dd, nil
}

//GET /api/v1/streamList
//{"code":200,"msg":"ok","streamList":["GSPb4ohbi65Wm-eZPaao5h2P","GSPb4ohbi65Wm-eZSaaoO8BR"]}
//func GetAllPubStreamId()

/*************************************************/
/* rtsp api
/*************************************************/
/*
rtsp://[username]:[password]@[ip]:[port]/[codec]/[channel]/[subtype]/av_stream
rtsp://admin:12345@192.168.1.67:554/h264/ch1/main/av_stream
rtsp://admin:123456@10.6.4.247:554/ch10/0
rtsp://admin:brzx666888@10.6.246.76:554
rtsp://192.168.16.1:5544/live/test110
rtsp://125.39.179.77:2554/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa
rtmp://127.0.0.1:1935/live/test002?owner=Spy2023Zjr
create接口post的数据
{
    "app":"SPq3pr6f6kNa",
    "sourceUrl":"rtsp://admin:brzx666888@10.6.246.76:554",
    "streamId":"RSSPq3pr6f6kNa-ddsn4p4hYa",
    "sourceOption":{
        "rtsp_transport":"tcp"
    },
    "hookUrl":"",
    "retry":3
}
delete接口post的数据
{
    "app":"SPq3pr6f6kNa",
    "streamId":"RSPq3pr6f6kNa-eFY54pI7qA"
}
*/
type PullOpt struct {
	//拉流网络协议, 支持tcp/udp/multicast, 默认tcp
	PullNetPtcl string `json:"rtsp_transport"`
}

type RtspRqst struct {
	PullUrl   string `json:"sourceUrl"` //拉流地址
	PullOpt   `json:"sourceOption"`
	PullRetry int    `json:"retry"`     //拉流重试次数, 默认3次
	PushUrl   string `json:"casterUrl"` //推流地址, 为空不推流
	ReportUrl string `json:"hookUrl"`   //任务状态回调地址

	PullAuth string
	PullIp   string
	PullPort string
	PullPath []string
	PullApp  string
	PullSid  string
	PullArgs string

	PushIp   string
	PushPort string
	PushPath []string
	PushApp  string `json:"app"`      //默认值rtsp
	PushSid  string `json:"streamId"` //推流的流id
	PushArgs string

	PullKey string //PullIp_PullPort_PullPath[0]_PullPath[n]
	PushKey string //PushIp_PushPort_PushPath[0]_PushPath[n]
}

func RtspRqstUrlParse(rqst *RtspRqst) error {
	ua, err := UrlParse(rqst.PullUrl)
	if err != nil {
		log.Println(err)
		return err
	}
	//log.Printf("%#v", ua)

	rqst.PullAuth = ua.Auth
	rqst.PullIp = ua.Ip
	rqst.PullPort = ua.Port
	rqst.PullPath = ua.Path
	rqst.PullApp = ua.Path[0]
	if len(ua.Path) > 1 {
		rqst.PullSid = ua.Path[1]
	}
	rqst.PullArgs = ua.Args
	//rqst.PullKey = ua.Key
	rqst.PullKey = fmt.Sprintf("%s_%s", rqst.PullApp, rqst.PullSid)

	if rqst.PushUrl == "" {
		log.Println("PushUrl is empty")
		return nil
	}

	ua, err = UrlParse(rqst.PushUrl)
	if err != nil {
		log.Println(err)
		return err
	}
	//log.Printf("%#v", ua)

	rqst.PushIp = ua.Ip
	rqst.PushPort = ua.Port
	rqst.PushPath = ua.Path
	rqst.PushApp = ua.Path[0]
	if len(ua.Path) > 1 {
		rqst.PushSid = ua.Path[1]
	}
	rqst.PushArgs = ua.Args
	//rqst.PushKey = ua.Key
	rqst.PushKey = fmt.Sprintf("%s_%s", rqst.PushApp, rqst.PushSid)
	rqst.PullKey = fmt.Sprintf("%s_%s", rqst.PushApp, rqst.PushSid)
	return nil
}

//http://172.20.25.24:8082/api/v1/streams?action=create_streamProxy&app=slivestreamproxy&ts=1684290363147&token=7d475daf67656461850fb17a998583d3
func HttpApiRtspPullCreate(w http.ResponseWriter, r *http.Request, d []byte) ([]byte, error) {
	var rqst RtspRqst
	rs := NewRtspStream(nil)

	err := json.Unmarshal(d, &rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//判断参数是否合法
	if rqst.PullUrl == "" {
		err = fmt.Errorf("rtsp pull url is empty")
		log.Println(err)
		return nil, err
	}
	if rqst.PushUrl == "" {
		rqst.PushIp = "127.0.0.1"
		rqst.PushPort = "1935"
		rqst.PushUrl = fmt.Sprintf("rtmp://%s:%s/%s/%s", rqst.PushIp, rqst.PushPort, rqst.PushApp, rqst.PushSid)
	}
	if rqst.ReportUrl == "" {
		rqst.ReportUrl = conf.Rtsp.ReportUrl
	}

	//是否后门推流, 仅用于测试, 后门字符串 不能出现在配置和日志中
	if strings.Contains(rqst.PushUrl, BackDoor) == false && conf.Rtsp.PushBackDoor == true {
		if strings.Contains(rqst.PushUrl, "?") == true {
			rqst.PushUrl = fmt.Sprintf("%s&%s", rqst.PushUrl, BackDoor)
		} else {
			rqst.PushUrl = fmt.Sprintf("%s?%s", rqst.PushUrl, BackDoor)
		}
	}

	//解析拉流地址信息和推流地址信息
	err = RtspRqstUrlParse(&rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	rs.log.Println("==============================")
	rs.log.Printf("%#v", rqst)
	rs.log.Println("==============================")

	//判断拉流任务是否已经存在
	_, ok := RtspPuberMap.Load(rqst.PushKey)
	if ok == true {
		err = fmt.Errorf("rtsp pull %s is exist", rqst.PullUrl)
		log.Println(err)

		log.Printf("rm %s", rs.LogFn)
		rs.LogFp.Close()
		_ = os.Remove(rs.LogFn)
		return nil, err
	}
	rs.Rqst = &rqst
	rs.StreamId = rqst.PushSid
	RtspPuberMap.Store(rqst.PushKey, rs)

	go RtspPuller(rs)

	rsps := GetRsps(200, "ok")
	return rsps, nil
}

func HttpApiRtspPullDelete(w http.ResponseWriter, r *http.Request, d []byte) ([]byte, error) {
	var rqst RtspRqst
	err := json.Unmarshal(d, &rqst)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	//判断参数是否合法
	if rqst.PushApp == "" || rqst.PushSid == "" {
		err = fmt.Errorf("args is empty")
		log.Println(err)
		return nil, err
	}
	if rqst.ReportUrl == "" {
		rqst.ReportUrl = conf.Rtsp.ReportUrl
	}

	rqst.PushKey = fmt.Sprintf("%s_%s", rqst.PushApp, rqst.PushSid)
	log.Printf("%#v", rqst)

	//判断拉流任务是否已经存在
	v, ok := RtspPuberMap.Load(rqst.PushKey)
	if ok == false {
		err = fmt.Errorf("rtsp pull %s is not exist", rqst.PullUrl)
		log.Println(err)
		return nil, err
	}
	rs := v.(*RtspStream)
	rs.Stop = true
	//这里从map删除, 不影响之前获得指针的使用
	RtspPuberMap.Delete(rqst.PushKey)

	rsps := GetRsps(200, "ok")
	return rsps, nil
}
