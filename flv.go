package main

import (
	"bytes"
	"container/list"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"utils"
)

const (
	IV = "0123456789012345"
)

type FlvStream struct {
}

func NewFlvStream(addr string) *Stream {
	ip := strings.Split(addr, ":")

	s := &Stream{
		RemoteAddr: addr,
		RemoteIp:   ip[0],
		NewPlayer:  true,
	}
	return s
}

/**********************************************************/
/* http-flv
/**********************************************************/
// chan接收数据, http-flv发送数据
func FlvTransmit(p *Stream, s *Stream, exitChan chan int, w http.ResponseWriter, bitrate uint32) {
	var c *Chunk
	var ok bool
	var err error

	td := time.Duration(p.PlaybackTimeout)
	ticker := time.NewTicker(td * time.Second)
	defer ticker.Stop()
	defer p.Wg.Done()
	rc := http.NewResponseController(w)

	for {
		ticker.Reset(td * time.Second)
		select {
		case c, ok = <-s.PlayChan:
			if ok == false {
				p.log.Printf("%s FlvTranspond stop", s.Key)
				exitChan <- 0
				return
			}
		case <-p.Ctx.Done():
			p.log.Printf("publish stop then flv live stop: %s", s.Key)
			exitChan <- 0
			return
		case <-ticker.C:
			p.log.Printf("flv %s recv timeout %d second stop", s.Key, td)
			exitChan <- 0
			return
		}
		err = rc.SetWriteDeadline(time.Now().Add(time.Duration(p.PlaybackTimeout) * time.Second))
		if err != nil {
			s.log.Println(err)
			exitChan <- 0
			return
		}
		//s.log.Printf("test Bitrate:%d, send type:%s,data:%d", bitrate, c.DataType, len(c.MsgData))
		//发送数据给播放器
		_, err = w.Write(c.MsgData)

		if err != nil {
			//write tcp 10.66.253.136:8888->10.66.253.148:54284: write: broken pipe
			s.log.Println(err)
			if strings.Contains(err.Error(), "error: chunk point is nil") {
				continue
			}
			//FIXME: 要确定 是不是 timeout这个关键字
			//if strings.Contains(err.Error(), "timeout") {
			//	continue
			//}
			exitChan <- 0
			return
		}

		//s.log.Printf("SendData, type:%s, size:%d", c.DataType, c.MsgLength)
	}
}

func FlvStop(s *Stream) {
	if s == nil {
		return
	}

	if s.PlayChan != nil {
		close(s.PlayChan)
		s.PlayChan = nil
	}

	if s.LogFp != nil {
		s.LogFp.Close()
		s.LogFp = nil
	}
}

// http-flv播放流程
// 1 http服务收到播放请求 http://ip:port/app/streamId.flv
// 2 获取播放信息, 判断发布者是否存在 不存在直接返回错误
// 3 创建播放者 添加到发布者的Playes里
// 4 开始通过chan接收数据, 加工后发送出去
func GetFlv(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var err error
	app, sid, _ := GetPlayInfo(r.URL.String())
	addr := r.RemoteAddr
	//log.Printf("app:%s, sid:%s, addr:%s", app, sid, addr)

	//判断发布者是否存在 不存在直接返回错误
	//key := fmt.Sprintf("%s_%s", app, sid)
	key := sid
	log.Printf("publisher key is %s", key)

	v, ok := RtmpPuberMap.Load(key)
	if ok == false { // 发布者不存在, 断开连接并返回错误
		err = fmt.Errorf("publisher %s isn't exist", key)
		log.Println(err)
		return nil, err
	}

	var ss []string
	cIpPort := r.FormValue("client")
	if len(cIpPort) == 0 {
		log.Println("flv play no client ip in url ", r.URL.String())
	} else {
		ss = strings.Split(cIpPort, ".")
		if len(ss) < 4 {
			log.Println("flv play illegal client ip in url ", sid, cIpPort)
		} else {
			PlayLocks.Lock()
			playIpPort := fmt.Sprintf("%s.%s.%s.%s:%s", ss[0], ss[1], ss[2], ss[3], ss[4])
			v1, ok := PlayerMap.Load(sid)
			if ok == false {
				var value []PlayAddrTime
				var info PlayAddrTime
				info.PlayIpPort = playIpPort
				info.Timestamp = 0

				value = append(value, info)
				PlayerMap.Store(sid, value)
				log.Println("flv play first ip port", sid, playIpPort)
			} else {
				value := v1.([]PlayAddrTime)
				var bSameIpPort bool = false

				for j, s0 := range value {
					if playIpPort == s0.PlayIpPort {
						value[j].PlayIpPort = playIpPort
						value[j].Timestamp = 0
						PlayerMap.Store(sid, value)
						bSameIpPort = true
						log.Println("flv play same ip port", sid, playIpPort)
						break
					}
				}

				if bSameIpPort == false {
					var info PlayAddrTime
					info.PlayIpPort = playIpPort
					info.Timestamp = 0
					value = append(value, info)
					PlayerMap.Store(sid, value)
					log.Println("flv play add ip port", sid, playIpPort)
				}
			}
			PlayLocks.Unlock()

		}
	}

	p, _ := v.(*Stream)

	s := NewFlvStream(addr)
	s.Type = "flvPlayer"
	s.AmfInfo.App = app
	s.AmfInfo.StreamId = sid
	s.IsPublisher = false
	if strings.Contains(r.URL.String(), BackDoor) {
		s.FlvPlayBackDoor = true
	}
	if strings.Contains(r.URL.String(), "encrypt=0") {
		s.FlvPlayEncrypt = true
	}
	if strings.Contains(r.URL.String(), "startmode=low") {
		s.PlayStartMode = "low"
	} else if strings.Contains(r.URL.String(), "startmode=fastlow") {
		s.PlayStartMode = "fastlow"
	}

	s.LogFn = fmt.Sprintf("%s/%s/play_flv_%s.log", conf.Log.StreamLogPath, sid, addr)
	s.log, s.LogFp, err = StreamLogCreate(s.LogFn)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	p.Wg.Add(1)
	//先启动播放转发协程, 再添加到发布者的players里
	s.PlayChan = make(chan *Chunk, conf.PlayStockMax)
	//要通过w发送数据给播放器, 所以这个http请求不能提前结束
	exitChan := make(chan int)
	defer close(exitChan)
	//w写默认超时时间是多少??? 如何设置???
	//经过实际测试, 50分钟不发数据 w也没超时
	go FlvTransmit(p, s, exitChan, w, p.GopBitrate)

	//w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("transfer-encoding", "chunked")

	s.Key = fmt.Sprintf("%s_%s_%s", app, sid, addr)
	s.log.Printf("player key is %s, AesKey=%s, Bitrate=%dkbps", s.Key, p.PubAuth.Data.AesKey, p.GopBitrate)
	p.Players.Store(s.Key, s)

	<-exitChan //阻塞等待 结束http请求
	err = fmt.Errorf("flv play %s NormalReturn", s.Key)
	s.log.Println(err)
	log.Println(err)

	//千万不要在 接收chan的协程里 关闭chan
	//FlvStop(s) //此函数不能调用, 资源在RtmpSender()里释放
	//自己退出后要通知发送者
	s.PlayClose = true
	r.Close = true

	if len(ss) >= 4 {
		PlayLocks.Lock()
		v1, ok := PlayerMap.Load(sid)
		if ok == true {
			value := v1.([]PlayAddrTime)

			playIpPort := fmt.Sprintf("%s.%s.%s.%s:%s", ss[0], ss[1], ss[2], ss[3], ss[4])
			for j, s0 := range value {
				if playIpPort == s0.PlayIpPort {
					value = append(value[:j], value[j+1:]...)
					PlayerMap.Store(sid, value)
					log.Println("flv play delete ip port", sid, playIpPort)
					break
				}
			}
		} else {
			log.Println("flv play delete streamid not in map ", sid)
		}
		PlayLocks.Unlock()
	}

	return nil, err
}

/**********************************************************/
/* rtmp2flv
/**********************************************************/
//4+1+4+4=13Byte
type FlvHeader struct {
	Signature0     uint8  // 8bit, F 0x46
	Signature1     uint8  // 8bit, L 0x4c
	Signature2     uint8  // 8bit, V 0x56
	Version        uint8  // 8bit, 0x01
	FlagsReserved0 uint8  // 5bit, must be 0
	FlagsAudio     uint8  // 1bit, 0:无音频, 1:有音频
	FlagsReserved1 uint8  // 1bit, must be 0
	FlagsVideo     uint8  // 1bit, 0:无视频, 1:有视频
	DataOffset     uint32 // 32bit, 0x09
	PreTagSize     uint32 // 32bit, 0x0
}

//1+3+3+1+3+4=15Byte
type FlvTag struct {
	TagType           uint8  // 8bit, 8:audio, 9:video, 18:script data
	DataSize          uint32 // 24bit, Data数据的大小
	Timestamp         uint32 // 24bit
	TimestampExtended uint8  // 8bit, 当24bit不够时, TimestampExtended为最高位1字节, 从而把时间戳扩展为32bit
	StreamId          uint32 // 24bit, 固定值0x0
	Data              []byte
	PreTagSize        uint32 // 32bit
}

type FlvData struct {
	Head FlvHeader
	Tags []FlvTag
}

func MetaSendFlv(pub, s *Stream) error {
	var h FlvHeader
	h.Signature0 = 0x46
	h.Signature1 = 0x4c
	h.Signature2 = 0x56
	h.Version = 0x01
	h.FlagsReserved0 = 0x0
	h.FlagsAudio = 0x1
	if pub.AudioCodecType == "" {
		h.FlagsAudio = 0x0
	}
	h.FlagsReserved1 = 0x0
	h.FlagsVideo = 0x1
	if pub.VideoCodecType == "" {
		h.FlagsVideo = 0x0
	}
	h.DataOffset = 0x9
	h.PreTagSize = 0x0
	s.log.Printf("low latency %#v", h)

	//当推流数据没来VideoHeader等数据时, 这时候播放应该失败
	err := FlvSendHead(s, h)
	if err != nil {
		return err
	}

	var timestamp uint32 = 0
	v, ok := s.GopCache.MetaData.Load(pub.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		s.log.Printf("<== set meta data timestamp %d", v1.Timestamp)
		err = FlvSendMetaData(pub, s, *v1)
		if err != nil {
			return err
		}
		timestamp = v1.Timestamp
	}

	//if no metadata, timestamp is zero
	err = FlvSendMetaDataAes(pub, s, timestamp) // 发送解密信息
	if err != nil {
		return err
	}

	return nil
}

// pub是发布者, s是播放者
func GopCacheFastlowSendFlv(pub, s *Stream, gop *GopCache) error {
	var h FlvHeader
	h.Signature0 = 0x46
	h.Signature1 = 0x4c
	h.Signature2 = 0x56
	h.Version = 0x01
	h.FlagsReserved0 = 0x0
	h.FlagsAudio = 0x1
	if pub.AudioCodecType == "" {
		h.FlagsAudio = 0x0
	}
	h.FlagsReserved1 = 0x0
	h.FlagsVideo = 0x1
	if pub.VideoCodecType == "" {
		h.FlagsVideo = 0x0
	}
	h.DataOffset = 0x9
	h.PreTagSize = 0x0
	s.log.Printf("%#v", h)

	//TODO: use sync.Mutex
	goplocks.Lock()
	var chunkNum, videoChunkNum, audioChunkNum int
	var lastChunk, videoChunk, audioChunk Chunk
	for e := gop.MediaData.Back(); e != nil; e = e.Prev() {
		c := (e.Value).(*Chunk)
		if chunkNum == 0 {
			lastChunk = *c
		}
		chunkNum++
		if videoChunkNum == 0 && (c.DataType == "VideoKeyFrame" || c.DataType == "VideoInterFrame") {
			videoChunk = *c
			videoChunkNum++
		}
		if audioChunkNum == 0 && c.DataType == "AudioAacFrame" {
			audioChunk = *c
			audioChunkNum++
		}
		s.log.Printf("<== videoChunkNum :%d, audioChunkNum %d: DataType:%s, Timestamp:%d", videoChunkNum, audioChunkNum, c.DataType, c.Timestamp)
		if videoChunkNum > 0 && audioChunkNum > 0 {
			break
		}
	}
	s.log.Printf("<== videoChunkNum :%d, audioChunkNum %d", videoChunkNum, audioChunkNum)

	//当推流数据没来VideoHeader等数据时, 这时候播放应该失败
	err := FlvSendHead(s, h)
	if err != nil {
		goplocks.Unlock()
		return err
	}

	var timestamp uint32 = 0
	v, ok := gop.MetaData.Load(pub.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if chunkNum > 0 {
			s.log.Printf("<== set meta data timestamp %d to %d", v1.Timestamp, lastChunk.Timestamp)
			v1.Timestamp = lastChunk.Timestamp
			err = FlvSendMetaData(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}
			timestamp = lastChunk.Timestamp
		} else {
			s.log.Printf("<== set meta data timestamp %d", v1.Timestamp)
			err = FlvSendMetaData(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}
			timestamp = v1.Timestamp
		}
	}

	if chunkNum > 0 {
		timestamp = lastChunk.Timestamp
	}
	err = FlvSendMetaDataAes(pub, s, timestamp) // 发送解密信息
	if err != nil {
		goplocks.Unlock()
		return err
	}
	v, ok = gop.VideoHeader.Load(pub.Key)
	if ok == false {
		s.log.Printf("video header is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if videoChunkNum > 0 {
			s.log.Printf("<== set video header timestamp %d to %d", v1.Timestamp, videoChunk.Timestamp)
			v1.Timestamp = videoChunk.Timestamp
			err = FlvSendVideoHead(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}
		} else {
			err = FlvSendVideoHead(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}
		}
	}

	v, ok = gop.AudioHeader.Load(pub.Key)
	if ok == false {
		s.log.Printf("audio header is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if audioChunkNum > 0 {
			s.log.Printf("<== set audio header timestamp %d to %d", v1.Timestamp, audioChunk.Timestamp)
			v1.Timestamp = audioChunk.Timestamp
			err = FlvSendAudioHead(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}

			//need send one audio chuck, some player need audio data to initial
			err = MessageSendFlv(pub, s, audioChunk)
			if err != nil {
				s.log.Println(err)
				goplocks.Unlock()
				return err
			}
		} else {
			if videoChunkNum > 0 {
				s.log.Printf("<== set audio header timestamp %d to %d", v1.Timestamp, videoChunk.Timestamp)
				v1.Timestamp = videoChunk.Timestamp
			}
			err = FlvSendAudioHead(pub, s, *v1)
			if err != nil {
				goplocks.Unlock()
				return err
			}
		}
	}

	if gop.MediaData.Len() > 0 {
		err = FlvSendFastlowData(pub, s, gop.MediaData, videoChunk.Timestamp)
	}
	goplocks.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// pub是发布者, s是播放者
func GopCacheSendFlv(pub, s *Stream, gop *GopCache) error {
	var h FlvHeader
	h.Signature0 = 0x46
	h.Signature1 = 0x4c
	h.Signature2 = 0x56
	h.Version = 0x01
	h.FlagsReserved0 = 0x0
	h.FlagsAudio = 0x1
	if pub.AudioCodecType == "" {
		h.FlagsAudio = 0x0
	}
	h.FlagsReserved1 = 0x0
	h.FlagsVideo = 0x1
	if pub.VideoCodecType == "" {
		h.FlagsVideo = 0x0
	}
	h.DataOffset = 0x9
	h.PreTagSize = 0x0
	s.log.Printf("%#v", h)

	//TODO: use sync.Mutex
	goplocks.Lock()
	var chunkNum, videoChunkNum, audioChunkNum int
	var firstChunk, videoChunk, audioChunk Chunk
	for e := gop.MediaData.Front(); e != nil; e = e.Next() {
		c := (e.Value).(*Chunk)
		if chunkNum == 0 {
			firstChunk = *c
		}
		chunkNum++
		if videoChunkNum == 0 && (c.DataType == "VideoKeyFrame" || c.DataType == "VideoInterFrame") {
			videoChunk = *c
			videoChunkNum++
		}
		if audioChunkNum == 0 && c.DataType == "AudioAacFrame" {
			audioChunk = *c
			audioChunkNum++
		}
		s.log.Printf("<== videoChunkNum :%d, audioChunkNum %d: DataType:%s, Timestamp:%d", videoChunkNum, audioChunkNum, c.DataType, c.Timestamp)
		if videoChunkNum > 0 && audioChunkNum > 0 {
			break
		}
	}
	s.log.Printf("<== videoChunkNum :%d, audioChunkNum %d", videoChunkNum, audioChunkNum)
	goplocks.Unlock()

	//当推流数据没来VideoHeader等数据时, 这时候播放应该失败
	err := FlvSendHead(s, h)
	if err != nil {
		return err
	}

	var timestamp uint32 = 0
	v, ok := gop.MetaData.Load(pub.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if chunkNum > 0 {
			s.log.Printf("<== set meta data timestamp %d to %d", v1.Timestamp, firstChunk.Timestamp)
			v1.Timestamp = firstChunk.Timestamp
			err = FlvSendMetaData(pub, s, *v1)
			if err != nil {
				return err
			}
			timestamp = firstChunk.Timestamp
		} else {
			s.log.Printf("<== set meta data timestamp %d", v1.Timestamp)
			err = FlvSendMetaData(pub, s, *v1)
			if err != nil {
				return err
			}
			timestamp = v1.Timestamp
		}
	}

	if chunkNum > 0 {
		timestamp = firstChunk.Timestamp
	}
	err = FlvSendMetaDataAes(pub, s, timestamp) // 发送解密信息
	if err != nil {
		return err
	}
	v, ok = gop.VideoHeader.Load(pub.Key)
	if ok == false {
		s.log.Printf("video header is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if videoChunkNum > 0 {
			s.log.Printf("<== set video header timestamp %d to %d", v1.Timestamp, videoChunk.Timestamp)
			v1.Timestamp = videoChunk.Timestamp
			err = FlvSendVideoHead(pub, s, *v1)
			if err != nil {
				return err
			}
		} else {
			err = FlvSendVideoHead(pub, s, *v1)
			if err != nil {
				return err
			}
		}
	}

	v, ok = gop.AudioHeader.Load(pub.Key)
	if ok == false {
		s.log.Printf("audio header is not exist %v", pub.Key)
	} else {
		v1 := v.(*Chunk)
		if audioChunkNum > 0 {
			s.log.Printf("<== set audio header timestamp %d to %d", v1.Timestamp, audioChunk.Timestamp)
			v1.Timestamp = audioChunk.Timestamp
			err = FlvSendAudioHead(pub, s, *v1)
			if err != nil {
				return err
			}
		} else {
			if videoChunkNum > 0 {
				s.log.Printf("<== set audio header timestamp %d to %d", v1.Timestamp, videoChunk.Timestamp)
				v1.Timestamp = videoChunk.Timestamp
			}
			err = FlvSendAudioHead(pub, s, *v1)
			if err != nil {
				return err
			}
		}
	}

	goplocks.Lock()
	err = FlvSendData(pub, s, gop.MediaData)
	goplocks.Unlock()
	if err != nil {
		return err
	}
	return nil
}

func FlvSendHead(s *Stream, h FlvHeader) error {
	buf := make([]byte, 13)
	buf[0] = h.Signature0
	buf[1] = h.Signature1
	buf[2] = h.Signature2
	buf[3] = h.Version
	buf[4] = ((h.FlagsReserved0 & 0x1f) << 3) | ((h.FlagsAudio & 0x1) << 2) | ((h.FlagsReserved1 & 0x1) << 1) | (h.FlagsVideo & 0x1)
	Uint32ToByte(h.DataOffset, buf[5:9], BE)
	Uint32ToByte(h.PreTagSize, buf[9:13], BE)
	//s.log.Println(len(buf), buf)

	var c Chunk
	c.DataType = "FlvHeaderer"
	c.MsgLength = 13
	c.MsgData = buf[:]
	s.PlayChan <- &c
	return nil
}

func FlvSendMetaData(pub, s *Stream, c Chunk) error {
	s.log.Println("<== send MeteData")
	var err error

	err = MessageSendFlv(pub, s, c)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func FlvSendMetaDataAes(pub, s *Stream, timestamp uint32) error {
	// 推流鉴权要求的播放加密 要通过flv的metadata发送解密参数
	if pub.PubAuth.Data.AesKey == "" {
		return nil
	}
	s.log.Println("<== send MeteData AES")

	key := pub.PubAuth.Data.AesKey
	md := make(Object)
	md["key"] = key
	s.log.Println(md)

	// 结构化转序列化
	d, _ := AmfMarshal(s, "onMetaData", md)
	s.log.Printf("%x", d)

	c := CreateMessage(MsgTypeIdDataAmf0, uint32(len(d)), d)
	c.Csid = 0x4
	c.MsgStreamId = 0x0
	// 0x8 Audio, 0x9 Video, 0x12 Metadata
	c.MsgTypeId = 0x12
	c.DataType = "MetaData"
	c.Timestamp = timestamp

	err := MessageSendFlv(pub, s, c)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func FlvSendVideoHead(pub, s *Stream, c Chunk) error {
	s.log.Println("<== send VideoHead")
	var err error

	err = MessageSendFlv(pub, s, c)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func FlvSendAudioHead(pub, s *Stream, c Chunk) error {
	s.log.Println("<== send AudioHead")
	var err error

	err = MessageSendFlv(pub, s, c)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func FlvSendFastlowData(pub, s *Stream, md *list.List, timestamp uint32) error {
	s.log.Printf("<== GopCache MediaData Len=%d", md.Len())
	var err error
	if md == nil {
		err = fmt.Errorf("error: MediaData list is nil")
		s.log.Println(err)
		return err
	}

	i := 0
	for e := md.Front(); e != nil; e = e.Next() {
		c := (e.Value).(*Chunk)
		if c.DataType == "AudioAacFrame" {
			continue
		}
		c.Timestamp = timestamp
		err = MessageSendFlv(pub, s, *c)
		if err != nil {
			s.log.Println(err)
			return err
		}
		s.log.Printf("<== Send %d: DataType:%s, MsgLength:%d, Timestamp:%d", i, c.DataType, c.MsgLength, c.Timestamp)
		i++
	}
	s.log.Println("<== GopCache MediaData send ok")
	return nil
}

func FlvSendData(pub, s *Stream, md *list.List) error {
	s.log.Printf("<== GopCache MediaData Len=%d", md.Len())
	var err error
	if md == nil {
		err = fmt.Errorf("error: MediaData list is nil")
		s.log.Println(err)
		return err
	}

	i := 0
	for e := md.Front(); e != nil; e = e.Next() {
		c := (e.Value).(*Chunk)
		err = MessageSendFlv(pub, s, *c)
		if err != nil {
			s.log.Println(err)
			return err
		}
		s.log.Printf("<== Send %d: DataType:%s, MsgLength:%d, Timestamp:%d", i, c.DataType, c.MsgLength, c.Timestamp)
		i++
	}
	s.log.Println("<== GopCache MediaData send ok")
	return nil
}

func AesEncrypt(orgData, key, iv []byte) []byte {
	blk, err := aes.NewCipher(key)
	if err != nil {
		log.Println(err)
		return nil
	}

	s := cipher.NewOFB(blk, iv)
	ob := new(bytes.Buffer)
	//生成流式写入器
	sw := &cipher.StreamWriter{S: s, W: ob}

	nb := bytes.NewBuffer(orgData)
	//真正进行加密的步骤
	_, err = io.Copy(sw, nb)
	if err != nil {
		log.Println(err)
		return nil
	}
	return ob.Bytes()
}

// s是发布者, p是播放者
func MessageSendFlv(s, p *Stream, c Chunk) error {
	var t FlvTag
	t.TagType = uint8(c.MsgTypeId)
	t.DataSize = c.MsgLength
	//49811025       0x2F80E51
	if c.Timestamp <= 0xffffff {
		t.Timestamp = c.Timestamp
		t.TimestampExtended = 0x0
	} else {
		t.Timestamp = c.Timestamp & 0xffffff
		t.TimestampExtended = uint8(c.Timestamp >> 24)
		//p.log.Printf("c.Timestamp=%x, t.Timestamp=%x, t.TimestampExtended=%x", c.Timestamp, t.Timestamp, t.TimestampExtended)
	}
	t.StreamId = c.MsgStreamId
	t.Data = nil
	t.PreTagSize = 11 + t.DataSize
	//p.log.Printf("%#v", t)

	//11 + DataSize + 4
	size := 11 + t.DataSize + 4
	buf := make([]byte, size)
	//11byte
	buf[0] = t.TagType
	Uint24ToByte(t.DataSize, buf[1:4], BE)
	Uint24ToByte(t.Timestamp, buf[4:7], BE)
	buf[7] = t.TimestampExtended
	Uint24ToByte(t.StreamId, buf[8:11], BE)

	//推流鉴权要求的播放加密 在这里做 只对视频关键帧加密
	//"VideoHeader" 和 "VideoKeyFrame" 两种数据 都是视频关键帧
	if c.DataType == "VideoHeader" || c.DataType == "VideoKeyFrame" {
		key := s.PubAuth.Data.AesKey
		if key == "" || p.FlvPlayBackDoor == true || p.FlvPlayEncrypt == true {
			//视频数据不加密
			copy(buf[11:11+t.DataSize], c.MsgData)
		} else {
			//前5个字节不加密 解密是需, 从第6个字节开始加密
			copy(buf[11:16], c.MsgData[:5])

			encData := AesEncrypt(c.MsgData[5:], []byte(key), []byte(IV))
			//p.log.Printf("orgData len=%d, encData len=%d", len(c.MsgData[5:]), len(encData))
			copy(buf[16:16+t.DataSize-5], encData)
		}
	} else {
		//音频等其他类型数据
		copy(buf[11:11+t.DataSize], c.MsgData)
	}

	//PreTagSize
	Uint32ToByte(t.PreTagSize, buf[11+t.DataSize:], BE)

	var ck Chunk
	ck.DataType = c.DataType
	ck.MsgLength = size
	ck.MsgData = buf[:]

	//发送数据给播放者
	if len(p.PlayChan) < conf.PlayStockMax {
		p.PlayChan <- &ck
	} else {
		p.log.Printf("PlayChanNum=%d(%d), DropDataType=%s", len(p.PlayChan), conf.PlayStockMax, c.DataType)
	}

	//为了 play_flv_xxx.log 能有周期性打印
	p.FlvSendDataSize += c.MsgLength
	if p.FlvSendDataSize >= conf.Flv.FlvSendDataSize {
		p.log.Printf("send data %d(2MB) + %d byte", conf.Flv.FlvSendDataSize, p.FlvSendDataSize-conf.Flv.FlvSendDataSize)
		p.FlvSendDataSize = 0
	}

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
		go TrafficReport(p, p.TrafficInfo, "play", "http-flv")
		p.StartTime = cTime
		p.DataSize = 0
	} else {
		//FIXME: 只算c.MsgLength, 其实是少算了一些字节
		p.DataSize += c.MsgLength
	}
	//p.log.Printf("%#v", p.TrafficInfo)
	return nil
}
