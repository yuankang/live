package main

import (
	"encoding/json"
	"fmt"
)

//接入(上行)和输出(下行)流量都要统计和上报
//上报的格式
//POST http://127.0.0.1:8999/api/v1/flowReport
//{"app":"sliveserver","streamId":"GSP3bnx69BgxI-avEc0oE4C4","startTime":1669253776759,"duration":30001,"localIP":"172.20.25.20","remoteIP":"127.0.0.1","dataType":"play","dataProtocol":"http-flv","dataSize":3997622}
//{"app":"sliveserver","streamId":"GSP3bnx69BgxI-avEc0oE4C4","startTime":1669259895115,"duration":3002,"localIP":"172.20.25.20","IpOuter":"172.20.25.20","remoteIP":"127.0.0.1","dataType":"play","dataProtocol":"http-flv","dataSize":0}

/*************************************************/
/* 网络流量统计与上报
/*************************************************/
type TrafficInfo struct {
	StartTime int64  //统计的开始时间, 单位毫秒
	Duration  int64  //持续统计数据多久, 单位毫秒
	DataSize  uint32 //统计的数据量, 单位字节
}

type TrafficRqst struct {
	App          string `json:"app"`          //上报数据的程序名sliveserver
	StreamId     string `json:"streamId"`     //
	StartTime    int64  `json:"startTime"`    //开始统计数据的时间戳, 单位毫秒
	Duration     int64  `json:"duration"`     //持续统计数据多久, 单位毫秒
	IpInner      string `json:"localIP"`      //数据上报者的内网ip
	IpClient     string `json:"remoteIP"`     //数据来源者的ip
	DataType     string `json:"dataType"`     //数据类型, origin接收的流量, play发送的流量
	DataProtocol string `json:"dataProtocol"` //数据来源哪种直播协议, gb28181/rtmp/http-flv/hls
	DataSize     uint32 `json:"dataSize"`     //数据大小, 单位字节
	//IpOuter      string `json:""`             //数据上报者的公网ip
}

type TrafficRsps struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
}

func TrafficReport(s *RtmpStream, ti TrafficInfo, dataType, dataProtocol string) {
	var rqst TrafficRqst
	rqst.App = "sliveserver"
	rqst.StreamId = s.AmfInfo.StreamId
	rqst.StartTime = ti.StartTime
	rqst.Duration = ti.Duration
	rqst.IpInner = conf.IpInner
	//rqst.IpOuter = conf.IpOuter
	rqst.IpClient = "127.0.0.1"
	rqst.DataType = dataType
	rqst.DataProtocol = dataProtocol
	rqst.DataSize = ti.DataSize

	//http://127.0.0.1:8999/api/v1/flowReport
	url := fmt.Sprintf(conf.Nt.Server)
	//s.log.Printf("TrafficUrl: %s", url)
	//s.log.Printf("TrafficData: %#v", rqst)

	d, err := json.Marshal(rqst)
	if err != nil {
		s.log.Println(err)
		return
	}

	d, err = HttpRequest("POST", url, d, 2, 2)
	if err != nil {
		s.log.Println(err)
		return
	}
	//s.log.Println(string(d))

	var rsps TrafficRsps
	err = json.Unmarshal(d, &rsps)
	if err != nil {
		s.log.Println(err)
		return
	}
	s.log.Printf("TrafficRsps: %#v", rsps)
}
