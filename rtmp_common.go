package main

import (
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
	//Publishers      sync.Map    // map[string]*Stream, key:App_StreamId
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
