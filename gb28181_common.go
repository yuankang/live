package main

import (
	"container/list"
	"fmt"
	"sync"
	"utils"
)

type Gb28181Stream struct {
}

//streamlog/GSP3bnx69BgxI-guCc0oo88s/publish_rtp_20230406.log
//streamlog/GSP3bnx69BgxI-guCc0oo88s/play_rtmp_192.168.109.57:50471.log
func NewGb28181Stream(key string, rqst GbRqst) (*Stream, error) {
	var s Stream
	s.Key = key
	s.Type = "GbPub"
	s.GbRqst = rqst

	s.LogFn = fmt.Sprintf("%s/%s/GbPub_%s.log", conf.Log.StreamLogPath, rqst.StreamId, utils.GetYMD())
	s.log, s.LogFp, _ = StreamLogCreate(s.LogFn)
	return &s, nil
}

//StreamName	为流的中文名称, 如: 北海市分公司前台门口
//StreamId		流的唯一标识, 如:GSP3bnx69BgxI-avEc0oE4C4
//Ssrc			10位整数, 设备唯一表示, 如:0108000710

//GB28181中设备是指 摄像头(ipc) 或 网络视频录像机(nvr)
//ipc只能推一路流, nvr可以推多路流, 流媒体服务器只需关注流信息
//每路流都有一个唯一的streamId和一个唯一的ssrc
//cc下发任务	必带streamId 不定ssrc, del接口就没有
//设备推流上来	必无streamId 必带ssrc
//Stream代表每路流, StreamMap是sync.Map类型全局变量
//map的key用ssrc时     接收音视频数据方便, 但是处理cc任务不方便
//map的key用streamId时 接收音视频数据不方便, 但是处理cc任务方便
//由于map不能同时有2个key
//所以最总 使用两个map来存放 并且 增加互相查找的函数
//StreamMap key:StreamId, val:Stream
//SsrcMap   key:uint32,	  val:*Stream
var (
	StreamMap sync.Map
	SsrcMap   sync.Map
)

func SsrcFindStream(ssrc uint32) (*Stream, error) {
	var err error
	v, ok := SsrcMap.Load(ssrc)
	if ok == false {
		err = fmt.Errorf("unkonw ssrc %.10d", ssrc)
		return nil, err
	}
	s := v.(*Stream)
	return s, nil
}

//视频frame 由多个RtpPacket组成, 1个RtpPacket就是1个rtp包中的视频数据
//接收到rtp包 先把frameSeg放到 FrameSeg中, 等一帧数据够了, 在组装成frame
//这样可以精确的给frame分配空间, 因为收到第一个frameSeg时, 不知道整个frame大小
//组装的frame放入到GopCache里, 并且把frame发送给 现有的播放连接
//新播放连接, 先发送GopCache里的数据, 然后等待发送下个frame
type GbPub struct {
	GbRqst //cc发送的国标GB1818流的任务信息

	RtpPktList        list.List  //缓存收到的rtp包
	RtpPktListHeadSeq uint16     //rtplist首节点的rtpseq值
	RtpPktListTailSeq uint16     //rtplist尾节点的rtpseq值
	RtpPktListMutex   sync.Mutex //rtplist锁, 防止插入和删除并发
	RtpPktNeedSeq     uint16     //下一个rtp包的seq
	RtpPktCrtTs       int64      //帧的第一个rtp包的时间戳 CurrentTimestamp

	RtpSeqNeed  uint16   //期待的rtp包序号
	RtpSeqWait  uint16   //期待的rtp包序号, 等了多少次
	RtpTsCurt   uint32   //当前rtp包的时间戳
	RtpPkgCache sync.Map //期待rtp序号为10的包, 来了序号为11的包, 要先缓存上

	AhcFlag bool //AudioHeaderChangeFlag
	VhcFlag bool //VideoHeaderChangeFlag
}

/*
## RtpTcp数据封装格式
rtpTcp包
rtp包长度		2字节
rtpheader 		12字节, 有数据类型, 有音视频各自的序号, 有时间戳, 有ssrc
psHeader		14字节, 没什么重要数据
psSysHeader		15字节, 没什么重要数据
psSysMap		10字节, 有streamMap, 有crc32
streamMap		2字节数据长度, 有音视频编码类型, 有流描述信息
pesHeader		14/19字节, 区分音视频数据, 有pes包长度, 有optPesHeader
optPesHeader	3字节, 有pts和dts标志位, 有时间戳
naluHeader		h264是1字节, h265是2字节, 有naluType
vps
pesHeader
optPesHeader
naluHeader
sps
pesHeader
optPesHeader
naluHeader
pps
pesHeader
optPesHeader
naluHeader
sei
pesHeader
optPesHeader
naluHeader
idr
*/
