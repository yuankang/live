package main

import (
	"bufio"
	"container/list"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"utils"
)

//StreamType取值
//RtmpPub	对方rtmp推流, 我们是代理发布, 是一个stream
//RtmpPush	我们推流给第三方, 是一个stream
//RtmpPull	我们从第三方拉流, 是一个stream
//RtmpPlay  对方rtmp播放, 我们是服务端, 是一个stream
//GbPub		对方gb28181rtp推流, 我们是代理发布者, 是一个stream
//GbPush	级联给上级
//GbPull	无
//GbPlay	对方gb28281播放, 我们是服务端, 是一个stream
//RtspPub	对方rtmp推流, 我们是代理发布, 是一个stream
//RtspPush	我们推流给第三方, 是一个stream
//RtspPull	我们从第三方拉流, 是一个stream
//RtspPlay	对方rtsp播放, 我们是服务端, 是一个stream
//FlvPub	无
//FlvPush	无
//FlvPull	我们拉第三方flv直播流
//FlvPlay	对方flv播放, 我们是服务端, 是一个stream
//HlsPub	我们生产直播m3u8和ts, 是一个stream
//HlsPush	无
//HlsPull	我们拉第三方hls直播流
//HlsPlay	对方hls播放, 我们是服务端, 是一个stream
//RecHls	我们生产录制m3u8和ts, 是一个stream
//RecMp4    我们生产录制mp4, 是一个stream

//RtmpPusher	我们推流给别人, RtmpPuller	我们拉别人的流
//RtmpPublisher	别人推流给我们, RtmpPlayer	别人拉我们的流

/*
AudioCodecType string //国标GB28181中的定义可以知道
1 MPEG-4视频流	0x10
2 H.264视频流	0x1B
  H.265			0x24 ???
3 SVAC视频流	0x80
4 G.711音频流	0x90
5 G.722.1音频流 0x92
6 G.723.1音频流 0x93
7 G.729音频流	0x99
  AAC			0x??
8 SVAC音频流	0x9B
*/

//接入流不管什么协议 都以rtmp协议缓存, rtmp协议转出rtmp/flv/hls/gb28181/rtsp
type Stream struct {
	Key  string //app_streamid
	Type string //stream type

	LogFn     string      //临时或正式的日志名, Stream_Timestamp.log
	LogFp     *os.File    //不用的时候 需要调用Close()
	log       *log.Logger //每个流的请求(发布/播放) 都是独立的
	LogCutoff bool        //日志要按天分割

	IsPublisher bool        // true为发布者，false为播放者
	PubAuth     PubAuthRsps // 业务逻辑 推流鉴权结果
	GbPub
	RtmpPublisher
	Players   sync.Map //发布者才会有播放者
	PlayClose bool     //播放者是否已断开连接

	RemoteAddr string
	RemoteIp   string
	RemotePort string
	//RemoteConn      net.Conn          //需要Close()
	//BufioConn       *bufio.ReadWriter //有缓存
	Conn0           net.Conn          //需要Close()
	Conn            *bufio.ReadWriter //有缓存
	PlaybackTimeout int               // 默认5秒, 摄像头本地回看超时时间
	TrafficInfo                       //用于网络流量统计

	ChunkSize           uint32 //接收数据
	WindowAckSize       uint32
	RemoteChunkSize     uint32 //发送数据
	RemoteWindowAckSize uint32
	RemotePeerBandwidth uint32
	Chunks              map[uint32]Chunk
	AmfInfo             AmfInfo
	MessageHandleDone   bool   //
	RecvMsgLen          uint32 //用于ACK回应,接收消息的总长度(不包括ChunkHeader)
	TransmitSwitch      string //

	PsPktChan      chan *PsPacket
	RtpChan        chan *RtpPacket
	RtpRecChan     chan RtpPacket
	FrameChan      chan Chunk //每个播放者一个
	AvPkg2RtspChan chan Chunk

	VideoCodecType string // "H264" or "H265"
	AudioCodecType string // "AAC" or "G711a"
	Width          int
	Height         int

	FlvPlayBackDoor     bool //flv不加密播放 方便自测
	FlvPlayEncrypt      bool //flv play, encrypt = 0 no encrypt
	NewPlayer           bool // player use, 新来的播放者要先发GopCache
	Msg2RtmpChan        chan Chunk
	PlayChan            chan *Chunk // 每个播放者一个, 容量配置项指定
	HlsChan             chan Chunk  // 发布者和hls生产者的数据通道
	HlsLiveChan         chan Chunk  // 发布者和hls live生产者的数据通道
	PlayStartMode       string
	PlaySendAHeaderFlag bool   // 低延时启播发送音频头
	PlaySendVHeaderFlag bool   // 低延时启播发送视频头
	HlsAddDiscFlag      bool   //
	HlsLiveAddDiscFlag  bool   //
	FlvSendDataSize     uint32 //play_flv_xxx.log, 每发送1MB数据打一条日志

	Ctx    context.Context
	Wg     sync.WaitGroup
	Cancel context.CancelFunc

	RecordFlag bool   //是否录制原始流, 每个流独立控制, 配置文件还有总开关
	RecRtmpFn  string //保存发送的rtmp数据

	/////////////////////////////////////
	CountNum        uint32 //值用于触发GopCache清理Gop
	FirstVideoTs    uint32 //视频首帧时间戳
	FirstAudioTs    uint32 //音频首帧时间戳
	FirstDifValue   uint32 //音视频首帧时间差, |FirstVideoTs - FirstAudioTs|
	PrevVideoTs     uint32 //视频上一帧时间戳, 用于计算帧间隔
	PrevAudioTs     uint32 //音频上一帧时间戳, 用于计算帧间隔
	VideoTsDifValue uint32 //视频帧间隔, 帧率为25时 是40ms
	AudioTsDifValue uint32 //音频帧间隔, xxx
	VideoFps        uint32
	AudioFps        uint32
	GopNum          uint32 //10个gop 统计一次码率
	GopStartTs      int64  //second
	GopEndTs        int64  //second
	GopBitrate      uint32 //kbps
	GopDataSize     uint32 //byte
	MediaData       *list.List
	NaluNum         uint32
	GopCache
	HlsInfo
	TsMaxCutSize     uint32
	TsLiveMaxCutSize uint32
	AudioChunk       Chunk
	AudioLiveChunk   Chunk
	AacC             *AudioSpecificConfig
	AvcC             *AVCDecoderConfigurationRecord  // h264 header
	HevcC            *HEVCDecoderConfigurationRecord // h265 header
	PktNum           uint32
	StartTimeVideo   int64
	PktNumVideo      uint32
	CalNumVideo      uint32
	SeqNumVideo      uint32
	TotalVideoDelta  uint32
	FirstVideoAdust  uint32
	PrevVideoAdust   uint32
	DeltaCalVideo    [3]uint32
	StartTimeAudio   int64
	PktNumAudio      uint32
	CalNumAudio      uint32
	SeqNumAudio      uint32
	TotalAudioDelta  uint32
	FirstAudioAdust  uint32
	PrevAudioAdust   uint32
	DeltaCalAudio    [3]uint32
}

func NewStream(c net.Conn) (s *Stream, err error) {
	s = &Stream{
		LogFn:               GetTempLogFn("rtmp"),
		ChunkSize:           128,
		WindowAckSize:       2500000,
		RemoteChunkSize:     128,
		RemoteWindowAckSize: 2500000,
		RemotePeerBandwidth: 2500000,
		Chunks:              make(map[uint32]Chunk),
		NewPlayer:           true,
		Msg2RtmpChan:        make(chan Chunk, conf.Rtmp.Msg2RtmpChanNum),
		HlsChan:             make(chan Chunk, conf.HlsRec.HlsStockMax),
		HlsLiveChan:         make(chan Chunk, conf.HlsRec.HlsStockMax),
		PsPktChan:           make(chan *PsPacket, 1000),
		GopCache:            GopCacheNew(),
		MediaData:           list.New(),
	}
	s.AvPkg2RtspChan = make(chan Chunk, conf.Rtmp.AvPkt2RtspChanNum)
	s.PlaybackTimeout = 20
	s.RtpPktCtTs = -1

	if c != nil {
		//把c转换为有缓存的io, 调用Flush()才会及时发送数据
		//8KB = 1024 * 8 = 8192
		bs := 8192
		nr := bufio.NewReaderSize(c, bs)
		nw := bufio.NewWriterSize(c, bs)
		nrw := bufio.NewReadWriter(nr, nw)

		ra := c.RemoteAddr().String()
		ip := strings.Split(ra, ":")

		s.Conn0 = c
		s.Conn = nrw
		s.RemoteAddr = ra
		s.RemoteIp = ip[0]
	}

	//s.LogFn = fmt.Sprintf("%s/%s/push_rtmp_%s.log", conf.Log.StreamLogPath, s.StreamId, s.RemoteAddr)
	//磁盘满时, 日志创建失败, 没做判断 继续执行 会导致打印日志时崩溃
	s.log, s.LogFp, err = StreamLogCreate(s.LogFn)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if conf.StreamRec.Enable == true && strings.Contains(s.StreamId, conf.StreamRec.StreamId) {
		s.RecRtmpFn = fmt.Sprintf("%s/%s_%d_rtmp.rec", conf.StreamRec.SavePath, s.StreamId, utils.GetTimestamp("s"))
		s.log.Printf("RecFile:%s", s.RecRtmpFn)
	}
	return s, err
}
