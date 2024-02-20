package main

import (
	"container/list"
	"fmt"
	"log"
	"sync"
	"utils"
)

/*************************************************/
/* gop Cache
/*************************************************/
func MetaSendRtmp(p, s *RtmpStream) {
	// 1 发送Metadata
	s.log.Println("<== low latency send Metadata")
	v, ok := p.GopCache.MetaData.Load(p.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", p.Key)
	} else {
		//TODO: timestamp use original or set 0?
		s.PlayChan <- v.(*Chunk)
	}
}

func GopCacheFastlowSendRtmp(p, s *RtmpStream, gop *GopCache) {
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

	// 1 发送Metadata
	v, ok := gop.MetaData.Load(p.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", p.Key)
	} else {
		s.log.Println("<== send Metadata")
		if chunkNum > 0 {
			v1 := v.(*Chunk)
			v1.Timestamp = lastChunk.Timestamp
			s.PlayChan <- v1
		} else {
			s.PlayChan <- v.(*Chunk)
		}
	}

	// 2 发送VideoHeader
	v, ok = gop.VideoHeader.Load(p.Key)
	if ok == false {
		s.log.Printf("video header is not exist %v", p.Key)
	} else {
		s.log.Println("<== send VideoHeader")
		if videoChunkNum > 0 {
			v1 := v.(*Chunk)
			v1.Timestamp = videoChunk.Timestamp
			s.PlayChan <- v1
		} else {
			s.PlayChan <- v.(*Chunk)
		}
	}

	// 3 发送AudioHeader
	v, ok = gop.AudioHeader.Load(p.Key)
	if ok == false {
		s.log.Printf("audio header is not exist %v", p.Key)
	} else {
		s.log.Println("<== send AudioHeader")
		v1 := v.(*Chunk)
		if audioChunkNum > 0 {
			v1.Timestamp = audioChunk.Timestamp
			s.PlayChan <- v1
			s.PlayChan <- &audioChunk
		} else {
			if videoChunkNum > 0 {
				v1.Timestamp = videoChunk.Timestamp
			}
			s.PlayChan <- v1
		}
	}

	// 4 发送MediaData(包含最后收到的数据)
	s.log.Println("<== send MediaData, GopCache.MediaData len", gop.MediaData.Len())
	i := 0
	var c *Chunk

	for e := gop.MediaData.Front(); e != nil; e = e.Next() {
		c = (e.Value).(*Chunk)
		if c.DataType == "AudioAacFrame" {
			continue
		}
		c.Timestamp = videoChunk.Timestamp
		s.PlayChan <- c
		s.log.Println("<== GopCacheFastlowSendRtmp", i, c.DataType, c.MsgLength, c.Timestamp)
		i++
	}
	goplocks.Unlock()
	s.log.Println("<== GopCacheFastlowSendRtmp() ok")
}

func GopCacheFastlowSend(s, p *RtmpStream) {
	switch p.StreamType {
	case "rtmpPlayer":
		GopCacheFastlowSendRtmp(s, p, &s.GopCache)
	case "flvPlayer":
		GopCacheFastlowSendFlv(s, p, &s.GopCache)
	}
}

func GopCacheSend(s, p *RtmpStream) {
	switch p.StreamType {
	case "rtmpPlayer":
		GopCacheSendRtmp(s, p, &s.GopCache)
	case "flvPlayer":
		GopCacheSendFlv(s, p, &s.GopCache)
	}
}

func CalcGopBitrate(s *RtmpStream, c Chunk) {
	if s.GopStartTs == 0 {
		s.GopStartTs = utils.GetTimestamp("s")
	}
	if c.DataType == "VideoKeyFrame" {
		s.GopNum++
	}

	//10个gop一般是20秒, 线上建议30个gop 统计并打印一次
	if s.GopNum > conf.Rtmp.BitrateGopNum {
		s.GopEndTs = utils.GetTimestamp("s")
		//发送来的是字节, 码率是bit 所以要乘8
		if uint32(s.GopEndTs-s.GopStartTs) == 0 {
			s.GopBitrate = 0
			s.log.Printf("abnormal GopNum=%d, EndTs=%d, StartTs=%d", s.GopNum, s.GopEndTs, s.GopStartTs)
		} else {
			s.GopBitrate = s.GopDataSize / 1024 / uint32(s.GopEndTs-s.GopStartTs) * 8
		}

		s.log.Printf("GopNum=%d, DataSize=%d, EndTs=%d, StartTs=%d, Bitrate=%dkbps, NaluNum=%d", s.GopNum-1, s.GopDataSize, s.GopEndTs, s.GopStartTs, s.GopBitrate, s.NaluNum)
		s.log.Printf("FirstDifValue(%dms), VideoTsDifValue=%dms, vfps=%d; AudioTsDifValue=%dms, afps=%d", s.FirstDifValue, s.VideoTsDifValue, s.VideoFps, s.AudioTsDifValue, s.AudioFps)

		s.GopNum = 1
		s.GopStartTs = s.GopEndTs
		s.GopDataSize = 0
	}
	s.GopDataSize += c.MsgLength
	//s.log.Printf("GopNum=%d, MsgLen=%d, DataSize=%d, DataType=%s", s.GopNum, c.MsgLength, s.GopDataSize, c.DataType)
}

func PrintList(s *RtmpStream, l *list.List) {
	s.log.Println(">>>>>> s.MediaData list <<<<<<")
	var c *Chunk
	var i uint
	for e := l.Front(); e != nil; e = e.Next() {
		c = (e.Value).(*Chunk)
		s.log.Println(i, c.DataType, c.MsgLength, c.Timestamp)
		i++
	}
}

func sGopCacheSendRtmp(rs *RtmpStream, s *RtmpStream, gop *GopCache) {
	// 1 发送Metadata
	s.log.Println("<== send Metadata")

	// 2 发送VideoHeader
	s.log.Println("<== send VideoHeader")
	var c Chunk
	c.MsgTypeId = MsgTypeIdVideo
	c.MsgStreamId = 0
	c.Timestamp = 0

	/*
		c.MsgData = CreateVideoPacket(gop.VideoHeader, "header", "h264")
		c.MsgLength = uint32(len(c.MsgData))
		s.log.Printf("naluLen:%d, rtmpLen:%d", len(gop.VideoHeader), c.MsgLength)
		s.log.Printf("naluData:%x, rtmpData:%x", gop.VideoHeader[:10], c.MsgData[10])
	*/

	s.PlayChan <- &c

	// 3 发送AudioHeader
	s.log.Println("<== send AudioHeader")

	// 4 发送MediaData(包含最后收到的数据)
	s.log.Println("<== send MediaData")
	s.log.Println("<== GopCache.MediaData len", gop.MediaData.Len())
	i := 0
	var p *AvPacket
	for e := gop.MediaData.Front(); e != nil; e = e.Next() {
		p = (e.Value).(*AvPacket)
		var c Chunk
		c.MsgTypeId = MsgTypeIdVideo
		c.MsgStreamId = 0
		c.Timestamp = 0

		if p.Type == "VideoInterFrame" {
			c.MsgData = CreateVideoPacket(p.Data, "interframe", "h264")
		} else {
			c.MsgData = CreateVideoPacket(p.Data, "keyframe", "h264")
		}
		c.MsgLength = uint32(len(c.MsgData))
		s.log.Printf("naluLen:%d, rtmpLen:%d", len(p.Data), c.MsgLength)
		s.log.Printf("naluData:%x, rtmpData:%x", p.Data[:10], c.MsgData[10])

		s.PlayChan <- &c
		s.log.Printf("GopSend %d, %s, %d, %d", i, p.Type, len(p.Data), p.Timestamp)
		i++
	}
	s.log.Println("<== GopCacheSendRtmp() ok")
}

func sGopCacheUpdate(s *RtmpStream) {
	if s.GopCacheNum <= s.GopCacheMax {
		return
	}

	var i, vFrameNum, aFrameNum, DataSize uint32
	var p *AvPacket
	var n *list.Element
	for e := s.MediaData.Front(); e != nil; e = n {
		p = (e.Value).(*AvPacket)
		//s.log.Printf("list %d: %s, %d, %d", i, p.Type, len(p.Data), p.Timestamp)
		if p.Type == "VideoKeyFrame" {
			s.GopCacheNum--
		}
		//一次只能删除一个gop的数据
		if s.GopCacheNum < s.GopCacheMax {
			s.GopCacheNum++
			break
		}
		s.log.Printf("list %d rm: %s, %d, %d", i, p.Type, len(p.Data), p.Timestamp)

		// 这部分只是用户统计, 线上运行可以注释掉
		if p.Type == "VideoKeyFrame" || p.Type == "VideoInterFrame" {
			vFrameNum++
		}
		if p.Type == "AudioAacFrame" {
			aFrameNum++
		}
		DataSize += uint32(len(p.Data))
		// 这部分只是用户统计, 线上运行可以注释掉

		n = e.Next()
		s.MediaData.Remove(e)
		i++
	}
	s.log.Printf("GopRm: FrameLen=%d, vFrameNum=%d, aFrameNum=%d, DataSize=%d, SaveFrameNum=%d", i, vFrameNum, aFrameNum, DataSize, s.MediaData.Len())
}

func GopCacheShow(s *RtmpStream) {
	var i int
	var c *Chunk

	//如果打印的值相同, 排查chan接收数据 是否在for循环里申请
	//详见 rtmp_server.go, 230         var c Chunk
	for e := s.GopCache.MediaData.Front(); e != nil; e = e.Next() {
		c = (e.Value).(*Chunk)
		s.log.Printf("%d: type:%d(%s), ts=%d, len=%d, naluNum:%d", i, c.MsgTypeId, c.DataType, c.Timestamp, c.MsgLength, c.NaluNum)
		i++
	}
}

/*************************************************/
/* gop Cache
/*************************************************/
//要缓存的数据
//1 Metadata
//2 Video Header
//3 Audio Header
//4 MediaData 里面有 I/B/P帧和音频帧, 按来的顺序存放，
//  MediaData 里内容举例：I B P B A A B P I ...
//MediaData里 最多有 GopCacheMax 个 Gop的数据
//比如GopCacheMax=2, 那么MediaData里最多有2个Gop, 第2个Gop不完整,
//当第3个Gop的关键帧到达的时，删除第1个Gop的数据
type GopCache struct {
	GopCacheMax int // 最多缓存几个Gop
	GopCacheNum int // 当前缓存种有几个Gop
	//MetaData    *Chunk
	//VideoHeader *Chunk
	//AudioHeader *Chunk
	MetaData    sync.Map
	VideoHeader sync.Map
	AudioHeader sync.Map
	MediaData   *list.List // 双向链表, FIXME:写的时候不能读
}

//dType: header, keyframe, interframe
//codecType: h264, h265
func CreateVideoPacket(data []byte, dType, codecType string) []byte {
	var vp RtmpVideoDataHeader
	vp.FrameType = 1
	if dType == "interframe" {
		vp.FrameType = 2
	}
	vp.CodecID = 7
	if codecType == "h265" {
		vp.CodecID = 12
	}
	vp.AvcPacketType = 1
	if dType == "header" {
		vp.AvcPacketType = 0
	}
	vp.CompositionTime = 0

	l := 5 + len(data)
	d := make([]byte, l)
	d[0] = ((vp.FrameType & 0xf) << 4) | (vp.CodecID & 0x0f)
	d[1] = vp.AvcPacketType
	Uint32ToByte(vp.CompositionTime, d[2:5], BE)
	copy(d[5:], data)
	return d
}

/*************************************************/
/* gop Cache
/*************************************************/
//avcc格式 要转为 annexB格式
//开始码+sps+开始码+pps+开始码+VideoKeyFrame
func GopGetIframeH264(s *RtmpStream) ([]byte, error) {
	var d []byte
	var err error
	var c Chunk
	/*
		v, ok := VideoKeyFrame.Load(s.Key)
		if ok == false {
			s.log.Printf(" %v video keyframe is not exist", s.Key)
			err = fmt.Errorf("%s video keyframe is not exist", s.Key)
			return nil, err
		} else {
			c = v.(Chunk)
		}
	*/
	//log.Println(c.DataType)

	if s.TransmitSwitch == "off" {
		log.Printf("%s is stoping", s.Key)
		err = fmt.Errorf("%s is stoping", s.Key)
		return nil, err
	}

	//MsgData前5字节要去掉, sps和pps前面各有1个4字节的起始码
	//nalu 4字节长度要换成4字节起始码, 如果有多个slice 每个slice都要换
	l := 8 + uint32(s.AvcC.SpsSize) + uint32(s.AvcC.PpsSize) + c.MsgLength - 4
	d = make([]byte, l)
	sc := []byte{0x00, 0x00, 0x00, 0x01}

	var st uint32
	copy(d[st:], sc)
	st += 4
	copy(d[st:], s.AvcC.SpsData)
	st += uint32(s.AvcC.SpsSize)
	copy(d[st:], sc)
	st += 4
	copy(d[st:], s.AvcC.PpsData)
	st += uint32(s.AvcC.PpsSize)

	var i, naluLen, s0, e0 uint32 = 0, 0, 5, 9
	//log.Printf("keyFrame have %d slice", c.NaluNum)
	for i = 0; i < c.NaluNum; i++ {
		naluLen = ByteToUint32(c.MsgData[s0:e0], BE)
		copy(d[st:], sc)
		st += 4
		s0 = e0 + naluLen
		copy(d[st:], c.MsgData[e0:s0])
		st += naluLen
		e0 = s0 + 4
	}

	return d, nil
}

// avcc格式 要转为 annexB格式
// 开始码+vps+开始码+sps+开始码+pps+开始码+VideoKeyFrame
func GopGetIframeH265(s *RtmpStream) ([]byte, error) {
	var d []byte
	var err error
	var c Chunk
	/*
		v, ok := VideoKeyFrame.Load(s.Key)
		if ok == false {
			s.log.Printf(" %v video keyframe is not exist", s.Key)
			err = fmt.Errorf("%s video keyframe is not exist", s.Key)
			return nil, err
		} else {
			c = v.(Chunk)
		}
	*/
	//log.Println(c.DataType)

	if s.TransmitSwitch == "off" {
		log.Printf("%s is stoping", s.Key)
		err = fmt.Errorf("%s is stoping", s.Key)
		return nil, err
	}

	if len(s.HevcC.Vps) == 0 || len(s.HevcC.Sps) == 0 || len(s.HevcC.Pps) == 0 {
		s.log.Printf(" %v video header is not exist", s.Key)
		err = fmt.Errorf("%s video header is not exist", s.Key)
		return nil, err
	}
	//MsgData前5字节VideoTag数据要去掉, vps sps和pps前面各有1个4字节的起始码
	//nalu 4字节长度要换成4字节起始码, 如果有多个slice 每个slice都要换
	l := 12 + uint32(s.HevcC.Vps[0].NaluLen) + uint32(s.HevcC.Sps[0].NaluLen) + uint32(s.HevcC.Pps[0].NaluLen) + c.MsgLength - 5
	d = make([]byte, l)
	sc := []byte{0x00, 0x00, 0x00, 0x01}

	var st uint32
	copy(d[st:], sc)
	st += 4
	copy(d[st:], s.HevcC.Vps[0].NaluData)
	st += uint32(s.HevcC.Vps[0].NaluLen)
	copy(d[st:], sc)
	st += 4
	copy(d[st:], s.HevcC.Sps[0].NaluData)
	st += uint32(s.HevcC.Sps[0].NaluLen)
	copy(d[st:], sc)
	st += 4
	copy(d[st:], s.HevcC.Pps[0].NaluData)
	st += uint32(s.HevcC.Pps[0].NaluLen)

	var i, naluLen, s0, e0 uint32 = 0, 0, 5, 9
	//log.Printf("keyFrame have %d slice", c.NaluNum)
	for i = 0; i < c.NaluNum; i++ {
		naluLen = ByteToUint32(c.MsgData[s0:e0], BE)
		copy(d[st:], sc)
		st += 4
		s0 = e0 + naluLen
		copy(d[st:], c.MsgData[e0:s0])
		st += naluLen
		e0 = s0 + 4
	}

	return d, err
}

func GopCacheNew() GopCache {
	return GopCache{
		GopCacheMax: conf.Rtmp.GopCacheMax,
		MediaData:   list.New(),
	}
}

//TODO: gop中总的帧减去第2个关键帧的位置, 大于某个值时, 才会把
//第1个关键帧到第2个关键帧之间的数据删除, 以确保gop中有足够数据快速启播
func GopCacheUpdate(s *RtmpStream) {
	// 1 当前缓存gop数 GopCacheNum <= GopCacheMax 就直接存入并退出
	// 1 当前缓存gop数 GopCacheNum > GopCacheMax 要先删除第1个Gop数据(含音频)然后再存入
	// 第1个关键帧存入时 GopCacheNum = 1
	// 第2个关键帧存入时 GopCacheNum = 2
	//s.log.Printf("GopCacheMax=%d, GopCacheNum=%d, MediaDataLen=%d", s.GopCacheMax, s.GopCacheNum, s.MediaData.Len())
	//s.log.Println(s.GopCacheNum, s.GopCacheMax)
	if s.GopCacheNum <= s.GopCacheMax {
		return
	}

	var i, vFrameNum, aFrameNum, DataSize uint32
	var c *Chunk
	var n *list.Element
	//goplocks.Lock()
	for e := s.MediaData.Front(); e != nil; e = n {
		c = (e.Value).(*Chunk)
		//s.log.Printf("list show: %s, %d, %d", c.DataType, c.MsgLength, c.Timestamp)
		if c.DataType == "VideoKeyFrame" {
			s.GopCacheNum--
		}
		//一次只能删除一个gop的数据
		if s.GopCacheNum < s.GopCacheMax {
			s.GopCacheNum++
			break
		}
		//s.log.Printf("GOP remove %d: %s, %d, %d", i, c.DataType, c.MsgLength, c.Timestamp)

		// 这部分只是用户统计, 线上运行可以注释掉
		if c.DataType == "VideoKeyFrame" || c.DataType == "VideoInterFrame" {
			vFrameNum++
		}
		if c.DataType == "AudioAacFrame" {
			aFrameNum++
		}
		DataSize += c.MsgLength
		// 这部分只是用户统计, 线上运行可以注释掉

		n = e.Next()
		s.MediaData.Remove(e)
		i++
	}
	//goplocks.Unlock()
	s.log.Printf("Gop remove GopLen=%d, vFrameNum=%d, aFrameNum=%d, DataSize=%d, FrameNum=%d", i, vFrameNum, aFrameNum, DataSize, s.MediaData.Len())

	//音视频首包相同时, VideoTsDifValue=50ms, vfps=20; … 打印太多
	if s.VideoFps == 0 {
		s.FirstDifValue = s.FirstVideoTs - s.FirstAudioTs
		if s.FirstVideoTs < s.FirstAudioTs {
			s.FirstDifValue = s.FirstAudioTs - s.FirstVideoTs
		}
		s.log.Printf("|FirstVideoTs(%d) - FirstAudioTs(%d)| = FirstDifValue(%dms)", s.FirstVideoTs, s.FirstAudioTs, s.FirstDifValue)

		//VideoFrameTimeGap
		if s.VideoTsDifValue != 0 {
			s.VideoFps = 1000 / s.VideoTsDifValue
		}
		if s.AudioTsDifValue != 0 {
			s.AudioFps = 1000 / s.AudioTsDifValue
		}
		s.log.Printf("VideoTsDifValue=%dms, vfps=%d; AudioTsDifValue=%dms, afps=%d", s.VideoTsDifValue, s.VideoFps, s.AudioTsDifValue, s.AudioFps)
	}
}

func GopCacheSendRtmp0(p, s *RtmpStream, gop *GopCache) {
	/*
		// 1 发送Metadata
		s.log.Println("<== send Metadata")
		if gop.MetaData != nil {
			s.FrameChan <- *(gop.MetaData)
		}

		// 2 发送VideoHeader
		s.log.Println("<== send VideoHeader")
		if gop.VideoHeader != nil {
			s.FrameChan <- *(gop.VideoHeader)
		}

		// 3 发送AudioHeader
		s.log.Println("<== send AudioHeader")
		if gop.AudioHeader != nil {
			s.FrameChan <- *(gop.AudioHeader)
		}

		// 4 发送MediaData(包含最后收到的数据)
		s.log.Printf("<== send MediaData, Len=%d", gop.MediaData.Len())
		var i int
		var c *Chunk
		for e := gop.MediaData.Front(); e != nil; e = e.Next() {
			c = (e.Value).(*Chunk)
			s.FrameChan <- *c
			s.log.Println("<== GopCacheSendRtmp", i, c.DataType, c.MsgLength, c.Timestamp)
			i++
		}
	*/
	s.log.Println("<== GopCacheSendRtmp() ok")
}

func GopCacheSendRtmp(p, s *RtmpStream, gop *GopCache) {
	//goplocks.Lock()
	var chunkNum, videoChunkNum, audioChunkNum int
	var lastChunk, videoChunk, audioChunk Chunk

	for e := gop.MediaData.Front(); e != nil; e = e.Next() {
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

	// 1 发送Metadata
	v, ok := gop.MetaData.Load(p.Key)
	if ok == false {
		s.log.Printf("meta data is not exist %v", p.Key)
	} else {
		s.log.Println("<== send Metadata")
		if chunkNum > 0 {
			v1 := v.(*Chunk)
			v1.Timestamp = lastChunk.Timestamp
			s.PlayChan <- v1
		} else {
			//TODO: timestamp use original or set 0?
			s.PlayChan <- v.(*Chunk)
		}
	}

	// 2 发送VideoHeader
	v, ok = gop.VideoHeader.Load(p.Key)
	if ok == false {
		s.log.Printf("video header is not exist %v", p.Key)
	} else {
		s.log.Println("<== send VideoHeader")
		v1 := v.(*Chunk)
		if videoChunkNum > 0 {
			v1.Timestamp = videoChunk.Timestamp
			s.PlayChan <- v1
		} else {
			s.PlayChan <- v1
		}
	}

	// 3 发送AudioHeader
	v, ok = gop.AudioHeader.Load(p.Key)
	if ok == false {
		s.log.Printf("audio header is not exist %v", p.Key)
	} else {
		s.log.Println("<== send AudioHeader")
		v1 := v.(*Chunk)
		if audioChunkNum > 0 {
			v1.Timestamp = audioChunk.Timestamp
			s.PlayChan <- v1
		} else {
			//if gop no audio, it will use first gop video packet timestamp to audio header
			if videoChunkNum > 0 {
				v1.Timestamp = videoChunk.Timestamp
			}
			s.PlayChan <- v1
		}
	}

	// 4 发送MediaData(包含最后收到的数据)
	s.log.Println("<== send MediaData, GopCache.MediaData len", gop.MediaData.Len())
	i := 0
	var c *Chunk
	for e := gop.MediaData.Front(); e != nil; e = e.Next() {
		c = (e.Value).(*Chunk)
		s.PlayChan <- c
		s.log.Println("<== GopCacheSendRtmp", i, c.DataType, c.MsgLength, c.Timestamp)
		i++
	}
	//goplocks.Unlock()
	s.log.Println("<== GopCacheSendRtmp() ok")
}
