package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"
)

/*
v=0
o=- 0 0 IN IP4 127.0.0.1
s=No Name
c=IN IP4 192.168.16.143
t=0 0
a=tool:libavformat LIBAVFORMAT_VERSION
m=video 0 RTP/AVP 96
b=AS:1606
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1; sprop-parameter-sets=Z2QAH6zRAFAFuwFqAgICgAAB9IAAdTAHjBiJ,aOuPLA==; profile-level-id=64001F
a=control:streamid=0
m=audio 0 RTP/AVP 97
b=AS:128
a=rtpmap:97 MPEG4-GENERIC/44100/2
a=fmtp:97 profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3; config=12100000000000000000000000000000
a=control:streamid=1

## 码率bitrate
b=AS:128代表这路媒体流可使用多少带宽

## rtsp的sdp中视频sps和pps的说明
当packetization-mode=1时, RTP打包H.264的NALU单元必须使用非交错(non-interleaved)封包模式
sprop-parameter-sets=Z2QAH6zRAFAFuwFqAgICgAAB9IAAdTAHjBiJ,aOuPLA==;
sprop-parameter-sets是SPS和PPS的的Base64之后的字符串, 中间以逗号分割
SpsBase64:Z2QAH6zRAFAFuwFqAgICgAAB9IAAdTAHjBiJ
PpsBase64:aOuPLA==
profile-level-id=64001F 就是Sps去掉NaluHeader后的前3字节
profile-level-id=42C01E 0x42表示Sps.ProfileIdc, 0xC0表示Sps中各个Flag, 0x1E表示Sps.LevelIdc, 0x1E即30代表了LevelIdc=3(30/3)
rtsp协议 流的 sps pps 要么在sdp里 要么在rtp包的nalu数据里, 至少有一个地方有
ffmpeg推rtsp流 只有sdp有sps pps, rtp包的nalu数据没有 sps pps
ffmpeg查看是否有sps pps的命令
ffmpeg -rtsp_transport tcp -i rtsp://125.39.179.77:2554/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa -an -c copy -t 10 -bsf:v trace_headers -f null - 2>&1 | grep Parameter

## rtsp的sdp中音频为aac时的说明
a=rtpmap:97 mpeg4-generic/12000/2
rtpmap表示音频为AAC的其sample采样率为12000双通道音频，其中mpeg4-generic代表了AAC的互联网媒体类型
a=fmtp:96 profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;config=1490
这里面是AAC的详细编码和封装信息：
profile-level-id=1; 指定1表示低复杂度类型
mode=AAC-hbr; 代表编码模式
sizelength=13;indexlength=3;indexdeltalength=3 涉及到AAC的AU Header,如果一个RTP包则只包含一个AAC包,则没有这三个字段,有则说明是含有AU Header字段, AAC的RTP封装中的AU头详见 https://www.cnblogs.com/djw316/p/10975372.html
AU-size 由 sizeLength 决定表示本段音频数据占用的字节数
AU-Index 由 indexLength 决定表示本段的序号, 通常0开始
AU-Index-delta 由 indexdeltaLength 决定表示本段序号与上一段序号的差值
config=1210(十六进制); 是AudioSpecificConfig的值, 二进制: 00010 0100 0010 000, 2 4 2
config=1508(十六进制); 是AudioSpecificConfig的值, 二进制: 00010 1010 0001 000, 2 10 1
分别代表aac的profile是2, 10代表采样率是11025, 通道个数是1

RTP封装AAC的协议采用MPEG-4格式的封装协议rfc3640, RTP在封装AAC包通常和RTSP信令SDP的AAC音频信息的填充有一定关系
通常根据AAC码率大小可以分为Low Bit-rate AAC以及High Bit-rate AAC模式

Low Bit-rate下规定AAC的一帧大小最大不超过63字节。在Low Bit-rate AAC模式下其对应的SDP(示例)信息如下所示；SDP中的mode=AAC-lbr，表示RTP封包的AAC采用Low Bit-rate AAC的模式；sizeLength则表示AAC编码帧长这一参数占用的bit数，sizeLength=6则表示AAC帧长这一参数中占6bit所以编码帧长取值最大是63(取值范围0-63)，即AAC编码帧长最大63字节。
m=audio 49230 RTP/AVP 96
a=rtpmap:96 mpeg4-generic/22050/1
a=fmtp:96 streamtype=5; profile-level-id=14; mode=AAC-lbr; config=
1388; sizeLength=6; indexLength=2; indexDeltaLength=2;
constantDuration=1024; maxDisplacement=5

High Bit-rate AAC下规定一帧大小最大不超过8191字节。在High Bit-rate AAC其对应的SDP(示例)信息如下；SDP中mode=AAC-hbr，表示RTP封包的AAC采用High Bit-rate AAC的模式；sizeLength则表示AAC编码帧长这一参数占用的bit数，sizeLength=13则表示AAC编码帧长这一参数中占13bit所以取值最大是8191(取值范围0-8191)，即AAC帧长最大8191字节。
m=audio 49230 RTP/AVP 96
a=rtpmap:96 mpeg4-generic/48000/6
a=fmtp:96 streamtype=5; profile-level-id=16; mode=AAC-hbr;
config=11B0; sizeLength=13; indexLength=3;
indexDeltaLength=3; constantDuration=1024
*/

var SdpFmt = `v=0
o=- 0 0 IN IP4 %s
s=%s
c=IN IP4 %s
t=0 0
a=tool:%s
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1; sprop-parameter-sets=%s,%s; profile-level-id=%X
a=control:streamid=0
m=audio 0 RTP/AVP 97
a=rtpmap:97 MPEG4-GENERIC/%d/%d
a=fmtp:97 profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3; config=%X
a=control:streamid=1
`

type SdpInfo struct {
	Oip  string
	Cip  string //可以同Oip
	App  string
	Tool string //可以同App
	Sps  string //base64编码的sps
	Pps  string //base64编码的pps
	Vsc  []byte //3B, 16进制 就是Sps去掉NaluHeader后的前3字节
	Asr  int    //audio samplerate
	Acn  int    //audio channel num
	Asc  []byte //2B, 16进制, AudioSpecificConfig
}

func CreateSdp(si *SdpInfo) (string, error) {
	//log.Printf("%#v", si)
	sdp := fmt.Sprintf(SdpFmt, si.Oip, si.App, si.Cip, si.Tool, si.Sps, si.Pps, si.Vsc, si.Asr, si.Acn, si.Asc)
	return sdp, nil
}

type Sdp struct {
	RawSdp      []byte
	NetProtocol string //tcp or udp

	VideoPayloadTypeInt int    //96:h264
	VideoPayloadTypeStr string //96:h264
	VideoClockRate      int    //90000
	VideoAControl       string //streamid=0

	AudioPayloadTypeInt int    //97:aac
	AudioPayloadTypeStr string //97:aac
	AudioClockRate      int    //8000, 16000, 44100
	AudioChannelNum     int    //1, 2
	AudioAControl       string //streamid=1
	AudioProfileId      int
	AacC                *AudioSpecificConfig
	AacCData            []byte

	SpsBase64 string
	PpsBase64 string
	SpsData   []byte
	PpsData   []byte
	Sps       *Sps
	Width     int
	Height    int
}

func GetSessionSegment(d []byte) string {
	s := 0
	e := strings.Index(string(d), "m=")
	return string(d[s:e])
}

func GetVideoSegment(d []byte) string {
	s := strings.Index(string(d), "m=video")
	if s < 0 {
		return ""
	}
	e := strings.Index(string(d), "m=audio")
	if e < 0 {
		e = len(d)
	}
	return string(d[s:e])
}

func GetAudioSegment(d []byte) string {
	s := strings.Index(string(d), "m=audio")
	e := len(d)
	return string(d[s:e])
}

func ParseSdp(d []byte) (*Sdp, error) {
	var err error
	sdp := &Sdp{}
	sdp.RawSdp = d

	//回话段, 视频段, 音频段
	//sSeg := GetSessionSegment(d)
	vSeg := GetVideoSegment(d)
	aSeg := GetAudioSegment(d)

	p := strings.Split(vSeg, "\r\n")
	for i := 0; i < len(p); i++ {
		if strings.Contains(p[i], "m=video") {
			ss := strings.Split(p[i], " ")
			sdp.VideoPayloadTypeInt, _ = strconv.Atoi(ss[3])
		}
		if strings.Contains(p[i], "a=rtpmap") {
			s := strings.Split(p[i], " ")
			ss := strings.Split(s[1], "/")
			sdp.VideoPayloadTypeStr = ss[0]
			sdp.VideoClockRate, _ = strconv.Atoi(ss[1])
		}
		if strings.Contains(p[i], "sprop-parameter-sets=") {
			//log.Printf("%s", p[i])
			str := strings.TrimSpace(p[i])
			s := strings.Split(str, ";")
			for i := 0; i < len(s); i++ {
				if strings.Contains(s[i], "sprop-parameter-sets=") {
					ss := strings.Split(s[i], ",")
					b := []byte(ss[0])
					sdp.SpsBase64 = string(b[len("sprop-parameter-sets=")+1:])
					sdp.PpsBase64 = ss[1]
					//log.Printf("spsBase64=%s, ppsBase64=%s", sdp.SpsBase64, sdp.PpsBase64)
					sdp.SpsData, _ = base64.StdEncoding.DecodeString(sdp.SpsBase64)
					sdp.PpsData, _ = base64.StdEncoding.DecodeString(sdp.PpsBase64)
					//log.Printf("sps=%x, pps=%x", sdp.SpsData, sdp.PpsData)

					sdp.Sps, err = SpsParse(sdp.SpsData)
					if err != nil {
						//log.Println(err)
						return nil, err
					}
					//log.Printf("%#v", sdp.Sps)
					sdp.Width = int((sdp.Sps.PicWidthInMbsMinus1 + 1) * 16)
					sdp.Height = int((sdp.Sps.PicHeightInMapUnitsMinus1 + 1) * 16)
					//log.Printf("video width=%d, height=%d", sdp.Width, sdp.Height)
				}
				if strings.Contains(s[i], "profile-level=") {
				}
			}
		}
		if strings.Contains(p[i], "a=control") {
			s := strings.Split(p[i], ":")
			sdp.VideoAControl = s[1]
		}
	}

	p = strings.Split(aSeg, "\r\n")
	for i := 0; i < len(p); i++ {
		if strings.Contains(p[i], "m=audio") {
			ss := strings.Split(p[i], " ")
			sdp.AudioPayloadTypeInt, _ = strconv.Atoi(ss[3])
		}
		if strings.Contains(p[i], "a=rtpmap") {
			s := strings.Split(p[i], " ")
			ss := strings.Split(s[1], "/")
			sdp.AudioPayloadTypeStr = ss[0]
			sdp.AudioClockRate, _ = strconv.Atoi(ss[1])
			sdp.AudioChannelNum, _ = strconv.Atoi(ss[2])
		}
		if strings.Contains(p[i], "config=") {
			//log.Printf("%s", p[i])
			str := strings.TrimSpace(p[i])
			s := strings.Split(str, ";")
			for i := 0; i < len(s); i++ {
				if strings.Contains(s[i], "profile-level-id=") {
					ss := strings.Split(s[i], "=")
					sdp.AudioProfileId, _ = strconv.Atoi(ss[1])
				}
				if strings.Contains(s[i], "config=") {
					ss := strings.Split(s[i], "=")
					b := []byte(ss[1])
					b = b[:4]
					asc, _ := strconv.ParseInt(string(b), 16, 64)
					//log.Printf("asc=%x(%d)", asc, asc)

					AacC := &AudioSpecificConfig{}
					AacC.ObjectType = uint8((asc & 0xf800) >> 11)     // 5bit
					AacC.SamplingIdx = uint8((asc & 0x0780) >> 7)     // 4bit
					AacC.ChannelNum = uint8((asc & 0x0078) >> 3)      // 4bit
					AacC.FrameLenFlag = uint8((asc & 0x4) >> 2)       // 1bit
					AacC.DependCoreCoder = uint8((asc & 0x0002) >> 1) // 1bit
					AacC.ExtensionFlag = uint8((asc & 0x0001))        // 1bit
					//log.Printf("%#v", AacC)
					sdp.AacC = AacC
					sdp.AacCData = Uint16ToByte(uint16(asc), nil, BE)
				}
			}
		}
		if strings.Contains(p[i], "a=control") {
			s := strings.Split(p[i], ":")
			sdp.AudioAControl = s[1]
		}
	}
	return sdp, nil
}

func CreateSdpUseSpsPps(sps, pps []byte) (*Sdp, error) {
	sdp := &Sdp{}
	sdp.RawSdp = nil
	sdp.NetProtocol = "tcp"

	sdp.VideoPayloadTypeInt = 96
	sdp.VideoPayloadTypeStr = "h264"
	sdp.VideoClockRate = 90000
	sdp.VideoAControl = "streamid=0"

	sdp.AudioPayloadTypeInt = 97
	sdp.AudioPayloadTypeStr = "aac"
	sdp.AudioClockRate = 11025
	sdp.AudioChannelNum = 1
	sdp.AudioAControl = "streamid=1"
	sdp.AudioProfileId = 0

	sdp.AacC = &AudioSpecificConfig{}
	sdp.AacC.ObjectType = 2
	sdp.AacC.SamplingIdx = 10
	sdp.AacC.ChannelNum = 1
	sdp.AacC.FrameLenFlag = 0
	sdp.AacC.DependCoreCoder = 0
	sdp.AacC.ExtensionFlag = 0
	d := make([]byte, 2)
	d[0] = (sdp.AacC.ObjectType&0x1f)<<3 | (sdp.AacC.SamplingIdx&0xf)>>1
	d[1] = (sdp.AacC.SamplingIdx&0xf)<<7 | (sdp.AacC.ChannelNum&0xf)<<3 | (sdp.AacC.FrameLenFlag&0x1)<<2 | (sdp.AacC.DependCoreCoder&0x1)<<1 | sdp.AacC.ExtensionFlag&0x1
	sdp.AacCData = d

	sdp.SpsBase64 = base64.StdEncoding.EncodeToString(sps)
	sdp.PpsBase64 = base64.StdEncoding.EncodeToString(pps)
	sdp.SpsData = sps
	sdp.PpsData = pps

	var err error
	sdp.Sps, err = SpsParse(sdp.SpsData)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sdp.Width = int((sdp.Sps.PicWidthInMbsMinus1 + 1) * 16)
	sdp.Height = int((sdp.Sps.PicHeightInMapUnitsMinus1 + 1) * 16)

	si := &SdpInfo{}
	//si.Oip = rs.Puber.LIp
	//si.Cip = rs.Puber.RIp
	si.Oip = "127.0.0.1"
	si.Cip = conf.IpOuter
	si.App = AppName
	si.Tool = AppName
	si.Sps = sdp.SpsBase64
	si.Pps = sdp.PpsBase64
	si.Vsc = sdp.SpsData[1:4]
	si.Asr = sdp.AudioClockRate
	si.Acn = sdp.AudioChannelNum
	si.Asc = sdp.AacCData
	s, err := CreateSdp(si)
	sdp.RawSdp = []byte(s)
	return sdp, nil
}
