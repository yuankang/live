package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	utils "utilsGIT"
)

/*************************************************/
/* RtmpPlayer 别人拉我们的流
/*************************************************/
func RtmpTransmit(p *Stream, s *Stream) {
	defer p.Wg.Done()
	var c *Chunk
	var ok bool
	var err error

	for {
		select {
		case c, ok = <-s.PlayChan:
			if ok == false {
				s.log.Printf("%s RtmpTransmit stop", s.Key)
				return
			}
		case <-p.Ctx.Done():
			p.log.Printf("publish stop then rtmp play stop")
			return
		}
		//发送数据给播放器
		err = MessageSplit(s, c, false)
		if err != nil {
			s.log.Println(err)
			if strings.Contains(err.Error(), "error: chunk point is nil") {
				continue
			}
			//FIXME: 要确定 是不是 timeout这个关键字
			if strings.Contains(err.Error(), "timeout") {
				continue
			}
			//自己退出后要通知发送者
			s.PlayClose = true
			return
		}
		//s.log.Printf("SendData, type:%s, size:%d", c.DataType, c.MsgLength)
	}
}

//拉流者网络有好有坏, 某个拉流者阻塞不能影响别的
//每个拉流者一个发送协程, 哪个发送者有积压数据就扔掉
func RtmpPlayer(s *Stream) {
	//key := fmt.Sprintf("%s_%s", s.AmfInfo.App, s.AmfInfo.StreamId)
	key := s.AmfInfo.StreamId
	s.log.Println("publisher key is", key)

	info := fmt.Sprintf("app=%s, sid=%s, can't be empty", s.AmfInfo.App, s.AmfInfo.StreamId)
	if s.AmfInfo.App == "" || s.AmfInfo.StreamId == "" {
		log.Printf(info)
		s.log.Printf(info)
		RtmpStop(s)
		return
	}

	info = fmt.Sprintf("publisher %s isn't exist", key)
	v, ok := RtmpPuberMap.Load(key)
	if ok == false { // 发布者不存在, 断开连接并返回错误
		log.Printf(info)
		s.log.Printf(info)
		RtmpStop(s)
		return
	}
	p, _ := v.(*Stream)
	p.Wg.Add(1)
	//先启动播放转发协程, 再添加到发布者的players里
	s.PlayChan = make(chan *Chunk, conf.PlayStockMax)
	// TODO: 网络io写设置4秒超时
	go RtmpTransmit(p, s)
	s.log.Printf("%s RtmpTransmit() start", s.AmfInfo.StreamId)

	s.Key = fmt.Sprintf("%s_%s_%s", s.AmfInfo.App, s.AmfInfo.StreamId, s.RemoteAddr)
	s.log.Println("player key is", s.Key)
	p.Players.Store(s.Key, s)
}

/*************************************************/
/* RtmpPublisher 别人推流给我们
/*************************************************/
func HeadDataSend(s, p *Stream) {
	switch p.Type {
	case "rtmpPlayer":
		MetaSendRtmp(s, p)
	case "flvPlayer":
		MetaSendFlv(s, p)
	}
}

//s是发布者, p是播放者
func MessageSendRtmp(s, p *Stream, c Chunk) error {
	//发送数据给播放者
	p.PlayChan <- &c

	//为了 play_rtmp_xxx.log 能有周期性打印
	p.FlvSendDataSize += c.MsgLength
	if p.FlvSendDataSize >= conf.Flv.FlvSendDataSize {
		p.log.Printf("send data %d(2MB) + %d byte", conf.Flv.FlvSendDataSize, p.FlvSendDataSize-conf.Flv.FlvSendDataSize)
		p.FlvSendDataSize = 0
	}

	//rtmp播放网络流量暂不用统计和上报, 因为暂不提供rtmp播放地址给用户
	//support rtmp play, so report datasize
	//return nil

	if conf.Nt.Enable == false {
		return nil
	}

	//http-flv播放网络流量统计和上报
	//这么处理duraton值会浮动, 如果想恒定就要用定时器
	cTime := utils.GetTimestamp("ms")
	if p.StartTime == 0 {
		p.StartTime = cTime
	}
	p.Duration = cTime - p.StartTime
	if p.Duration >= conf.Nt.Interval {
		go TrafficReport(p, p.TrafficInfo, "play", "rtmp")
		p.StartTime = cTime
		p.DataSize = 0
	} else {
		//FIXME: 只算c.MsgLength, 其实是少算了一些字节
		p.DataSize += c.MsgLength
	}
	//p.log.Printf("%#v", p.TrafficInfo)
	return nil
}

//s: server, p: player
func LiveDataSend(s, p *Stream, c Chunk) {
	n := len(p.PlayChan)
	//播放积压已经到最大值, 这时音视频数据都不发送
	//flv播放器不正常关闭, 导致PlayChanNum=600日志打印太多，增加ticker解决此问题
	//如果持续阻塞800次发送(约30秒), 服务器主动断开播放器的连接
	if n == conf.PlayStockMax {
		s.log.Printf("%s PlayChanNum=%d(%d), stop send data", p.Key, n, conf.PlayStockMax)
		p.PlayClose = true

		return
	}

	if p.PlayStartMode == "low" {
		if p.PlaySendAHeaderFlag == false && c.DataType == "AudioAacFrame" {
			p.log.Println("<== low latency send AudioHeader", c.DataType)
			v, ok := s.GopCache.AudioHeader.Load(s.Key)
			if ok == false {
				s.log.Printf("audio header is not exist %v", p.Key)
			} else {
				v1 := v.(*Chunk)
				v1.Timestamp = c.Timestamp
				switch p.Type {
				case "rtmpPlayer":
					p.PlayChan <- v1
				case "flvPlayer":
					err := FlvSendAudioHead(s, p, *v1)
					if err != nil {
						return
					}
				}
				p.PlaySendAHeaderFlag = true
			}
		}
		if p.PlaySendVHeaderFlag == false {
			if c.DataType == "VideoKeyFrame" {
				p.log.Println("<== low latency send VideoHeader", c.DataType)
				v, ok := s.GopCache.VideoHeader.Load(s.Key)
				if ok == false {
					s.log.Printf("video header is not exist %v", s.Key)
				} else {
					v1 := v.(*Chunk)
					v1.Timestamp = c.Timestamp
					switch p.Type {
					case "rtmpPlayer":
						p.PlayChan <- v1
					case "flvPlayer":
						err := FlvSendVideoHead(s, p, *v1)
						if err != nil {
							return
						}
					}
					p.PlaySendVHeaderFlag = true
				}
			} else if c.DataType == "VideoInterFrame" {
				//p.log.Println("<== low latency no send", c.DataType)
				return
			}
		}
	}

	switch p.Type {
	case "rtmpPlayer":
		MessageSendRtmp(s, p, c)
	case "flvPlayer":
		MessageSendFlv(s, p, c)
	}
}

//启播方式: 默认采用快速启播
//1 快速启播: 先发送缓存的gop数据, 再发送最新数据. 启播快 但延时交高
//2 低延时启播: 直接发送最新数据. 启播交慢 但是延时最低
func RtmpSender(s *Stream) {
	var ok bool
	var n int
	var p *Stream
	var err error
	td := time.Duration(s.PlaybackTimeout)
	ticker := time.NewTicker(td * time.Second)
	defer ticker.Stop()

	i := 0
	for {
		//必须在for循环里申请c, 否则GopCache.MediaData循环打印的值相同
		//必须在for循环里申请c, 否则GopCache.MediaData循环打印的值相同
		var c Chunk
		s.log.Printf("----> RtmpSender message %d", i)

		ticker.Reset(td * time.Second)
		select {
		case c, ok = <-s.DataChan:
			if ok == false {
				s.log.Printf("%s RtmpSender stop", s.Key)
				return
			}
		case <-ticker.C:
			s.log.Printf("rtmp recv timeout %d second", td)
			s.TransmitSwitch = "off"
			return
		}

		switch c.MsgTypeId {
		case MsgTypeIdCmdAmf0: // 20
			err = AmfHandle(s, &c)
			//TODO: maybe give response
			continue
		case MsgTypeIdAudio: // 8
			//s.log.Printf("audio timestamp=%d", c.Timestamp)
			err = AudioHandle(s, &c)
		case MsgTypeIdVideo: // 9
			//s.log.Printf("video timestamp=%d", c.Timestamp)
			err = VideoHandle(s, &c)
		case MsgTypeIdDataAmf3, MsgTypeIdDataAmf0: // 15 18
			err = MetadataHandle(s, &c)
		case MsgTypeIdUserControl: //用户控制消息
			s.log.Printf("recv MsgTypeId=%d UserControlMsg", c.MsgTypeId)
			err = nil
		default:
			//s.log.Printf("undefined msg=%#v", c)
			err = fmt.Errorf("undefined MsgTypeId=%d", c.MsgTypeId)
		}
		s.log.Printf("%d, xxx000", i)

		if err != nil {
			s.log.Println(err)
			continue
		}
		//s.log.Printf("Message TypeId %d, len %d", c.MsgTypeId, c.MsgLength)
		//s.log.Printf("%x", c.MsgData)
		//10个Gop计算一次发布者的发送码率
		//CalcGopBitrate(s, c)

		if s.TransmitSwitch == "off" {
			s.log.Printf("%s RtmpPublisher stop", s.Key)
			continue
		}

		if conf.HlsRec.Enable == true {
			n = len(s.HlsChan)
			if n < conf.HlsRec.HlsStockMax { //发送数据给hls生产协程
				s.HlsChan <- c
			} else { //硬盘io异常会导致阻塞
				s.log.Printf("HlsChanNum=%d(%d), DropDataType=%s", n, conf.HlsRec.HlsStockMax, c.DataType)
			}
		}
		if conf.HlsLive.Enable == true {
			n = len(s.HlsLiveChan)
			if n < conf.HlsRec.HlsStockMax { //发送数据给hls生产协程
				s.HlsLiveChan <- c
			} else { //硬盘io异常会导致阻塞
				s.log.Printf("HlsLiveChanNum=%d(%d), DropDataType=%s", n, conf.HlsRec.HlsStockMax, c.DataType)
			}
		}
		s.log.Printf("%d, xxx111", i)

		//轮询发送数据给所有播放者 通过每个播放者的chan
		//播放者到播放器的网络差 也不会引起这个循环阻塞
		//详细说明见 RtmpTransmit()
		s.Players.Range(func(k, v interface{}) bool {
			p, _ = v.(*Stream)
			//s.log.Printf("<== send data to %s, %d", p.Key, i)
			if i%1000 == 0 {
				s.log.Printf("<== send data to %s, dataType=%s", p.Key, c.DataType)
				i = 0
			}

			if p.PlayClose == true {
				s.log.Printf("<== player %s is stop", p.Key)
				RtmpStop(p)
				s.Players.Delete(p.Key)
				return true
			}

			//新播放者, 先发送缓存的gop数据, 再发送实时数据
			if p.NewPlayer == true {
				s.log.Printf("<== player %s is NewPlayer", p.Key)
				p.NewPlayer = false
				if p.PlayStartMode == "low" {
					HeadDataSend(s, p)
				} else if p.PlayStartMode == "fastlow" {
					GopCacheFastlowSend(s, p)
				} else {
					GopCacheSend(s, p)
				}
			} else {
				LiveDataSend(s, p, c)
			}
			return true
		})
		s.log.Printf("%d, xxx222", i)
		i++
	}
}

//接收(合并)数据 并 传递数据给发送者
func RtmpReceiver(s *Stream) error {
	c, err := MessageMerge(s, nil)
	if err != nil {
		s.log.Println(err)
		s.log.Println("RtmpReceiver close")
		return err
	}

	err = SendAckMessage(s, c.MsgLength)
	if err != nil {
		s.log.Println(err)
		return err
	}

	//s.log.Printf("DataChanNum=%d(%d)", len(s.DataChan), conf.DataStockMax)
	if len(s.DataChan) < conf.DataStockMax {
		s.DataChan <- c
	} else {
		s.log.Printf("DataChanNum=%d(%d)", len(s.DataChan), conf.DataStockMax)
	}
	return nil
}

type RtmpPublisher struct {
	VpsData   []byte //在???
	SpsData   []byte //在AVCDecoderConfigurationRecord
	PpsData   []byte //在AVCDecoderConfigurationRecord
	SeiData   []byte //nalu(sei)+nalu(iframe), sei可以不发给sv
	VpsChange bool
	SpsChange bool
	PpsChange bool
	SeiChange bool
	AvcSH     []byte // video sequence header

	GopCache
	FrameNum int

	ChunkSize           uint32 //接收数据
	WindowAckSize       uint32
	RemoteChunkSize     uint32 //发送数据
	RemoteWindowAckSize uint32
	RemotePeerBandwidth uint32

	AmfInfo           AmfInfo
	Chunks            map[uint32]Chunk
	PlaybackTimeout   int         // 默认5秒, 摄像头本地回看超时时间
	IsPublisher       bool        // true为发布者，false为播放者
	PubAuth           PubAuthRsps // 业务逻辑 推流鉴权结果
	MessageHandleDone bool        //
	RecvMsgLen        uint32      // 用于ACK回应,接收消息的总长度(不包括ChunkHeader)
	TransmitSwitch    string      //
	PlayClose         bool        //播放者是否已断开连接
	PlayStock         bool        //播放是否积压数据
	FlvPlayBackDoor   bool        //flv不加密播放 方便自测
	NewPlayer         bool        // player use, 新来的播放者要先发GopCache
	DataChan          chan Chunk  // 发布者和播放者的数据通道
	PlayChan          chan Chunk  // 每个播放者一个, 容量配置项指定
	HlsChan           chan Chunk  // 发布者和hls生产者的数据通道
	PlaySendBlockNum  int         //给播放器发送帧数据, 阻塞次数
	HlsAddDiscFlag    bool        //
	FlvSendDataSize   uint32      //play_flv_xxx.log, 每发送1MB数据打一条日志
	FirstVideoTs      uint32      //视频首帧时间戳
	FirstAudioTs      uint32      //音频首帧时间戳
	FirstDifValue     uint32      //音视频首帧时间差, |FirstVideoTs - FirstAudioTs|
	PrevVideoTs       uint32      //视频上一帧时间戳, 用于计算帧间隔
	PrevAudioTs       uint32      //音频上一帧时间戳, 用于计算帧间隔
	VideoTsDifValue   uint32      //视频帧间隔, 帧率为25时 是40ms
	AudioTsDifValue   uint32      //音频帧间隔, xxx
	VideoFps          uint32
	AudioFps          uint32
	GopNum            uint32 //10个gop 统计一次码率
	GopStartTs        int64  //second
	GopEndTs          int64  //second
	GopBitrate        uint32 //kbps
	GopDataSize       uint32 //byte
	NaluNum           uint32
	//HlsInfo
	TsMaxCutSize uint32
	AudioChunk   Chunk
	AvcC         AVCDecoderConfigurationRecord  // h264 header
	HevcC        HEVCDecoderConfigurationRecord // h265 header
}

func RtmpPublisher0(s *Stream) {
	//s.Key = fmt.Sprintf("%s_%s", s.AmfInfo.App, s.AmfInfo.StreamId)
	s.Key = s.AmfInfo.StreamId
	s.log.Println("publisher key is", s.Key)

	_, ok := RtmpPuberMap.Load(s.Key)
	if ok == true { // 发布者已存在, 断开当前连接并返回错误
		s.log.Printf("publisher %s is exist", s.Key)
		RtmpPublishStop1(s)
		return
	}
	if conf.HlsRec.HlsStoreUse == "Disk" {
		s.HlsStorePath = conf.HlsRec.DiskPath
	} else if conf.HlsRec.HlsStoreUse == "Mem" {
		s.HlsStorePath = conf.HlsRec.MemPath
	} else {
		s.HlsStorePath = conf.HlsRec.MemPath
	}
	s.Ctx, s.Cancel = context.WithCancel(context.Background())
	RtmpPuberMap.Store(s.Key, s)

	if conf.HlsRec.Enable == true {
		s.Wg.Add(1)
		go HlsCreator(s) // 开启hls生产协程
	}
	if conf.HlsLive.Enable == true {
		s.Wg.Add(1)
		go HlsLiveCreator(s) // 开启hls生产协程
	}
	go RtmpSender(s) // 给所有播放者发送数据

	s.TransmitSwitch = "on"
	var err error
	td := time.Duration(s.PlaybackTimeout)
	for {
		//5秒收不到数据, 就断开连接并上报流状态
		//通过RtmpSender() 中的接收chan来 判断5秒有没有数据
		//s.Conn0.SetReadDeadline(time.Now().Add(5 * time.Second))
		//设备端本地录像回放, 播放暂停时, 要保持住连接
		s.Conn0.SetReadDeadline(time.Now().Add(td * time.Second))

		if s.LogCutoff == true {
			s.LogCutoff = false
			LogCutoffAction(s, "rtmp")
		}

		//s.log.Println("====================>> message", i)
		if s.TransmitSwitch == "off" {
			s.log.Printf("%s RtmpPublisher stop", s.Key)
			RtmpPublishStop(s)
			return
		}

		// 接收(合并)数据 并 传递数据给发送者
		if err = RtmpReceiver(s); err != nil {
			s.log.Println(err)
			s.log.Printf("%s RtmpPublisher stop", s.Key)
			s.TransmitSwitch = "off"
			RtmpPublishStop(s)
			return
		}
	}
}

/*************************************************/
/* RtmpServer
/*************************************************/
func RtmpStop(s *Stream) {
	if s.Conn0 != nil {
		s.Conn0.Close()
		s.Conn0 = nil
	}
	if s.DataChan != nil {
		close(s.DataChan)
		s.DataChan = nil
	}
	/*
		if s.PlayChan != nil {
			close(s.PlayChan)
			s.PlayChan = nil
		}
		if s.HlsChan != nil {
			close(s.HlsChan)
			s.HlsChan = nil
		}
		if s.HlsLiveChan != nil {
			close(s.HlsLiveChan)
			s.HlsLiveChan = nil
		}
	*/
	if s.LogFp != nil {
		s.LogFp.Close()
		s.LogFp = nil
	}
}

func RtmpPublishStop(s *Stream) {
	s.Cancel()
	s.log.Printf("unpublish after cancel")
	s.Wg.Wait()
	s.log.Printf("unpublish after wait")

	/*
		if !strings.Contains(s.AmfInfo.PublishName, BackDoor) {
			//上报流状态 为 直播结束
			var streamStat StreamStateInfo
			streamStat.s = *s
			streamStat.state = 2
			StreamStateChan <- streamStat
		}
	*/
	if s.Conn0 != nil {
		s.Conn0.Close()
		s.Conn0 = nil
	}

	if s.DataChan != nil {
		close(s.DataChan)
		s.DataChan = nil
	}
	/*
		if s.PlayChan != nil {
			close(s.PlayChan)
			s.PlayChan = nil
		}
		if s.HlsChan != nil {
			close(s.HlsChan)
			s.HlsChan = nil
		}
		if s.HlsLiveChan != nil {
			close(s.HlsLiveChan)
			s.HlsLiveChan = nil
		}
	*/
	var p *Stream
	s.Players.Range(func(k, v interface{}) bool {
		p, _ = v.(*Stream)

		switch p.Type {
		case "rtmpPlayer":
			RtmpStop(p)
		case "flvPlayer":
			/*
				if p.PlayChan != nil {
					close(p.PlayChan)
					p.PlayChan = nil
				}
			*/
		}
		return true
	})

	//停止推流时, 接收到的数据要写入ts 并 发送tsinfo给mqtt
	if s.TsFile != nil {
		s.TsFileBuf.Flush()
		s.TsFile.Close()

		if s.PubAuth.Data.HlsUpload == 1 {
			M3u8Update(s)
		} else {
			M3u8Flush(s)
		}
		s.TsFile = nil
	}

	if s.M3u8File != nil {
		s.M3u8File.Close()
		s.M3u8File = nil
	}

	if conf.HlsLive.Enable == true {
		if s.TsLiveRemainName != "" {
			err := os.Remove(s.TsLiveRemainName)
			if err != nil {
				log.Println("delete ts error ", s.TsLiveRemainName, err)
			}
			s.log.Printf("unpublish live delete ts %s", s.TsLiveRemainName)
		}
		if s.TsLiveFile != nil {
			s.TsLiveFileBuf.Flush()
			s.TsLiveFile.Close()
			s.TsLiveFile = nil

			M3u8LiveFlush(s)
		}
		if s.M3u8LiveFile != nil {
			s.M3u8LiveFile.Close()
			s.M3u8LiveFile = nil
		}
	}
	//TODO: improve
	//其他协程需要时间来感知到资源释放
	//time.Sleep(200 * time.Millisecond)
	key := s.Key
	s.log.Printf("publish %s delete, puber num %d", key, utils.SyncMapLen(&RtmpPuberMap))

	if s.LogFp != nil {
		s.LogFp.Close()
		s.LogFp = nil
	}

	s.Chunks = nil
	VideoKeyFrame.Delete(s.Key)

	s.GopCache.MetaData.Delete(s.Key)
	s.GopCache.VideoHeader.Delete(s.Key)
	s.GopCache.AudioHeader.Delete(s.Key)

	RtmpPuberMap.Delete(s.Key)
	log.Printf("publish %s delete, puber num %d", key, utils.SyncMapLen(&RtmpPuberMap))
}

func RtmpPublishStop1(s *Stream) {
	if s.Conn0 != nil {
		s.Conn0.Close()
		s.Conn0 = nil
	}

	if s.DataChan != nil {
		close(s.DataChan)
		s.DataChan = nil
	}
	/*
		if s.PlayChan != nil {
			close(s.PlayChan)
			s.PlayChan = nil
		}
		if s.HlsChan != nil {
			close(s.HlsChan)
			s.HlsChan = nil
		}
		if s.HlsLiveChan != nil {
			close(s.HlsLiveChan)
			s.HlsLiveChan = nil
		}
	*/
	if s.LogFp != nil {
		s.LogFp.Close()
		s.LogFp = nil
	}
}

//cctv1?app=pgm0&pbto=xxx, pbto单位为秒
func GetPlaybackTimeout(url string) int {
	p := strings.Split(url, "?")
	if len(p) < 2 {
		return 0
	}

	ps := strings.Split(p[1], "&")
	ss := ""
	n := 0
	for i := 0; i < len(ps); i++ {
		ss = ps[i]
		if strings.Contains(ss, "pbto=") {
			n, _ = strconv.Atoi(ss[5:])
			return n
		}
	}
	return 0
}

func RtmpHandler(c net.Conn) {
	//这里还无法区分是 rtmp发布 还是 rtmp播放
	s, err := NewStream(c)
	if err != nil {
		log.Println("rtmp error ", err)
		RtmpStop(s)
		return
	}
	s.log.Println("==============================")

	if err := RtmpHandshakeServer(s); err != nil {
		s.log.Println(err)
		RtmpStop(s)
		return
	}
	s.log.Println("RtmpServerHandshake ok")

	if err := RtmpHandleMessage(s); err != nil {
		s.log.Println(err)
		RtmpStop(s)
		return
	}
	s.log.Println("RtmpHandleMessage ok")

	//s.log.Printf("the stream have %d chunks", len(s.Chunks))
	s.log.Printf("the stream is publisher=%t, %s", s.IsPublisher, s.RemoteAddr)

	fn := fmt.Sprintf("%s/%s/publish_rtmp_%s.log", conf.Log.StreamLogPath, s.AmfInfo.StreamId, utils.GetYMD())
	if s.IsPublisher == false {
		fn = fmt.Sprintf("%s/%s/play_rtmp_%s.log", conf.Log.StreamLogPath, s.AmfInfo.StreamId, s.RemoteAddr)
	}
	StreamLogRename(s.LogFn, fn)
	s.LogFn = fn

	if s.IsPublisher {
		//设备本地录像回看, 播放器暂停播放的时候 服务端要保持住连接
		//socket读超时 和 接收等待 都要设置为 pbto秒
		//正常直播流 pbto一般为5秒, 设备本地录像回看流 pbto一般为3600秒
		s.PlaybackTimeout = GetPlaybackTimeout(s.AmfInfo.PublishName)
		if s.PlaybackTimeout == 0 {
			s.PlaybackTimeout = conf.Rtmp.PublishTimeout
		}
		s.log.Printf("pbto=%d", s.PlaybackTimeout)

		//GSP3bnx69BgxI-gCec0oMfJT?app=slivegateway&pbto=0&vhost=127.0.0.1
		//GSP3bnx69BgxI-gCec0oMfJT?puber=zjr#.yMm
		if strings.Contains(s.AmfInfo.PublishName, BackDoor) == true {
			s.PubAuth = PubAuthRsps{}
			s.PubAuth.Data.ResultCode = 1 // 鉴权成功
			s.PubAuth.Data.AesKey = ""    // 空为不加密

			if strings.Contains(s.AmfInfo.PublishName, "action=aIStart") {
				s.PubAuth.Data.HlsUpload = 1
				s.PubAuth.Data.RecordType = 3
			} else if strings.Contains(s.AmfInfo.PublishName, "action=start") {
				s.PubAuth.Data.HlsUpload = 1
				s.PubAuth.Data.RecordType = 1
			} else {
				s.PubAuth.Data.HlsUpload = 0  // 不录制
				s.PubAuth.Data.RecordType = 0 // 录制类型
			}
		} else {
			//异步鉴权未返回结果时, 使用s.PubAuth要先判断 否则会导致崩溃
			go PublishAuth(s)
		}

		s.Type = "rtmpPublisher"
		RtmpPublisher0(s) // 进入for循环 接收数据并拷贝给发送协程
	} else {
		s.Type = "rtmpPlayer"
		RtmpPlayer(s) // 只需把播放信息 存入到Publisher的Players里
	}
}

//监控管理系统里 修改是否加密后 cc会控制重新推流
//摄像头管理后台 修改视频编码格式 修改宽高 等信息后, 摄像头不会断流 会重新发送音视频头信息 流媒体要自动适配
//以上这两种情况发生时, m3u8文件 都要加 #EXT-X-DISCONTINUITY 标签
func RtmpServer() {
	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Rtmp.Port)
	log.Printf("==> rtmp listen on %s", addr)

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
		log.Println("------ new rtmp connect ------")
		log.Printf("lAddr:%s, rAddr:%s", c.LocalAddr().String(), c.RemoteAddr().String())

		go RtmpHandler(c)
	}
}
