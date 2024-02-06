package main

import (
	"bufio"
	"container/list"
	"context"
	"fmt"
	"livegateway/utils"
	"log"
	"net"
	"os"
	"strings"
	"sync"
)

const (
	MsgTypeIdSetChunkSize     = 1  //默认128byte, 最大16777215(0xFFFFFF)
	MsgTypeIdAbort            = 2  //终止消息
	MsgTypeIdAck              = 3  //回执消息
	MsgTypeIdUserControl      = 4  //用户控制消息
	MsgTypeIdWindowAckSize    = 5  //窗口大小
	MsgTypeIdSetPeerBandwidth = 6  //设置对端带宽
	MsgTypeIdAudio            = 8  //音频消息
	MsgTypeIdVideo            = 9  //视频消息
	MsgTypeIdDataAmf3         = 15 //AMF3数据消息
	MsgTypeIdDataAmf0         = 18 //AMF0数据消息
	MsgTypeIdShareAmf3        = 16 //AMF3共享对象消息
	MsgTypeIdShareAmf0        = 19 //AMF0共享对象消息
	MsgTypeIdCmdAmf3          = 17 //AMF3命令消息
	MsgTypeIdCmdAmf0          = 20 //AMF0命令消息
)

var (
	//Publishers      sync.Map    // map[string]*RtmpStream, key:App_StreamId
	TsInfoChan      chan TsInfo // 用户发布tsInfo给mqtt
	Devices         sync.Map    // map[string]*Device, key:App_StreamId
	DevicesSsrc     sync.Map    // map[string]*Device, key:ssrc
	goplocks        sync.Mutex
	PlayLocks       sync.Mutex
	VideoKeyFrame   sync.Map
	PlayerMap       sync.Map
	StreamStateChan chan StreamStateInfo //send stream and stat to cc
	AdjustSeqNum    uint32
)

//StreamType取值
//RtmpPusher	我们推流给别人, RtmpPuller	我们拉别人的流
//RtmpPublisher	别人推流给我们, RtmpPlayer	别人拉我们的流
type RtmpStream struct {
	Key        string //app_streamid
	StreamType string

	LogFn     string      //临时或正式的日志名, Stream_Timestamp.log
	LogFp     *os.File    //不用的时候 需要调用Close()
	log       *log.Logger //每个流的请求(发布/播放) 都是独立的
	LogCutoff bool        //日志要按天分割

	IsPublisher bool        // true为发布者，false为播放者
	PubAuth     PubAuthRsps // 业务逻辑 推流鉴权结果
	GbPublisher
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

	RtpChan        chan RtpPacket
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

func NewRtmpStream(c net.Conn) (s *RtmpStream, err error) {
	s = &RtmpStream{
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
		GopCache:            GopCacheNew(),
		MediaData:           list.New(),
	}
	s.AvPkg2RtspChan = make(chan Chunk, conf.Rtmp.AvPkt2RtspChanNum)
	s.PlaybackTimeout = 20

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
