package main

import (
	"fmt"
	"log"
	"net"
	"time"
	"utils"
)

/*************************************************/
/* rtmp client
/*************************************************/
//建连+握手
func RtmpClient(ip, port string, to int) (*Stream, error) {
	addr := fmt.Sprintf("%s:%s", ip, port)
	//log.Printf("rtmp conn raddr=%s", addr)

	//c, err := net.Dial("tcp", addr)
	c, err := net.DialTimeout("tcp", addr, time.Duration(to)*time.Second)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	//log.Printf("rtmp conn laddr=%s, raddr=%s", c.LocalAddr().String(), c.RemoteAddr().String())

	rs, err := NewStream(c)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}
	rs.RemoteIp = ip
	rs.RemotePort = port

	rs.log.Println("==============================")
	rs.log.Printf("rtmp conn laddr=%s, raddr=%s", c.LocalAddr().String(), c.RemoteAddr().String())

	err = RtmpHandshakeClient(rs)
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}
	rs.log.Println("RtmpHandshakeClient() ok")
	return rs, nil
}

/*************************************************/
/* RtmpPusher 我们推流给别人 是rtmp客户端 是发布者
/*************************************************/
func RtmpPublishMsgInteract(rs *Stream) error {
	err := SendConnMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	err = RecvMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Println("<== SendConnMsg() ok")

	//设置自己发送的chunksize
	SetRemoteChunkSize(rs)
	rs.log.Printf("<== Set RemoteChunkSize = %d", conf.Rtmp.ChunkSize)

	err = SendCreateStreamMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	err = RecvMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Println("<== SendCreateStreamMsg() ok")

	err = SendPublishMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	err = RecvMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	rs.log.Println("<== SendPublishMsg() ok")
	return nil
}

func RtmpPusher(ip, port, app, sid string) (*Stream, error) {
	rs, err := RtmpClient(ip, port, 10)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	rs.Type = "RtmpPusher"
	rs.App = app
	rs.StreamId = sid

	err = RtmpPublishMsgInteract(rs)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	rs.log.Println("RtmpPublishMsgInteract() ok")

	fn := fmt.Sprintf("%s/%s/rtsp_rtmp_%s:%s.log", conf.Log.StreamLogPath, sid, ip, port)
	StreamLogRename(rs.LogFn, fn)
	rs.LogFn = fn
	return rs, nil
}

/*************************************************/
/* RtmpPuller 我们拉别人的流 是rtmp客户端 是接收者
/*************************************************/
func RtmpPlayMsgInteract(rs *Stream) error {
	err := SendConnMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	err = RecvMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	//rs.log.Println("SendConnMsg() ok")

	//设置自己发送的chunksize
	SetRemoteChunkSize(rs)
	rs.log.Printf("<== Set RemoteChunkSize = %d", conf.Rtmp.ChunkSize)

	err = SendCreateStreamMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	err = RecvMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	//rs.log.Println("SendCreateStreamMsg() ok")

	err = SendPlayMsg(rs)
	if err != nil {
		rs.log.Println(err)
		return err
	}
	//1 send User Control Message EventType = 4
	//2 send User Control Message EventType = 0
	//3 Command Message(onStatus-play reset)
	//4 Command Message(onStatus-play start)
	//5 Command Message(onStatus-data start)
	//6 Command Message(onStatus-play PublishNotify)
	for i := 0; i < 6; i++ {
		err = RecvMsg(rs)
		if err != nil {
			rs.log.Println(err)
			return err
		}
	}
	//rs.log.Println("SendPlayMsg() ok")
	return nil
}

func RtmpPuller(ip, port, app, sid string, to int, cc chan bool) (*Stream, error) {
	var rs *Stream
	var err error

	key := fmt.Sprintf("%s_%s", app, sid)
	v, ok := RtmpPuberMap.Load(key)
	if ok == true {
		rs = v.(*Stream)
		rs.log.Printf("rtmp %s is exist", key)
		cc <- true
		return rs, nil
	}

	rs, err = RtmpClient(ip, port, to)
	if err != nil {
		log.Println(err)
		cc <- false
		return nil, err
	}
	rs.Type = "RtmpPuller"
	rs.Key = key
	rs.App = app
	rs.StreamId = sid

	fn := fmt.Sprintf("%s/%s/pull_rtmp_%s:%s_%d.log", conf.Log.StreamLogPath, sid, ip, port, utils.GetTimestamp("ns"))
	StreamLogRename(rs.LogFn, fn)
	rs.LogFn = fn

	err = RtmpPlayMsgInteract(rs)
	if err != nil {
		rs.log.Println(err)
		cc <- false
		return nil, err
	}
	rs.log.Println("RtmpPlayMsgInteract() ok")

	log.Printf("PuberKey=%s(rtmp)", rs.Key)
	rs.log.Printf("PuberKey=%s(rtmp)", rs.Key)
	RtmpPuberMap.Store(key, rs)

	go RtmpMem2RtspServer(rs)
	//go RtmpMem2RtmpPlayers()
	go RtmpSender(rs) // 给所有播放者发送数据
	cc <- true

	var msg Chunk
	var l int
	i := 0
	for {
		//rs.log.Printf("====> recv message %d", i)

		//接收(合并)数据 并 传递数据给播放者
		//msg, err = RtmpRecvData(rs)
		msg, err = MessageMerge(rs, nil)
		if err != nil {
			rs.log.Println(err)
			rs.log.Println("RtmpReceiver close")
			close(rs.AvPkg2RtspChan)
			close(rs.Msg2RtmpChan)
			RtmpPuberMap.Delete(key)
			return nil, err
		}
		rs.log.Printf("%d: type:%d(%s), ts=%d, len=%d, naluNum:%d", i, msg.MsgTypeId, msg.DataType, msg.Timestamp, msg.MsgLength, msg.NaluNum)
		i++

		err = SendAckMessage(rs, msg.MsgLength)
		if err != nil {
			rs.log.Println(err)
			close(rs.AvPkg2RtspChan)
			close(rs.Msg2RtmpChan)
			RtmpPuberMap.Delete(key)
			return nil, err
		}

		l = len(rs.Msg2RtmpChan)
		//rs.log.Printf("Msg2RtmpChanNum=%d(%d)", l, conf.Rtmp.Msg2RtmpChanNum)
		if l < conf.Rtmp.Msg2RtmpChanNum {
			rs.Msg2RtmpChan <- msg
		} else {
			rs.log.Printf("Msg2RtmpChanNum=%d(%d)", l, conf.Rtmp.Msg2RtmpChanNum)
		}

		l = len(rs.AvPkg2RtspChan)
		//rs.log.Printf("AvPkt2RtspChanNum=%d(%d)", l, conf.Rtmp.AvPkt2RtspChanNum)
		if l < conf.Rtmp.AvPkt2RtspChanNum {
			rs.AvPkg2RtspChan <- msg
		} else {
			rs.log.Printf("AvPkt2RtspChanNum=%d(%d)", l, conf.Rtmp.AvPkt2RtspChanNum)
		}
	}
	rs.Conn0.Close()
	rs.log.Printf("RtmpPuller() stop")
	return rs, err
}
