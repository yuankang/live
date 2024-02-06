package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"strings"
)

const (
	Amf0MarkerNumber        = 0x00 // 1byte类型，8byte数据(double类型)
	Amf0MarkerBoolen        = 0x01 // 1byte类型, 1byte数据
	Amf0MarkerString        = 0x02 // 1byte类型，2byte长度，Nbyte数据
	Amf0MarkerObject        = 0x03 // 1byte类型，然后是N个kv键值对，最后00 00 09; kv键值对: key为字符串(不需要类型标识了) 2byte长度 Nbyte数据, value可以是任意amf数据类型 包括object类型
	Amf0MarkerMovieClip     = 0x04
	Amf0MarkerNull          = 0x05 // 1byte类型，没有数据
	Amf0MarkerUndefined     = 0x06
	Amf0MarkerReference     = 0x07
	Amf0MarkerEcmaArray     = 0x08 // MixedArray, 1byte类型后是4byte的kv个数, 其他和Object差不多
	Amf0MarkerObjectEnd     = 0x09
	Amf0MarkerArray         = 0x0a // StrictArray
	Amf0MarkerDate          = 0x0b
	Amf0MarkerLongString    = 0x0c
	Amf0MarkerUnSupported   = 0x0d
	Amf0MarkerRecordSet     = 0x0e
	Amf0MarkerXmlDocument   = 0x0f
	Amf0MarkerTypedObject   = 0x10
	Amf0MarkerAcmPlusObject = 0x11 // AMF3 data, Sent by Flash player 9+
)

type AmfInfo struct {
	CmdName        string
	TransactionId  float64 //事务id
	App            string  `amf:"app" json:"app"`
	FlashVer       string  `amf:"flashVer" json:"flashVer"` //FlashPlayer版本号
	SwfUrl         string  `amf:"swfUrl" json:"swfUrl"`     //swf文件源地址
	TcUrl          string  `amf:"tcUrl" json:"tcUrl"`       //服务器url
	Fpad           bool    `amf:"fpad" json:"fpad"`         //是否使用代理
	AudioCodecs    int     `amf:"audioCodecs" json:"audioCodecs"`
	VideoCodecs    int     `amf:"videoCodecs" json:"videoCodecs"`
	VideoFunction  int     `amf:"videoFunction" json:"videoFunction"`
	PageUrl        string  `amf:"pageUrl" json:"pageUrl"` //swf文件所加载的网页url
	ObjectEncoding int     //amf编码方法, 0:AMF0, 3:AMF3
	Type           string
	PublishName    string  // 可能带参数 cctv1?app=pgm0&pbto=xxx
	PublishType    string  // live/ record/ append
	StreamId       string  // play cmd use
	Start          float64 // play cmd use
	Duration       float64 // play cmd use, live is -1
	Reset          bool    // play cmd use
}

type Object map[string]interface{}

// AMF是Adobe开发的二进制通信协议, 有两种版本 AMF0 和 AMF3
// 序列化转结构化 AmfUnmarshal();  结构化转序列化 AmfMarshal();
func AmfHandle(s *RtmpStream, c *Chunk) error {
	r := bytes.NewReader(c.MsgData)
	vs, err := AmfUnmarshal(s, r) // 序列化转结构化
	//这个 && 不能动 ???
	if err != nil && err != io.EOF {
		s.log.Println(err)
		return err
	}
	s.log.Printf("Amf Unmarshal %#v", vs)

	switch vs[0].(string) {
	case "connect":
		if err = AmfConnectHandle(s, vs); err != nil {
			return err
		}
		if err = AmfConnectResponse(s, c); err != nil {
			return err
		}
	case "releaseStream":
		return nil
	case "FCPublish":
		return nil
	case "createStream":
		if err = AmfCreateStreamHandle(s, vs); err != nil {
			return err
		}
		if err = AmfCreateStreamResponse(s, c); err != nil {
			return err
		}
	case "publish":
		if err = AmfPublishHandle(s, vs); err != nil {
			return err
		}
		if err = AmfPublishResponse(s, c); err != nil {
			return err
		}
		s.IsPublisher = true
		s.MessageHandleDone = true
	case "play":
		if err = AmfPlayHandle(s, vs); err != nil {
			return err
		}
		if err = AmfPlayResponse(s, c); err != nil {
			return err
		}
		s.IsPublisher = false
		s.MessageHandleDone = true
	case "FCUnpublish":
		return nil
	//case "deleteStream":
	case "getStreamLength": //play交互出现, 获取stream的时间长度
		return nil
	case "_result": //作为客户端时, 对方的返回值
		s.log.Printf("%s", vs[0].(string))
		s.MessageHandleDone = true
	case "onStatus": //作为客户端时, 对方的返回值
		//Amf Unmarshal []interface {}{"onStatus", 0, interface {}(nil), main.Object{"code":"NetStream.Play.PublishNotify", "description":"Started playing notify.", "level":"status"}}
		s.log.Printf("%s", vs[0].(string))
		if strings.Contains(vs[0].(string), "PublishNotify") {
			s.MessageHandleDone = true
		}
	default:
		err = fmt.Errorf("Untreated AmfCmd %s", vs[0].(string))
		s.log.Println(err)
		return err
	}
	return nil
}

/*************************************************/
/* amf decode
/*************************************************/
func AmfUnmarshal(s *RtmpStream, r io.Reader) ([]interface{}, error) {
	var vs []interface{}
	var v interface{}
	var err error

	for {
		//s.log.Println("------")
		v, err = AmfDecode(s, r)
		if err != nil {
			if err != io.EOF {
				s.log.Println(err)
			}
			break
		}
		vs = append(vs, v)
	}
	return vs, err
}

func AmfDecode(s *RtmpStream, r io.Reader) (interface{}, error) {
	t, err := ReadUint8(r)
	if err != nil {
		if err != io.EOF {
			s.log.Println(err)
		}
		return nil, err
	}
	//s.log.Println("AmfType", t)

	switch t {
	case Amf0MarkerNumber:
		return Amf0DecodeNumber(s, r)
	case Amf0MarkerBoolen:
		return Amf0DecodeBoolean(s, r)
	case Amf0MarkerString:
		return Amf0DecodeString(s, r)
	case Amf0MarkerObject:
		return Amf0DecodeObject(s, r)
	case Amf0MarkerNull:
		return Amf0DecodeNull(s, r)
	case Amf0MarkerEcmaArray:
		return Amf0DecodeEcmaArray(s, r)
	}
	err = fmt.Errorf("Untreated AmfType %d", t)
	s.log.Println(err)
	return nil, err
}

func Amf0DecodeNumber(s *RtmpStream, r io.Reader) (float64, error) {
	var ret float64
	err := binary.Read(r, binary.BigEndian, &ret)
	if err != nil {
		if err != io.EOF {
			s.log.Println(err)
		}
		return 0, err
	}
	//s.log.Println(ret)
	return ret, nil
}

func Amf0DecodeBoolean(s *RtmpStream, r io.Reader) (bool, error) {
	var ret bool
	err := binary.Read(r, binary.BigEndian, &ret)
	if err != nil {
		if err != io.EOF {
			s.log.Println(err)
		}
		return false, err
	}
	//s.log.Println(ret)
	return ret, nil
}

func Amf0DecodeString(s *RtmpStream, r io.Reader) (string, error) {
	len, err := ReadUint32(r, 2, BE)
	if err != nil {
		if err != io.EOF {
			s.log.Println(err)
		}
		return "", err
	}

	ret, _ := ReadString(r, len)
	//s.log.Println(len, ret)
	return ret, nil
}

func Amf0DecodeObject(s *RtmpStream, r io.Reader) (Object, error) {
	var len uint32
	var key string
	var v interface{}
	var err error
	ret := make(Object)

	for {
		// 00 00 09
		len, err = ReadUint32(r, 2, BE)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		if len == 0 {
			ReadUint8(r)
			break
		}

		key, err = ReadString(r, len)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}

		v, err = AmfDecode(s, r)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
		ret[key] = v
	}
	//s.log.Printf("%#v", ret)
	return ret, nil
}

func Amf0DecodeNull(s *RtmpStream, r io.Reader) (interface{}, error) {
	return nil, nil
}

func Amf0DecodeEcmaArray(s *RtmpStream, r io.Reader) (Object, error) {
	//len, err := ReadUint32(r, 4, BE)
	_, err := ReadUint32(r, 4, BE)
	if err != nil {
		if err != io.EOF {
			s.log.Println(err)
		}
		return nil, err
	}
	//s.log.Println("Amf EcmaArray len", len)

	ret, err := Amf0DecodeObject(s, r)
	if err != nil {
		s.log.Println(err)
		if err != io.EOF {
			s.log.Println(err)
		}
		return nil, err
	}
	//s.log.Printf("%#v", ret)
	return ret, nil
}

/*************************************************/
/* amf encode
/*************************************************/
func AmfMarshal(s *RtmpStream, args ...interface{}) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	var err error
	for _, v := range args {
		_, err = AmfEncode(s, buf, v)
		if err != nil {
			s.log.Println(err)
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func AmfEncode(s *RtmpStream, buf io.Writer, v interface{}) (int, error) {
	if v == nil {
		return Amf0EncodeNull(s, buf)
	}

	val := reflect.ValueOf(v)
	//s.log.Println(v, val.Kind())
	switch val.Kind() {
	case reflect.String:
		return Amf0EncodeString(s, buf, val.String(), true)
	case reflect.Bool:
		return Amf0EncodeBool(s, buf, val.Bool())
	case reflect.Int:
		return Amf0EncodeNumber(s, buf, float64(val.Int()))
	case reflect.Uint32:
		return Amf0EncodeNumber(s, buf, float64(val.Uint()))
	case reflect.Float32, reflect.Float64:
		return Amf0EncodeNumber(s, buf, float64(val.Float()))
	case reflect.Map:
		return Amf0EncodeObject(s, buf, v.(Object))
	}
	err := fmt.Errorf("Untreated Amf0Marker %s", val.Kind())
	s.log.Println(err)
	return 0, err
}

func Amf0EncodeNull(s *RtmpStream, buf io.Writer) (int, error) {
	b := []byte{Amf0MarkerNull}
	n, err := buf.Write(b)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	return n, nil
}

func Amf0EncodeString(s *RtmpStream, buf io.Writer, v string, wType bool) (int, error) {
	var n int
	if wType {
		b := []byte{Amf0MarkerString}
		buf.Write(b)
		n += 1
	}

	l := uint32(len(v))
	WriteUint32(buf, BE, l, 2)
	n += 2

	m, err := buf.Write([]byte(v))
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	return n + m, nil
}

func Amf0EncodeBool(s *RtmpStream, buf io.Writer, v bool) (int, error) {
	var n int
	b := []byte{Amf0MarkerBoolen}
	buf.Write(b)
	n += 1

	b[0] = 0x00
	if v {
		b[0] = 0x01
	}

	m, err := buf.Write(b)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	return n + m, nil
}

func Amf0EncodeNumber(s *RtmpStream, buf io.Writer, v float64) (int, error) {
	var n int
	b := []byte{Amf0MarkerNumber}
	buf.Write(b)
	n += 1

	err := binary.Write(buf, binary.BigEndian, &v)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	return n + 8, nil
}

func Amf0EncodeObject(s *RtmpStream, buf io.Writer, o Object) (int, error) {
	var n, m int
	var err error
	b := []byte{Amf0MarkerObject}
	buf.Write(b)
	n += 1

	for k, v := range o {
		m, err = Amf0EncodeString(s, buf, k, false)
		if err != nil {
			s.log.Println(err)
			return 0, err
		}
		n += m

		m, err = AmfEncode(s, buf, v)
		if err != nil {
			s.log.Println(err)
			return 0, err
		}
		n += m
	}

	m, err = Amf0EncodeString(s, buf, "", false)
	if err != nil {
		s.log.Println(err)
		return 0, err
	}
	n += m

	b[0] = Amf0MarkerObjectEnd
	buf.Write(b)
	return n + 1, nil
}

/*************************************************/
/* amf command handle
/*************************************************/
func CreateMessage(TypeId, Len uint32, Data []byte) Chunk {
	// fmt: 控制Message Header的类型, 0表示11字节, 1表示7字节, 2表示3字节, 3表示0字节
	// csid: 0表示2字节形式, 1表示3字节形式, 2用于协议控制消息和命令消息, 3-65599表示块流id
	return Chunk{
		Fmt:         0,
		Csid:        2,
		Timestamp:   0,
		MsgLength:   Len,
		MsgTypeId:   TypeId,
		MsgStreamId: 0,
		MsgData:     Data,
	}
}

func AmfConnectHandle(s *RtmpStream, vs []interface{}) error {
	for _, v := range vs {
		switch v.(type) {
		case string:
			s.AmfInfo.CmdName = v.(string)
		case float64:
			s.AmfInfo.TransactionId = v.(float64)
		case Object:
			o := v.(Object)
			if i, ok := o["app"]; ok {
				s.AmfInfo.App = i.(string)
			}
			if i, ok := o["flashVer"]; ok {
				s.AmfInfo.FlashVer = i.(string)
			}
			if i, ok := o["tcUrl"]; ok {
				s.AmfInfo.TcUrl = i.(string)
			}
			if i, ok := o["objectEncoding"]; ok {
				s.AmfInfo.ObjectEncoding = int(i.(float64))
			}
			if i, ok := o["type"]; ok {
				s.AmfInfo.Type = i.(string)
			}
		}
	}
	//s.log.Printf("%#v", s.AmfInfo)
	return nil
}

func AmfConnectResponse(s *RtmpStream, c *Chunk) error {
	// 1 Window Acknowledge Size
	// 2 Set Peer BandWidth
	// 3 Set ChunkSize
	// 4 User Control(StreamBegin)
	// 5 Command Message (_result- connect response)
	s.log.Println("<---- Set Window Acknowledge Size = 2500000")
	d := Uint32ToByte(2500000, nil, BE) // 33554432
	rc := CreateMessage(MsgTypeIdWindowAckSize, 4, d)
	err := MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- Set Peer BandWidth = 2500000")
	d = make([]byte, 5)
	Uint32ToByte(2500000, d[:4], BE)
	d[4] = 2 // Limit Type: 0 is Hard, 1 is Soft, 2 is Dynamic
	rc = CreateMessage(MsgTypeIdSetPeerBandwidth, 5, d)
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	// A是服务器(接收方)		B是推流器(发送方)
	// A.ChunkSize(收)			B.ChunkSize(收)
	// A.RemoteChunkSize(发)	B.RemoteChunkSize(发)
	// A.ChunkSize 和 B.RemoteChunkSize 是一对
	// B.ChunkSize 和 A.RemoteChunkSize 是一对
	// 我们是A 此处 Set ChunkSize, 表示A想用1024字节发送
	// A.RemoteChunkSize = B.ChunkSize = 1024
	// srs中 接收用in_chunk_size, 发送用out_chunk_size
	// srs中 in_chunk_size  相当于 B.ChunkSize
	// srs中 out_chunk_size 相当于 B.RemoteChunkSize
	// 此处 Set ChunkSize后, srs的 in_chunk_size = 1024
	s.log.Printf("<---- Set ChunkSize = %d", conf.Rtmp.ChunkSize)
	d = Uint32ToByte(conf.Rtmp.ChunkSize, nil, BE)
	rc = CreateMessage(MsgTypeIdSetChunkSize, 4, d)
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}
	s.RemoteChunkSize = conf.Rtmp.ChunkSize

	s.log.Println("<---- ConnectMessageResponse")
	rsps := make(Object)
	rsps["fmsVer"] = "FMS/3,0,1,123"
	rsps["capabilities"] = 31
	info := make(Object)
	info["level"] = "status"
	info["code"] = "NetConnection.Connect.Success"
	info["description"] = "Connection succeeded."
	info["objectEncoding"] = s.AmfInfo.ObjectEncoding
	s.log.Println(rsps, info)

	d, _ = AmfMarshal(s, "_result", 1, rsps, info) // 结构化转序列化
	//s.log.Println(d)

	rc = CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err = MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func AmfCreateStreamHandle(s *RtmpStream, vs []interface{}) error {
	for _, v := range vs {
		switch v.(type) {
		case string:
			s.AmfInfo.CmdName = v.(string)
		case float64:
			s.AmfInfo.TransactionId = v.(float64)
		}
	}
	//s.log.Printf("%#v", s.AmfInfo)
	return nil
}

func AmfCreateStreamResponse(s *RtmpStream, c *Chunk) error {
	s.log.Println("<---- CreateStreamMessageResponse")
	d, _ := AmfMarshal(s, "_result", s.AmfInfo.TransactionId, nil, c.MsgStreamId)
	//s.log.Println(d)

	rc := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err := MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func AmfPublishHandle(s *RtmpStream, vs []interface{}) error {
	for k, v := range vs {
		switch v.(type) {
		case string:
			if k == 0 {
				s.AmfInfo.CmdName = v.(string)
			} else if k == 3 {
				s.AmfInfo.PublishName = v.(string)
				ss := strings.Split(s.AmfInfo.PublishName, "?")
				s.AmfInfo.StreamId = ss[0]
			} else if k == 4 {
				s.AmfInfo.PublishType = v.(string)
			}
		case float64:
			s.AmfInfo.TransactionId = v.(float64)
		case Object:
			s.log.Println("Untreated AmfType")
		}
	}
	//s.log.Printf("%#v", s.AmfInfo)
	return nil
}

func AmfPublishResponse(s *RtmpStream, c *Chunk) error {
	s.log.Println("<---- PublishMessageResponse")
	info := make(Object)
	info["level"] = "status"
	info["code"] = "NetStream.Publish.Start"
	info["description"] = "Start publising."
	s.log.Printf("%#v", info)

	d, _ := AmfMarshal(s, "onStatus", 0, nil, info) // 结构化转序列化
	//s.log.Println(d)

	rc := CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err := MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}

func AmfPlayHandle(s *RtmpStream, vs []interface{}) error {
	for k, v := range vs {
		switch v.(type) {
		case string:
			if k == 0 {
				s.AmfInfo.CmdName = v.(string)
			} else if k == 3 {
				s.AmfInfo.PublishName = v.(string)
				ss := strings.Split(s.AmfInfo.PublishName, "?")
				s.AmfInfo.StreamId = ss[0]
			}
		case float64:
			if k == 1 {
				s.AmfInfo.TransactionId = v.(float64)
			} else if k == 4 {
				s.AmfInfo.Start = v.(float64)
			} else if k == 5 {
				s.AmfInfo.Duration = v.(float64)
			}
		case Object:
			s.log.Println("Untreated AmfType")
		case bool:
			s.AmfInfo.Reset = v.(bool)
		}
	}
	s.log.Printf("%#v", s.AmfInfo)
	return nil
}

// User Control Message EventType:
// StreamBegin		(=0)
// StreamEOF		(=1)
// StreamDry		(=2)
// SetBufferLength	(=3)
// StreamIsRecorded	(=4)
// PingRequest		(=6)
// PingResponse		(=5)
func AmfPlayResponse(s *RtmpStream, c *Chunk) error {
	s.log.Println("<---- PlayMessageResponse")
	// 1 send User Control Message EventType = 4
	// 2 send User Control Message EventType = 0
	// 3 Command Message(onStatus-play reset)
	// 4 Command Message(onStatus-play start)
	// 5 Command Message(onStatus-data start)
	// 6 Command Message(onStatus-play PublishNotify)

	s.log.Println("<---- SendUserControl(StreamIsRecorded)")
	d := make([]byte, 6)
	Uint16ToByte(4, d[0:2], BE) // EventType
	Uint32ToByte(1, d[2:], BE)  // StreamId
	rc := CreateMessage(MsgTypeIdUserControl, 6, d)
	rc.MsgStreamId = 1
	err := MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- SendUserControl(StreamBegin)")
	//d = make([]byte, 6)
	Uint16ToByte(0, d[0:2], BE) // EventType
	Uint32ToByte(1, d[2:], BE)  // StreamId
	rc = CreateMessage(MsgTypeIdUserControl, 6, d)
	rc.MsgStreamId = 1
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- Send onStatus-play reset")
	info := make(Object)
	info["level"] = "status"
	info["code"] = "NetStream.Play.Reset"
	info["description"] = "Playing and resetting stream."
	d, _ = AmfMarshal(s, "onStatus", 0, nil, info) // 结构化转序列化
	//s.log.Println(d)
	rc = CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- Send onStatus-play start")
	//info = make(Object)
	info["level"] = "status"
	info["code"] = "NetStream.Play.Start"
	info["description"] = "Started playing stream."
	d, _ = AmfMarshal(s, "onStatus", 0, nil, info) // 结构化转序列化
	//s.log.Println(d)
	rc = CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- Send onStatus-data start")
	//info = make(Object)
	info["level"] = "status"
	info["code"] = "NetStream.Data.Start"
	info["description"] = "Started playing stream."
	d, _ = AmfMarshal(s, "onStatus", 0, nil, info) // 结构化转序列化
	//s.log.Println(d)
	rc = CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err = MessageSplit(s, &rc, false)
	if err != nil {
		s.log.Println(err)
		return err
	}

	s.log.Println("<---- Send onStatus-play PublishNotify")
	//info = make(Object)
	info["level"] = "status"
	info["code"] = "NetStream.Play.PublishNotify"
	info["description"] = "Started playing notify."
	d, _ = AmfMarshal(s, "onStatus", 0, nil, info) // 结构化转序列化
	//s.log.Println(d)
	rc = CreateMessage(MsgTypeIdCmdAmf0, uint32(len(d)), d)
	rc.Csid = c.Csid
	rc.MsgStreamId = c.MsgStreamId
	err = MessageSplit(s, &rc, true)
	if err != nil {
		s.log.Println(err)
		return err
	}
	return nil
}
