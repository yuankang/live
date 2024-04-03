package main

import "fmt"

/*************************************************/
/* message和chunk的转换
/*************************************************/
//1+3+4+3=11Byte
type RtmpMessage struct {
	MsgType   uint8  //1Byte
	Length    uint32 //3Btye, Data长度
	Timestamp uint32 //4Byte
	StreamId  uint32 //3Byte, chunk和msg互转 根据这个来, 音视频用不同的id
	Data      []byte
}

//rtmp协议分析(Message 消息，Chunk分块) (必看)
//https://blog.csdn.net/m0_37599645/article/details/116082210
//rtmp收发数据时不是以message为单位, 而是以chunk为单位
//每个Chunk带有MessageID(Chunk Stream ID)代表属于哪个Message,
//接受端按照这个id来将chunk组装成Message
//为什么RTMP要将Message拆分成不同的Chunk呢？避免优先级低的消息持续发送阻塞优先级高的数据
//Chunk的默认大小是128字节，在传输过程中，通过一个叫做Set Chunk Size的控制信息可以设置Chunk数据量的最大值
//大一点的Chunk减少了计算每个chunk的时间从而减少了CPU的占用率，但是它会占用更多的时间在发送上，尤其是在低带宽的网络情况下，很可能会阻塞后面更重要信息的传输。
//小一点的Chunk可以减少这种阻塞问题，但小的Chunk会引起过多额外的信息（Chunk中的Header），少量多次的传输也可能会造成发送的间断导致不能充分利用高带宽的优势，因此并不适合在高比特率的流中传输。
//在实际发送时应对要发送的数据用不同的Chunk Size去尝试，通过抓包分析等手段得出合适的Chunk大小，并且在传输过程中可以根据当前的带宽信息和实际信息的大小动态调Chunk的大小，从而尽量提高CPU的利用率并减少信息的阻塞机率。
func MessageMerge(s *Stream, c *Chunk) (Chunk, error) {
	var bh, fmt, csid uint32 // basic header
	var err error
	var id0, id1 uint32
	var sc Chunk
	var ok bool
	//i := 0
	for {
		//s.log.Println("----> chunk", i)
		bh, err = ReadUint32(s.Conn, 1, BE)
		if err != nil {
			s.log.Println(err)
			return sc, err
		}
		//fmt = (bh >> 6) & 0x03
		fmt = bh >> 6
		csid = bh & 0x3f // [0, 63]
		// csid 6bit,  表示范围[0, 63],     0 1 2 有特殊用处
		// csid 8bit,  表示范围[64, 319],   [0, 255]+64
		// csid 16bit, 表示范围[64, 65599], [0, 65535]+64
		// csid 应该优先使用最小字节表示,   [320, 65599]
		// csid 0表示2字节形式, csid 1表示3字节形式
		// csid 2用于协议的控制消息和命令消息
		// csid [3, 65599]表示块流id, 共65597个
		switch csid {
		case 0:
			id0, err = ReadUint32(s.Conn, 1, LE) // [0, 255]
			if err != nil {
				s.log.Println(err)
				return sc, err
			}
			csid = 64 + id0
		case 1:
			id0, err = ReadUint32(s.Conn, 1, LE) // [0, 65535]
			if err != nil {
				s.log.Println(err)
				return sc, err
			}
			id1, err = ReadUint32(s.Conn, 1, LE) // [0, 65535]
			if err != nil {
				s.log.Println(err)
				return sc, err
			}
			csid = 64 + id0 + id1*256 // [64, 65599]
		}
		//s.log.Printf("fmt:%d, csid:%d", fmt, csid)

		// 通常 一个rtmp连接 就是一路流
		// 一路流 可以有 多种数据类型，音频 视频 命令
		// 通常 不同数据类型的csid不同, 相当于不同通道(轨道)
		// csid 用于区分通道, MsgTypeId 用于区分数据类型
		sc, ok = s.Chunks[csid]
		if ok == false {
			sc = Chunk{}
		}

		//if i == 0 {
		//sc.FmtFirst = fmt
		//}
		//i++

		sc.Fmt = fmt
		sc.Csid = csid
		if err = ChunkAssemble(s, &sc); err != nil {
			s.log.Println(err)
			return sc, err
		}

		s.Chunks[csid] = sc
		if sc.Full {
			//s.log.Println("chunk Full")
			if c != nil {
				*c = sc
			}
			return sc, nil
		}
	}
}

func MessageSplit(s *Stream, c *Chunk, flush bool) error {
	var err error
	if c == nil {
		err = fmt.Errorf("error: chunk point is nil")
		s.log.Println(err)
		return err
	}

	var i, si, ei, div, sLen uint32
	n := c.MsgLength / s.RemoteChunkSize
	m := c.MsgLength % s.RemoteChunkSize
	if m != 0 {
		n++
	}
	//s.log.Printf("send times=%d, MsgLen=%d, RmtChunkSize=%d", n, c.MsgLength, s.RemoteChunkSize)

	c.Fmt = 0
	MsgDataLen := len(c.MsgData)
	for i = 0; i < n; i++ {
		if i != 0 {
			c.Fmt = 3
		}

		if err = ChunkHeaderAssemble(s, c); err != nil {
			s.log.Println(err)
			return err
		}

		//每次发送大小不能超过 RemoteChunkSize
		si = i * s.RemoteChunkSize
		div = uint32(MsgDataLen) - si
		if div > s.RemoteChunkSize {
			ei = si + s.RemoteChunkSize
			sLen += s.RemoteChunkSize
		} else {
			ei = si + div
			sLen += div
		}
		if _, err = s.Conn.Write(c.MsgData[si:ei]); err != nil {
			s.log.Println(err)
			return err
		}

		if sLen >= c.MsgLength {
			break
		}
	}
	if flush == true {
		s.Conn.Flush()
	}
	return nil
}

func MessageTypeCheck(c *Chunk) {
	switch c.MsgTypeId {
	case MsgTypeIdCmdAmf0: // 20
		c.DataType = "CmdAmf0"
	case MsgTypeIdAudio: // 8
		c.DataType = "AudioAacFrame"
		AACPacketType := c.MsgData[1]
		if AACPacketType == 0 {
			c.DataType = "AudioHeader"
		}
	case MsgTypeIdVideo: // 9
		c.DataType = "VideoFrame"
		FrameType := c.MsgData[0] >> 4 // 4bit
		if FrameType == 1 {
			c.DataType = "VideoKeyFrame"
		} else if FrameType == 2 {
			c.DataType = "VideoInterFrame"
		}
		AVCPacketType := c.MsgData[1] // 8bit
		if AVCPacketType == 0 {
			c.DataType = "VideoHeader"
		}
	case MsgTypeIdDataAmf3, MsgTypeIdDataAmf0: // 15 18
		c.DataType = "DataAmfx"
	case MsgTypeIdUserControl: //用户控制消息
		c.DataType = "UserControl"
	default:
		c.DataType = "undefined"
	}
}

/*************************************************/
/* 建连阶段message交互
/*************************************************/
func SetRemoteChunkSize(s *Stream) error {
	d := Uint32ToByte(conf.Rtmp.ChunkSize, nil, BE)
	rc := CreateMessage(MsgTypeIdSetChunkSize, 4, d)
	err := MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}
	s.RemoteChunkSize = conf.Rtmp.ChunkSize
	return nil
}

//{"connect", 1, main.Object{"app":"SP3bnx69BgxI", "swfUrl":"rtmp://127.0.0.1:1935/SP3bnx69BgxI", "tcUrl":"rtmp://127.0.0.1:1935/SP3bnx69BgxI", "type":"nonprivate"}}
//ffmpeg: {"connect", 1, main.Object{"app":"SPq3pr6f6kNa", "audioCodecs":4071, "capabilities":15, "flashVer":"LNX 9,0,124,2", "fpad":false, "tcUrl":"rtmp://192.168.16.164:1935/SPq3pr6f6kNa", "videoCodecs":252, "videoFunction":1}}
func SendConnMsg(s *Stream) error {
	s.log.Println("<== Send Connect Message")

	info := make(Object)
	info["app"] = s.App
	info["type"] = "nonprivate"
	//info["flashVer"] = "FMS.3.1"
	info["flashVer"] = "yuankang"
	info["tcUrl"] = fmt.Sprintf("rtmp://127.0.0.1:1935/%s", s.App)
	s.log.Printf("%#v", info)

	d, _ := AmfMarshal(s, "connect", 1, info)
	//s.log.Printf("%x", d)

	msg := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	msg.Csid = 3
	msg.MsgStreamId = 0
	err := MessageSplit(s, &msg, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

//{"createStream", 2, interface {}(nil)}
func SendCreateStreamMsg(s *Stream) error {
	s.log.Println("<== Send CreateStream Message")

	d, _ := AmfMarshal(s, "createStream", 2, nil)
	//s.log.Printf("%x", d)

	msg := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	msg.Csid = 3
	msg.MsgStreamId = 0
	err := MessageSplit(s, &msg, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

//{"publish", 3, interface {}(nil), "GSP3bnx69BgxI-guCc0oo88s?app=slivegateway&pbto=30", "live"}
func SendPublishMsg(s *Stream) error {
	s.log.Println("<== Send Publish Message")

	//GSPg5ol5nMd2O-n7tt1qe18E?app=slivegateway&pbto=30
	PubName := fmt.Sprintf("%s?app=slivegateway&pbto=30", s.StreamId)
	//PubName := fmt.Sprintf("%s?app=slivegateway&pbto=30&%s", s.StreamId, BackDoor)
	PubType := s.App

	d, _ := AmfMarshal(s, "publish", 3, nil, PubName, PubType)
	//s.log.Printf("%x", d)

	msg := CreateMessage(MsgTypeIdDataAmf0, uint32(len(d)), d)
	msg.Csid = 3
	msg.MsgStreamId = 0
	err := MessageSplit(s, &msg, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

//{"getStreamLength", 3, interface {}(nil), "RSPq3pr6f6kNa-eQW54pIhKb"}
func SendGetStreamLengthMsg(s *Stream) error {
	s.log.Println("<== Send GetStreamLength Message")

	d, _ := AmfMarshal(s, "getStreamLength", 3, nil, s.StreamId)
	//s.log.Printf("%x", d)

	msg := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	msg.Csid = 3
	msg.MsgStreamId = 0
	err := MessageSplit(s, &msg, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

//{"play", 4, interface {}(nil), "RSPq3pr6f6kNa-eQW54pIhKb", -2000}
func SendPlayMsg(s *Stream) error {
	s.log.Println("<== Send Play Message")
	s.log.Printf("play streamid %s", s.StreamId)

	d, _ := AmfMarshal(s, "play", 3, nil, s.StreamId, -2000)
	//s.log.Printf("%x", d)

	msg := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	msg.Csid = 3
	msg.MsgStreamId = 0
	err := MessageSplit(s, &msg, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func RecvMsg(s *Stream) error {
	err := RtmpHandleMessage(s)
	if err != nil {
		s.log.Println(err)
		return err
	}
	//s.log.Println("RtmpHandleMessage ok")
	return nil
}

//rtmp发送数据的时候 message 拆分成 chunk, MessageSplit()
//rtmp接收数据的时候 chunk 组合成 message, MessageMerge()
//接收完数据 要对数据处理, MessageHandle()
func RtmpHandleMessage(s *Stream) error {
	var i uint32
	var err error
	c := &Chunk{}
	for {
		s.log.Println("----> message", i)
		i++

		if _, err = MessageMerge(s, c); err != nil {
			s.log.Println(err)
			return err
		}
		if err = MessageHandle(s, c); err != nil {
			s.log.Println(err)
			return err
		}

		err = SendAckMessage(s, c.MsgLength)
		if err != nil {
			s.log.Println(err)
			return err
		}

		if s.MessageHandleDone {
			//s.log.Println("MessageHandleDone")
			break
		}
	}
	return nil
}

func MessageHandle(s *Stream, c *Chunk) error {
	switch c.MsgTypeId {
	case MsgTypeIdSetChunkSize:
		s.ChunkSize = ByteToUint32(c.MsgData, BE)
		s.log.Println("MsgTypeIdSetChunkSize", s.ChunkSize)
	case MsgTypeIdUserControl:
		s.log.Println("MsgTypeIdUserControl")
	case MsgTypeIdWindowAckSize:
		s.RemoteWindowAckSize = ByteToUint32(c.MsgData, BE)
		s.log.Println("MsgTypeIdWindowAckSize", s.RemoteWindowAckSize)
	case MsgTypeIdDataAmf0, MsgTypeIdShareAmf0, MsgTypeIdCmdAmf0:
		if err := AmfHandle(s, c); err != nil {
			s.log.Println(err)
			return err
		}
	case MsgTypeIdSetPeerBandwidth:
		bw := ByteToUint32(c.MsgData[:4], BE)
		//Limit Type: 0 is Hard, 1 is Soft, 2 is Dynamic
		lt := c.MsgData[4]
		s.log.Printf("MsgTypeIdSetPeerBandwidth, %d, %d", bw, lt)
	default:
		err := fmt.Errorf("Untreated MsgTypeId %d", c.MsgTypeId)
		s.log.Println(err)
		return err
	}
	return nil
}

//rtmp协议要求 接收到一定数量的数据后 要给对方一个回应
func SendAckMessage(s *Stream, MsgLen uint32) error {
	s.RecvMsgLen += MsgLen
	//s.log.Printf("RecvMsgLen=%d, RemoteWindowAckSize=%d", s.RecvMsgLen, s.RemoteWindowAckSize)

	if s.RecvMsgLen >= s.RemoteWindowAckSize {
		d := Uint32ToByte(s.RecvMsgLen, nil, BE)
		rc := CreateMessage(MsgTypeIdAck, 4, d)
		err := MessageSplit(s, &rc, true)
		if err != nil {
			s.log.Println(err)
			return err
		}
		s.RecvMsgLen -= s.RemoteWindowAckSize
		//s.log.Printf("send AckSize=%d, RemainLen=%d", s.RemoteWindowAckSize, s.RecvMsgLen)
	}
	return nil
}

/*
## ffmpeg的rtmp推流过程
Client(Publish)					  Server
|          Handshaking Done          | 握手阶段
|----- Command Message(connect) ---->|-----
|<----  Window Acknowledge Size -----|  |
|<----    Set Peer BandWidth    -----|  | connect
|<----       SetChunkSize       -----|  |
|<----     Command Message      -----|  |
|    (_result - connect response)    |-----
|-----       SetChunkSize       ---->|
|-- Command Message(releaseStream) ->|
|---- Command Message(FCPublish) --->|
|--- CommandMessage(createStream) -->|-----
|<----     Command Message      -----|  | createStream
|  (_result - createStream response) |-----
|----- Command Message(publish) ---->|-----
|<- onStatusNetStream.Publish.Start--|-----
|-----  Data Message(Metadata)  ---->|
|-----        Audio Data        ---->|
|-----        Video Data        ---->|
|-----            ...           -----|

## ffmpeg的rtmp拉流过程
Client(play)					  Server
|          Handshaking Done          | 握手阶段
|----- Command Message(connect) ---->|-----
|<----  Window Acknowledge Size -----|  |
|<----    Set Peer BandWidth    -----|  | connect
|<----       SetChunkSize       -----|  |
|<----     Command Message      -----|  |
|    (_result - connect response)    |-----
|-----  Window Acknowledge Size ---->|
|--- CommandMessage(createStream) -->|-----
|<----     Command Message      -----|  | createStream
|  (_result - createStream response) |-----
|-- CommandMessage(getStreamLength)->|
|-----   CommandMessage(play)   ---->|-----
|<- User Control(StreamIsRecorded) --|  |
|<---- User Control(StreamBegin)-----|  |
|<-  onStatusNetStream.Play.Reset  --|  | play
|<-  onStatusNetStream.Play.Start  --|  |
|<-  onStatusNetStream.Data.Start  --|  |
|<- onStatusNetStream.Play.PubNtfy --|-----
|<----  Data Message(Metadata)  -----|
|<----        Audio Data        -----|
|<----        Video Data        -----|
|-----            ...           -----|
*/
