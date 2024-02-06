package main

import (
	"container/list"
	"log"
	"net"
	"os"
	"strings"
	"sync"
)

type RtpUdpPkt struct {
	Ip   string
	Port int
	Data []byte
	Len  int
}

type RtpPkgQueue struct {
	Ssrc           uint32
	NeedTs         uint32
	NeedSeq        uint16
	SendSeq        uint16
	NeedSeqWaitNum uint8
	PkgMap         sync.Map
	PkgMapMinSeq   uint16
	PkgMapMaxSeq   uint16
}

type RtspStream struct {
	Key      string //PullIp_PullPort_PullArgs
	UrlArgs  UrlArgs
	Conn     net.Conn
	Session  string //66334873
	StreamId string
	IsPuber  bool
	Sdp      *Sdp
	HsRsps   *RtspHsRsps

	LogFn string      //文件名
	LogFp *os.File    //需要Close()
	log   *log.Logger //

	LAddr         string
	LIp           string
	LPort         string
	RAddr         string
	RIp           string
	RPort         string
	NetProtocol   string //tcp or udp
	IsInterleaved bool

	VideoRtpChanId  int
	VideoRtcpChanId int
	AudioRtpChanId  int
	AudioRtcpChanId int

	VideoRtpUdpPort  int
	VideoRtcpUdpPort int
	AudioRtpUdpPort  int
	AudioRtcpUdpPort int

	VideoRtpPkgs   *RtpPkgQueue
	AudioRtpPkgs   *RtpPkgQueue
	Rtp2RtspChan   chan *RtpPacket
	Rtp2RtmpChan   chan *RtpPacket
	AvPkt2RtspChan chan *AvPacket
	AvPkt2RtmpChan chan *AvPacket
	RtpUdpChan     chan *RtpUdpPkt

	SeiData []byte
	SpsData []byte
	PpsData []byte
	Sps     *Sps
	Width   int
	Height  int

	RtmpMetaData       []byte
	RtmpVideoSeqHeader []byte
	RtmpAudioSeqHeader []byte
	RtmpVideoIframePre [5]byte //5Byte
	RtmpVideoPframePre [5]byte //5Byte
	RtmpAudioAacPre    [2]byte //2Byte

	//以下用于拉rtsp流
	Rqst *RtspRqst
	Stop bool

	//以下用于rtsp播放
	Puber          *RtspStream
	Players        sync.Map //map[string]*RtspStream
	NewPlayer      bool
	RtpSpsPpsPkt   *RtpPacket //缓存RtpSpsPps包, 要更新seq和timestamp
	RtpGopCache    *list.List //缓存至少一组gop的rtp包(含音频), 用于rtsp快速启播; 双向链表, 写时候不能读 除非加锁;
	RtpGopCacheNum int
	RtpGopAvPkgNum int
}

func NewRtspStream(c net.Conn) *RtspStream {
	s := &RtspStream{}
	if c != nil {
		s.Conn = c
	}
	s.Session = "66334873"
	s.IsPuber = false

	if c != nil {
		s.LAddr = s.Conn.LocalAddr().String()
		sa := strings.Split(s.LAddr, ":")
		s.LIp = sa[0]
		s.LPort = sa[1]
		s.RAddr = s.Conn.RemoteAddr().String()
		sa = strings.Split(s.RAddr, ":")
		s.RIp = sa[0]
		s.RPort = sa[1]
	}

	s.VideoRtpPkgs = &RtpPkgQueue{}
	s.AudioRtpPkgs = &RtpPkgQueue{}
	s.Rtp2RtspChan = make(chan *RtpPacket, conf.Rtsp.Rtp2RtspChanNum)
	s.Rtp2RtmpChan = make(chan *RtpPacket, conf.Rtsp.Rtp2RtmpChanNum)
	s.AvPkt2RtspChan = make(chan *AvPacket, conf.Rtsp.AvPkt2RtspChanNum)
	s.AvPkt2RtmpChan = make(chan *AvPacket, conf.Rtsp.AvPkt2RtmpChanNum)
	s.RtpUdpChan = make(chan *RtpUdpPkt, conf.Rtsp.Rtp2RtspChanNum)
	s.RtpGopCache = list.New()
	s.HsRsps = &RtspHsRsps{}

	s.VideoRtpChanId = 0
	s.VideoRtcpChanId = 1
	s.AudioRtpChanId = 2
	s.AudioRtcpChanId = 3

	var err error
	s.LogFn = GetTempLogFn("rtsp")
	s.log, s.LogFp, err = StreamLogCreate(s.LogFn)
	if err != nil {
		log.Println(err)
		return nil
	}
	return s
}
