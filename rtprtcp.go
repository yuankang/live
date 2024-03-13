package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"utils"
)

/*
## SSRC和CSRC的说明
这里的同步信源是指产生媒体流的信源，它通过RTP报头中的一个32位数字SSRC标识符来标识，而不依赖于网络地址，接收者将根据SSRC标识符来区分不同的信源，进行RTP报文的分组。特约信源是指当混合器接收到一个或多个同步信源的RTP报文后，经过混合处理产生一个新的组合RTP报文，并把混合器作为组合RTP报文的 SSRC，而将原来所有的SSRC都作为CSRC传送给接收者，使接收者知道组成组合报文的各个SSRC。
考虑到在Internet这种复杂的环境中举行视频会议，RTP定义了两种中间系统: 混合器(Mixer) 和 转换器(Translator)。
### 混合器(Mixer)
在Internet上举行视频会议时，可能有少数参加者通过低速链路与使用高速网络的多数参加者相连接。为了不强制所有会议参加者都使用低带宽和低质量的数据编码，RTP允许在低带宽区域附近使用混合器作为RTP级中继器。混合器从一个或多个信源接收RTP 报文，对到达的数据报文进行重新同步和重新组合，这些重组的数据流被混合成一个数据流，将数据编码转化为在低带宽上可用的类型，并通过低速链路向低带宽区域转发。为了对多个输入信源进行统一的同步，混合器在多个媒体流之间进行定时调整，产生它自己的定时同步，因此所有从混合器输出的报文都把混合器作为同步信源。为了保证接收者能够正确识别混合器处理前的原始报文发送者，混合器在RTP报头中设置了CSRC标识符队列，以标识那些产生混和报文的原始同步信源。
### 转换器(Translator)
在Internet环境中，一些会议的参加者可能被隔离在应用级防火墙的外面，这些参加者被禁止直接使用 IP组播地址进行访问，虽然他们可能是通过高速链路连接的。在这些情况下，RTP允许使用转换器作为RTP级中继器。在防火墙两端分别安装一个转换器，防火墙之外的转换器过滤所有接收到的组播报文，并通过一条安全的连接传送给防火墙之内的转换器，内部转换器将这些组播报文再转发送给内部网络中的组播组成员。
*/

/*************************************************/
/* rtcp https://zhuanlan.zhihu.com/p/599191847
/*************************************************/
const (
	RTCP_FIR     = 192 // 关键帧请求, RFC2032
	RTCP_NACK    = 193 // 丢包重传, RFC2032
	RTCP_SMPTETC = 194 // RFC5484
	RTCP_IJ      = 195 // RFC5450
	RTCP_SR      = 200 // 发送者报告, Sender Report
	RTCP_RR      = 201 // 接受者报告, Receiver Report
	RTCP_SDES    = 202 // 源点描述, Source Description Items
	RTCP_BYE     = 203 // 结束传输
	RTCP_APP     = 204 // 特定应用
	RTCP_RTPFB   = 205 // RTP Feedback, RFC4585
	RTCP_PSFB    = 206 // PS Feedback, RFC4585
	RTCP_XR      = 207 // RFC3611
	RTCP_AVB     = 208
	RTCP_RSI     = 209 // RFC5760
	RTCP_TOKEN   = 210 // RFC6284
	RTCP_IDMS    = 211 // RFC7272
	RTCP_RGRS    = 212 // RFC8861
	RTCP_LIMIT   = 223
)

type RtcpHeader struct {
	Version       uint8  //2b, RTCP版本号
	Padding       uint8  //1b, 如果为1表示尾部有填充字节
	CountOrFormat uint8  //5b, 块的数目 可以为0
	PacketType    uint8  //8b
	Length        uint16 //16b, 表示整个RTCP包的长度(RtcpHeader+RtcpData+PaddingData), 实际字节长度=(Length+1)*4
}

type RtcpPacket struct {
	RtcpHeader
	Data   []byte //RtcpHeader+RtcpData
	Len    uint16 //2字节, Data长度, 最大为0xffff=65535
	UseNum uint16 //已经使用的Data字节数
	EsIdx  uint16 //EsData在Data里的下标
}

func RtcpParse(b []byte) RtcpHeader {
	var h RtcpHeader
	h.Version = b[0] >> 6
	h.Padding = (b[0] >> 5) & 0x1
	h.CountOrFormat = b[0] & 0x1F
	h.PacketType = b[1]
	h.Length = ByteToUint16(b[2:4], BE)

	switch h.PacketType {
	case 0xc8: //200 Sender Report 发送端报告
		RtcpPrintSr(&h, b)
	case 0xc9: //201 Receiver Report 接收端报告
		RtcpPrintRr(&h, b)
	case 0xca: //202 SDES 源点描述
		RtcpPrintSdes(&h, b)
	case 0xcb: //203 BYE 结束传输
		RtcpPrintBye(&h, b)
	case 0xcc: //204 Application-Defined 特定应用
		RtcpPrintApp(&h, b)
	default:
		log.Println("undefine rtcp packet type %d", h.PacketType)
	}
	return h
}

type Sr struct {
	SenderSsrc uint32
	NtpMsw     uint32 // NTP timestamp, most significant word
	NtpLsw     uint32 // NTP timestamp, least significant word
	Timestamp  uint32
	PktCnt     uint32
	OctetCnt   uint32
}

func RtcpPrintSr(h *RtcpHeader, b []byte) {
	var s Sr
	n := 4

	s.SenderSsrc = ByteToUint32(b[n:n+4], BE)
	n += 4
	s.NtpMsw = ByteToUint32(b[n:n+4], BE)
	n += 4
	s.NtpLsw = ByteToUint32(b[n:n+4], BE)
	n += 4
	s.Timestamp = ByteToUint32(b[n:n+4], BE)
	n += 4
	s.PktCnt = ByteToUint32(b[n:n+4], BE)
	n += 4
	s.OctetCnt = ByteToUint32(b[n:n+4], BE)
	n += 4
	//log.Printf("Rtcp Sr, ssrc=%d, Msw=%d, Lsw=%d, ts=%d pktCnt=%d, octCnt=%d", s.SenderSsrc, s.NtpMsw, s.NtpLsw, s.Timestamp, s.PktCnt, s.OctetCnt)
}

func RtcpPrintRr(h *RtcpHeader, b []byte) {
	//log.Println("call RtcpPrintRr()")
}

func RtcpPrintSdes(h *RtcpHeader, b []byte) {
	//log.Println("call RtcpPrintSdes()")
}

func RtcpPrintBye(h *RtcpHeader, b []byte) {
	//log.Println("call RtcpPrintBye()")
}

func RtcpPrintApp(h *RtcpHeader, b []byte) {
	//log.Println("call RtcpPrintApp()")
}

/*************************************************/
/* rtcp包类型说明
/*************************************************/
/*
rfc3550 6.4.1 SR: Sender Report RTCP Packet
 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|    RC   |   PT=SR=200   |             length            | header
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         SSRC of sender                        |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|              NTP timestamp, most significant word             | sender
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ info
|             NTP timestamp, least significant word             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         RTP timestamp                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                     sender's packet count                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      sender's octet count                     |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                 SSRC_1 (SSRC of first source)                 | report
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ block1
| fraction lost |       cumulative number of packets lost       |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           extended highest sequence number received           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      interarrival jitter                      |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         last SR (LSR)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                   delay since last SR (DLSR)                  |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                 SSRC_2 (SSRC of second source)                | report
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ block2
:                               ...                             :
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                  profile-specific extensions                  |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

rfc3550 6.4.2 RR: Receiver Report RTCP Packet
 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|    RC   |   PT=RR=201   |             length            | header
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                     SSRC of packet sender                     |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                 SSRC_1 (SSRC of first source)                 | report
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ block1
| fraction lost |       cumulative number of packets lost       |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           extended highest sequence number received           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      interarrival jitter                      |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         last SR (LSR)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                   delay since last SR (DLSR)                  |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                 SSRC_2 (SSRC of second source)                | report
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+ block2
:                               ...                             :
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                  profile-specific extensions                  |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

SDES: Source Description Items RTCP Packet
 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|    SC   |  PT=SDES=202  |             length            | header
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                          SSRC/CSRC_1                          | chunk1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           SDES items                          |
|                              ...                              |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|                          SSRC/CSRC_2                          | chunk2
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           SDES items                          |
|                              ...                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    CNAME=1    |     length    | user and domain name        ...
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/

/*************************************************/
/* rtp包类型说明
/*************************************************/
/*
https://blog.csdn.net/u010140427/article/details/127773028
https://www.cnblogs.com/abelchao/articles/11661706.html
Rtp可以装载 h264数据 也可以装载 ps数据

## Rtp载荷H264时, NaluType取值
0	  reserved
1-23  Nalu, single nal unit packet
24	  STAP-A, 单一时间的聚合包, 时间戳相同的多个NALU写到一起, 无DON
25	  STAP-B, 单一时间的聚合包, 时间戳相同的多个NALU写到一起, 有DON
26	  MTAP16, 多个时间的聚合包, 时间戳不同的多个NALU写到一起, 时间戳16bit
27	  MTAP24, 多个时间的聚合包, 时间戳不同的多个NALU写到一起, 时间戳24bit
28	  FU-A,   分片包, 一个rtp容不下一个NALU时 需要这种格式
29	  FU-B,   分片包, 一个rtp容不下一个NALU时 需要这种格式
30-31 reserved

## Rtp载荷H265时, NaluType取值
48    聚合包
49    分片包

## rtp包大小限制
rtp使用tcp协议发送数据的时候, rtp包大小 不受限制, 因为tcp提供分组到达的检测
rtp使用udp协议发送数据的时候, rtp包大小 不能大于mtu(一般是1500字节)
ip包头20字节, udp包头8字节, rtp包头12字节, 所以rtp负载1500-20-8-12=1460字节
因为音频编码数据的一帧通常是小于MTU的，所以通常是直接使用RTP协议进行封装和发送

如果负载数据长度大于1460字节, 由于没有在应用层分割数据, 将会产生大于MTU的rtp包
在IP层其将会被分割成几个小于MTU尺寸的包, 因为IP和UDP协议都没有提供分组到达的检测
分割后就算所有包都成功接收, 但是由于只有第一个包中包含有完整的RTP头信息, 而RTP头中没有关于载荷长度的标识, 因此判断不出该RTP包是否有分割丢失, 只能认为完整的接收了

## MTU是什么
MTU(Maximum Transmission Unit)是网络最大传输单元, 是指网络通信协议的某一协议层上 所能通过的数据包最大字节数.
通信主机A -> 网络设备(路由器/交换机) -> 通信主机B, 这些设备上 都有MTU的设置，一般都是1500
### 当MTU不合理时会造成如下问题
1 本地MTU值大于网络MTU值时，本地传输的"数据包"过大导致网络会拆包后传输，不但产生额外的数据包，而且消耗了"拆包, 组包"的时间
2 本地MTU值小于网络MTU值时，本地传输的数据包可以直接传输，但是未能完全利用网络给予的数据包传输尺寸的上限值，传输能力未完全发挥。
### 什么是合理的MTU值?
所谓合理的设置MTU值，就是让本地的MTU值与网络的MTU值一致，既能完整发挥传输性能，又不让数据包拆分。
怎么探测合理的MTU? 发送大小是1460(+28)字节的包, 20字节的ip头，和8字节的icmp封装
### linux探测MTU值
[localhost:~]# ping -s 1460 -M do baidu.com  小于等于网络mtu值, 都会返回正常
PING baidu.com (220.181.38.251) 1460(1488) bytes of data.
1468 bytes from 220.181.38.251 (220.181.38.251): icmp_seq=1 ttl=47 time=4.39 ms
[localhost:~]# ping -s 1500 -M do baidu.com  大于网络mtu值, 会返回错误信息
PING baidu.com (220.181.38.251) 1500(1528) bytes of data.
ping: local error: Message too long, mtu=1500
### linux临时修改MTU值
ifconfig eth0 mtu 1488 up

RtpHeader
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| V |P|X|   CC  |M|     PT      |         sequence number       |
+---+-+-+-------+-+-------------+-------------------------------+
|                           timestamp						    |
+---------------------------------------------------------------+
|                             SSRC					    	    |
+---------------------------------------------------------------+
|                             CSRC					    	    |
|                             ....					    	    |
+---------------------------------------------------------------+

RtpHeader扩展头 至少32bit, RtpHeader中X=1时才有扩展头
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|       defined by profile      |           length              |
+---+-+-+-------+-+-------------+-------------------------------+
|                        header extension                       |
|                             ....					    	    |
+---------------------------------------------------------------+
defined by profile: 表示扩展数据类型, 通常自定义, 占用16bit大小
length: 扩展数据长度, 不包含defined by profile和length所占用大小, 占用16bit大小
header extension: 扩展数据内容, 可以为空, 即大小最小可以为0

Single单一包, 只有一个nalu
如果Nalu的size <= MTU(网络最大传输单元, 一般是1500byte)
此时 单个Nalu 放入 RtpPayload中
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X|  CC   |M|     PT      |       sequence number         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           timestamp                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           synchronization source (SSRC) identifier            |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|            contributing source (CSRC) identifiers             |
|                             ....                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|F|NRI|  type   |                                               |
+-+-+-+-+-+-+-+-+                                               |
|               Bytes 2..n of a Single NAL unit                 |
|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                               :...OPTIONAL RTP padding        |
+---------------------------------------------------------------+

STAP-A聚合包, 多个时间戳相同的nalu
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X|  CC   |M|     PT      |       sequence number         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           timestamp                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           synchronization source (SSRC) identifier            |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|            contributing source (CSRC) identifiers             |
|                             ....                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|STAP-A NAL HDR |			NALU 1 Size		    |  NALU 1 HDR   |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|							NALU 1 Data							|
+				+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|				|			NALU 2 Size			|  NALU 2 HDR   |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|							NALU 2 Data							|
|               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|               :			...OPTIONAL RTP padding				|
+---------------------------------------------------------------+
聚合数据包的作用
1 一帧画面 可能有多个slice, 一个slice是一个nalu, 这些nalu的时间相同
2 两个目标网络的MTU大小不同, 有线网络MTU一般1500字节, 无线网络优选传输单元大小为254字节或更小, 为了防止两个网络之间的媒体转码并避免不期望的分组开销, 这些nalu的时间可能相同也可能不同

FU-A分组包, 一个nalu的一部分
如果Nalu的size > MTU(网络最大传输单元, 一般是1500byte)
此时 按照FU-A分片
1 第一个FU-A包的FU indicator: F应该为当前NALU头的F, 而NRI应该为当前NALU头的NRI，Type则等于28，表明它是FU-A包。FU header生成方法：S = 1，E = 0，R = 0，Type则等于NALU头中的Type。
2 后续的N个FU-A包的FU indicator和第一个是完全一样的，如果不是最后一个包，则FU header应该为：S = 0，E = 0，R = 0，Type等于NALU头中的Type。
3 最后一个FU-A包FU header应该为：S = 0，E = 1，R = 0，Type等于NALU头中的Type。
+---------------------------------------------------------------+
|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X|  CC   |M|     PT      |       sequence number         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           timestamp                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           synchronization source (SSRC) identifier            |
+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
|            contributing source (CSRC) identifiers             |
|                             ....                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| FU indicator  |   FU header   |                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
|                         FU payload                            |
|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                               :...OPTIONAL RTP padding        |
+---------------------------------------------------------------+
FU indicator
+---------------+
|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+
|F|NRI|  Type   |
+---------------+
FU header
+---------------+
|0|1|2|3|4|5|6|7|
+-+-+-+-+-+-+-+-+
|S|E|R|  Type   |
+---------------+
S: 1bit当设置成1, 开始位指示分⽚NAL单元的开始. 当跟随的FU荷载不是分⽚NAL单元荷载的开始, 开始位设为0
E: 1bit当设置成1, 结束位指示分⽚NAL单元的结束, 即 荷载的最后字节也是分⽚NAL单元的最后⼀个字节, 当跟随的FU荷载不是分⽚NAL单元的最后分⽚,结束位设置为0
也就是说⼀个NALU切⽚时, 第⼀个切⽚的SE是10, 然后中间的切⽚是00, 最后⼀个切⽚时11
R: 1bit保留位必须设置为0, 接收者必须忽略该位
Type: 5bits 此处的Type就是NALU头中的Type,取1-23的那个值, 表示 NAL单元荷载类型定义, 因为不管是不是分片, 都需要知道这一帧的类型, 所以这里的type就承载了帧类型
*/

/*************************************************/
/* Rtp PayloadType 在各种协议里的规定
/*************************************************/
/*
GB28181	中Rtp PayloadType定义
00, 0x00, G711u(PCMU)
08, 0x08, G711a(PCMA)
96, 0x60, PS
97, 0x61, AAC
98, 0x62, H264
99, 0x63, H265 ???

RTSP	中Rtp PayloadType定义
00, 0x00, G711u
08, 0x08, G711a(PCMA)
96, 0x60, H264/H265, a=rtpmap:96 H264/90000, a=rtpmap:96 H265/90000
97, 0x61, AAC, sdp MPEG4-GENERIC, ffmpeg推rtsp流 用的是这个值
98, 0x62, AAC, sdp mpeg4-generic, 拉ZLMediaKit的rtsp流 用的是这个值
*/
func RtpPayload2Str(ProtocolType string, PayloadType int) string {
	switch ProtocolType {
	case "GB28181":
		switch PayloadType {
		case 0x00:
			return "G711u"
		case 0x08:
			return "G711a"
		case 0x60:
			return "PS"
		case 0x61:
			return "AAC"
		case 0x62:
			return "H264"
		case 0x63:
			return "H265"
		default:
			return "undefined"
		}
	case "RTSP":
		switch PayloadType {
		case 0x00:
			return "G711u"
		case 0x08:
			return "G711a"
		case 0x60:
			return "H264H265"
		case 0x61: //ffmpeg
			return "AAC"
		case 0x62: //ZLMediaKit
			return "AAC"
		default:
			return "undefined"
		}
	default:
	}
	return "undefined"
}

/*************************************************/
/* rtp包解析
/*************************************************/
type FuIndicator struct {
	F    uint8 //1bit
	NRI  uint8 //2bit
	Type uint8 //5bit
}

type FuHeader struct {
	S    uint8 //1bit
	E    uint8 //1bit
	R    uint8 //1bit
	Type uint8 //5bit
}

//1+1+2+4+4+n*4=12+n*4
type RtpHeader struct {
	Version     uint8    //2bit
	Padding     uint8    //1bit
	Extension   uint8    //1bit
	CsrcCount   uint8    //4bit
	Marker      uint8    //1bit
	PayloadType uint8    //7bit, ffmpeg-5.1.2/libavformat/rtp.c
	SeqNumber   uint16   //16bit, 值:0-65535
	Timestamp   uint32   //32bit
	Ssrc        uint32   //32bit
	Csrc        []uint32 //32bit
	PtStr       string   //PayloadTypeString
}

type RtpPacket struct {
	RtpHeader
	Data   []byte //RtpHeader+RtpData
	Len    uint16 //2字节, Data长度, 最大为0xffff=65535
	UseNum uint16 //已经使用的Data字节数
	EsIdx  uint16 //EsData在Data里的下标
}

//StreamId_20230225124550_rtp.rec
func RtpRec(s *Stream) {
	fn := fmt.Sprintf("%s/%s_%s_rtp.rec", conf.StreamRec.SavePath, s.StreamId, utils.GetYMDHMS())
	s.log.Printf("RtpRec: %s", fn)

	err := os.MkdirAll(path.Dir(fn), 0755)
	if err != nil {
		s.log.Println(err)
		return
	}

	fp, err := os.Create(fn)
	if err != nil {
		s.log.Println(err)
		return
	}
	defer fp.Close()

	var rp RtpPacket
	var ok bool
	var d []byte
	for {
		rp, ok = <-s.RtpRecChan
		if ok == false {
			s.log.Println("RtpRecorder() stop")
			break
		}

		d = Uint16ToByte(rp.Len, nil, BE)
		_, err = fp.Write(d)
		if err != nil {
			s.log.Println(err)
			break
		}

		_, err = fp.Write(rp.Data)
		if err != nil {
			s.log.Println(err)
			break
		}
	}
}

func RtpPkt2Byte(rp *RtpPacket) {
}

func RtpParse(d []byte) *RtpPacket {
	var n uint16
	rp := &RtpPacket{}

	rp.Version = (d[n] >> 6) & 0x3
	rp.Padding = (d[n] >> 5) & 0x1
	rp.Extension = (d[n] >> 4) & 0x1
	rp.CsrcCount = (d[n] >> 0) & 0xf
	n += 1

	rp.Marker = (d[n] >> 7) & 0x1
	rp.PayloadType = (d[n] >> 0) & 0x7f
	n += 1

	rp.SeqNumber = ByteToUint16(d[n:n+2], BE)
	n += 2
	rp.Timestamp = ByteToUint32(d[n:n+4], BE)
	n += 4
	rp.Ssrc = ByteToUint32(d[n:n+4], BE)
	n += 4

	for i := 0; i < int(rp.CsrcCount); i++ {
		ByteToUint32(d[n:n+4], BE)
		n += 4
	}

	rp.Data = d
	rp.Len = uint16(len(d))
	rp.UseNum += n
	return rp
}

func RtpSinglePktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpSinglePkg"
	var n uint32 = 12
	var nh NaluHeaderH264
	var err error

	nh.F = (rp.Data[n] & 0x80) >> 7
	nh.NRI = (rp.Data[n] & 0x60) >> 5
	nh.Type = rp.Data[n] & 0x1f
	//n += 1
	//rs.log.Printf("h264 NaluVal=%x, %#v", rp.Data[n], nh)

	switch nh.Type {
	case 0x1:
		p.Type = "VideoInterFrame"
	case 0x5:
		p.Type = "VideoKeyFrame"
	case 0x6:
		//rtsp协议中sei可能单独一个rtp包
		//rtmp协议中sei要和关键帧写到一个AvPacket里
		//rtmp AvPacket = NaluSei + NaluKeyFrame
		p.Type = "VideoKeySei"
		if bytes.Compare(rs.SeiData, rp.Data[n:]) != 0 {
			rs.SeiData = rp.Data[n:]
			rs.log.Printf("SeiData:%x", rs.SeiData)
		}
		return nil
	case 0x7:
		p.Type = "VideoKeySps"
	case 0x8:
		p.Type = "VideoKeyPps"
	default:
		err = fmt.Errorf("undefined nalu type %d", nh.Type)
		rs.log.Println(err)
		return err
	}
	//rs.log.Printf("RtpSeq:%d, RtpTs:%d, RtpDataLen:%d, naluType=%d(%s)", rp.SeqNumber, rp.Timestamp, rp.Len, nh.Type, p.Type)

	p.Data = append(p.Data, rp.Data[n:]...)
	return nil
}

func RtpStapaPktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpStapaPkg"
	var n uint32 = 13
	var nh NaluHeaderH264
	var err error

	i := 0
	for n < uint32(rp.Len) {
		l := ByteToUint32(rp.Data[n:n+2], BE)
		rs.log.Printf("nalu %d size %d", i, l)
		i++
		n += 2

		nh.F = (rp.Data[n] & 0x80) >> 7
		nh.NRI = (rp.Data[n] & 0x60) >> 5
		nh.Type = rp.Data[n] & 0x1f
		rs.log.Printf("h264 NaluHead=%x, %#v, NaluVal=%x", rp.Data[n], nh, rp.Data[n:n+l])

		switch nh.Type {
		case 0x6: //SEI
			//rs.SeiData = rp.Data[n : n+l]
		case 0x7: //SPS
			p.Type = "RtpSeiSpsPps"
			rs.SpsData = rp.Data[n : n+l]
			rs.log.Printf("SpsData:%x", rs.SpsData)
			rs.Sps, err = SpsParse(rs.SpsData)
			if err != nil {
				rs.log.Println(err)
				return err
			}
			//rs.log.Printf("%#v", rs.Sps)
			rs.Width = int((rs.Sps.PicWidthInMbsMinus1 + 1) * 16)
			rs.Height = int((rs.Sps.PicHeightInMapUnitsMinus1 + 1) * 16)
			rs.log.Printf("video width=%d, height=%d", rs.Width, rs.Height)
			rs.RtpSpsPpsPkt = rp
		case 0x8: //PPS
			rs.PpsData = rp.Data[n : n+l]
			rs.log.Printf("PpsData:%x", rs.PpsData)
		default:
			err = fmt.Errorf("undefined NaluType %d", nh.Type)
		}
		n += l
	}
	return err
}

func RtpStapbPktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpStapbPkg"
	return nil
}

func RtpMtap16PktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpMtap16Pkg"
	return nil
}

func RtpMtap24PktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpMtap24Pkg"
	return nil
}

func RtpFuaPktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpFuaPkg"
	var n uint32 = 12
	var nh NaluHeaderH264
	var err error
	var fui FuIndicator
	var fuh FuHeader

	fui.F = (rp.Data[n] & 0x80) >> 7
	fui.NRI = (rp.Data[n] & 0x60) >> 5
	fui.Type = rp.Data[n] & 0x1f
	n += 1
	fuh.S = (rp.Data[n] & 0x80) >> 7
	fuh.E = (rp.Data[n] & 0x40) >> 6
	fuh.R = (rp.Data[n] & 0x20) >> 5
	fuh.Type = rp.Data[n] & 0x1f
	n += 1
	//rs.log.Printf("%#v", fui)
	//rs.log.Printf("%#v", fuh)

	switch fuh.Type {
	case 1:
		p.Type = "VideoInterFrame"
	case 5:
		p.Type = "VideoKeyFrame"
	default:
		err = fmt.Errorf("undefined nalu type %d", nh.Type)
		rs.log.Println(err)
		return err
	}

	//先把nalu header写进去
	if len(p.Data) == 0 {
		nh.F = fui.F
		nh.NRI = fui.NRI
		nh.Type = fuh.Type
		b := byte(nh.F<<7 | nh.NRI<<5 | nh.Type)
		p.Data = append(p.Data, b)
	}
	p.Data = append(p.Data, rp.Data[n:]...)
	return nil
}

func RtpFubPktParse(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	p.Type = "RtpFubPkg"
	return nil
}

func Rtp2H264Packet(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	var n uint32 = 12
	var nh NaluHeaderH264
	var err error

	nh.F = (rp.Data[n] & 0x80) >> 7
	nh.NRI = (rp.Data[n] & 0x60) >> 5
	nh.Type = rp.Data[n] & 0x1f
	//rs.log.Printf("h264 NaluHead=%x, %#v", rp.Data[n], nh)

	if nh.Type < 23 { //单个 NAL 单元包
		err = RtpSinglePktParse(rs, p, rp)
	} else if nh.Type == 24 { //STAP-A 单一时间的组合包
		err = RtpStapaPktParse(rs, p, rp)
	} else if nh.Type == 25 { //STAP-B 单一时间的组合包
		err = RtpStapbPktParse(rs, p, rp)
	} else if nh.Type == 26 { //MTAP16 多个时间的组合包
		err = RtpMtap16PktParse(rs, p, rp)
	} else if nh.Type == 27 { //MTAP24 多个时间的组合包
		err = RtpMtap24PktParse(rs, p, rp)
	} else if nh.Type == 28 { //FU-A 分片的单元
		err = RtpFuaPktParse(rs, p, rp)
	} else if nh.Type == 29 { //FU-B 分片的单元
		err = RtpFubPktParse(rs, p, rp)
	} else {
		err = fmt.Errorf("undefined video frame type %d", nh.Type)
		rs.log.Println(err)
	}
	return err
}

func Rtp2H265Packet(rs *RtspStream, p *AvPacket, rp *RtpPacket) error {
	n := 12
	var nh NaluHeaderH265
	var err error

	nh.F = (rp.Data[n] & 0x80) >> 7
	nh.Type = (rp.Data[n] & 0x7e) >> 1
	nh.LID = ((rp.Data[n] & 0x1) << 5) | (rp.Data[n+1] & 0xf8)
	nh.Tid = (rp.Data[n+1] & 0x7)
	rs.log.Printf("h265 NaluVal=%x, %#v", rp.Data[n:n+2], nh)

	if nh.Type < 23 { //单个 NAL 单元包
		err = RtpSinglePktParse(rs, p, rp)
	} else if nh.Type == 24 { //STAP-A 单一时间的组合包
		err = RtpStapaPktParse(rs, p, rp)
	} else if nh.Type == 25 { //STAP-B 单一时间的组合包
		err = RtpStapbPktParse(rs, p, rp)
	} else if nh.Type == 26 { //MTAP16 多个时间的组合包
		err = RtpMtap16PktParse(rs, p, rp)
	} else if nh.Type == 27 { //MTAP24 多个时间的组合包
		err = RtpMtap24PktParse(rs, p, rp)
	} else if nh.Type == 28 { //FU-A 分片的单元
		err = RtpFuaPktParse(rs, p, rp)
	} else if nh.Type == 29 { //FU-B 分片的单元
		err = RtpFubPktParse(rs, p, rp)
	} else {
		err = fmt.Errorf("undefined video frame type %d", nh.Type)
		rs.log.Println(err)
	}
	return err
}

/*************************************************/
/* rtp包生成
/*************************************************/
func RtpStapaPktCreate(rs *RtspStream, sps, pps []byte, ts uint32) (*RtpPacket, error) {
	rp := &RtpPacket{}
	rp.Version = 2
	rp.Padding = 0
	rp.Extension = 0
	rp.CsrcCount = 0
	rp.Marker = 0
	rp.PayloadType = 96
	rp.SeqNumber = rs.VideoRtpPkgs.SendSeq
	rs.VideoRtpPkgs.SendSeq += 1
	rp.Timestamp = ts
	rp.Ssrc = 999999999
	rp.Csrc = nil
	rp.PtStr = "Video"

	//h264 NaluHead=18, main.NaluHeaderH264{F:0x0, NRI:0x0, Type:0x18}
	var nh NaluHeaderH264
	nh.F = 0x0
	nh.NRI = 0x0
	nh.Type = 0x18

	spsLen := len(sps) //sps中含有NaluHeader
	ppsLen := len(pps) //pps中含有NaluHeader

	rp.Len = uint16(12 + 1 + 4 + spsLen + ppsLen)
	rp.Data = make([]byte, rp.Len)

	n := 0
	rp.Data[n] = ((rp.Version & 0x3) << 6) | ((rp.Padding & 0x1) << 5) |
		((rp.Extension & 0x1) << 4) | (rp.CsrcCount & 0xf)
	n += 1
	rp.Data[n] = ((rp.Marker & 0x1) << 7) | (rp.PayloadType & 0x7f)
	n += 1

	Uint16ToByte(rp.SeqNumber, rp.Data[n:n+2], BE)
	n += 2
	Uint32ToByte(rp.Timestamp, rp.Data[n:n+4], BE)
	n += 4
	Uint32ToByte(rp.Ssrc, rp.Data[n:n+4], BE)
	n += 4

	rp.Data[n] = ((nh.F & 0x1) << 7) | ((nh.NRI & 0x3) << 5) | (nh.Type & 0x1f)
	n += 1

	Uint16ToByte(uint16(spsLen), rp.Data[n:n+2], BE)
	n += 2
	copy(rp.Data[n:n+spsLen], sps)
	n += spsLen

	Uint16ToByte(uint16(ppsLen), rp.Data[n:n+2], BE)
	n += 2
	copy(rp.Data[n:n+ppsLen], pps)
	n += ppsLen
	return rp, nil
}

func RtpPkgCreate(rs *Stream, s *RtspStream, c *Chunk) ([]*RtpPacket, error) {
	n := c.MsgLength / 1460
	if c.MsgLength%1460 != 0 {
		n++
	}
	//s.log.Printf("DataLen=%d, n=%d(1460)", c.MsgLength, n)

	var rps []*RtpPacket
	var err error
	if n == 1 {
		rps, err = RtpSinglePktCreate(s, c)
	} else {
		rps, err = RtpFuaPktCreate(s, c, n)
	}

	//s.log.Printf("type:%d(%s), ts=%d, len=%d, naluNum:%d, rtpNum=%d", c.MsgTypeId, c.DataType, c.Timestamp, c.MsgLength, c.NaluNum, len(rps))
	return rps, err
}

func RtpSinglePktCreateAudio(rs *RtspStream, c *Chunk) (*RtpPacket, error) {
	rp := &RtpPacket{}
	rp.Version = 2
	rp.Padding = 0
	rp.Extension = 0
	rp.CsrcCount = 0
	rp.Marker = 0
	rp.PayloadType = 97
	rp.SeqNumber = rs.AudioRtpPkgs.SendSeq
	rs.AudioRtpPkgs.SendSeq += 1
	rp.PtStr = "Audio"
	rp.Timestamp = c.Timestamp * 11
	rp.Ssrc = 999999999
	rp.Csrc = nil

	rp.Len = uint16(12 + 4 + c.MsgLength - 2)
	rp.Data = make([]byte, rp.Len)

	n := 0
	rp.Data[n] = ((rp.Version & 0x3) << 6) | ((rp.Padding & 0x1) << 5) |
		((rp.Extension & 0x1) << 4) | (rp.CsrcCount & 0xf)
	n += 1
	rp.Data[n] = ((rp.Marker & 0x1) << 7) | (rp.PayloadType & 0x7f)
	n += 1

	Uint16ToByte(rp.SeqNumber, rp.Data[n:n+2], BE)
	n += 2
	Uint32ToByte(rp.Timestamp, rp.Data[n:n+4], BE)
	n += 4
	Uint32ToByte(rp.Ssrc, rp.Data[n:n+4], BE)
	n += 4

	var AuHeadersLength uint16 = 16
	Uint16ToByte(AuHeadersLength, rp.Data[n:n+2], BE)
	n += 2

	MsgLen := c.MsgLength - 2
	MsgLen = (MsgLen & 0x1fff) << 3
	Uint16ToByte(uint16(MsgLen), rp.Data[n:n+2], BE)
	n += 2

	copy(rp.Data[n:], c.MsgData[2:])
	n += int(c.MsgLength - 2)
	return rp, nil
}

func RtpSinglePktCreateVideo(rs *RtspStream, c *Chunk) (*RtpPacket, error) {
	rp := &RtpPacket{}
	rp.Version = 2
	rp.Padding = 0
	rp.Extension = 0
	rp.CsrcCount = 0
	rp.Marker = 0
	rp.PayloadType = 96
	rp.SeqNumber = rs.VideoRtpPkgs.SendSeq
	rs.VideoRtpPkgs.SendSeq += 1
	rp.PtStr = "Video"
	rp.Timestamp = c.Timestamp * 90
	rp.Ssrc = 999999999
	rp.Csrc = nil

	rp.Len = uint16(12 + c.MsgLength)
	rp.Data = make([]byte, rp.Len)

	n := 0
	rp.Data[n] = ((rp.Version & 0x3) << 6) | ((rp.Padding & 0x1) << 5) |
		((rp.Extension & 0x1) << 4) | (rp.CsrcCount & 0xf)
	n += 1
	rp.Data[n] = ((rp.Marker & 0x1) << 7) | (rp.PayloadType & 0x7f)
	n += 1

	Uint16ToByte(rp.SeqNumber, rp.Data[n:n+2], BE)
	n += 2
	Uint32ToByte(rp.Timestamp, rp.Data[n:n+4], BE)
	n += 4
	Uint32ToByte(rp.Ssrc, rp.Data[n:n+4], BE)
	n += 4

	copy(rp.Data[n:], c.MsgData)
	n += int(c.MsgLength)
	return rp, nil
}

//一般都为音频, 也可能有视频
func RtpSinglePktCreate(rs *RtspStream, c *Chunk) ([]*RtpPacket, error) {
	var rps []*RtpPacket
	var rp *RtpPacket
	var err error

	if strings.Contains(c.DataType, "Video") {
		rp, err = RtpSinglePktCreateVideo(rs, c)
	} else {
		rp, err = RtpSinglePktCreateAudio(rs, c)
	}
	if err != nil {
		rs.log.Println(err)
		return nil, err
	}
	rps = append(rps, rp)

	//rs.log.Printf("V=%d, P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, Len=%d", rp.Version, rp.Padding, rp.Extension, rp.CsrcCount, rp.Marker, rp.PayloadType, rp.PtStr, rp.SeqNumber, rp.Timestamp, rp.Ssrc, rp.Len)
	return rps, nil
}

func RtpFuaPktCreate1(rs *RtspStream, fui FuIndicator, fuh FuHeader, c *Chunk) (*RtpPacket, error) {
	rp := &RtpPacket{}
	rp.Version = 2
	rp.Padding = 0
	rp.Extension = 0
	rp.CsrcCount = 0
	rp.Marker = 0
	if strings.Contains(c.DataType, "Video") {
		rp.PayloadType = 96
		rp.SeqNumber = rs.VideoRtpPkgs.SendSeq
		rs.VideoRtpPkgs.SendSeq += 1
		rp.PtStr = "Video"
	} else {
		rp.PayloadType = 97
		rp.SeqNumber = rs.AudioRtpPkgs.SendSeq
		rs.AudioRtpPkgs.SendSeq += 1
		rp.PtStr = "Audio"
	}
	rp.Timestamp = c.Timestamp
	rp.Ssrc = 999999999
	rp.Csrc = nil

	var err error
	if strings.Contains(c.DataType, "Audio") {
		err = fmt.Errorf("rtsp drop audio")
		rs.log.Println(err)
		return nil, err
	}

	rp.Len = uint16(12 + 2 + c.MsgLength)
	rp.Data = make([]byte, rp.Len)

	n := 0
	rp.Data[n] = ((rp.Version & 0x3) << 6) | ((rp.Padding & 0x1) << 5) |
		((rp.Extension & 0x1) << 4) | (rp.CsrcCount & 0xf)
	n += 1
	rp.Data[n] = ((rp.Marker & 0x1) << 7) | (rp.PayloadType & 0x7f)
	n += 1

	Uint16ToByte(rp.SeqNumber, rp.Data[n:n+2], BE)
	n += 2
	Uint32ToByte(rp.Timestamp, rp.Data[n:n+4], BE)
	n += 4
	Uint32ToByte(rp.Ssrc, rp.Data[n:n+4], BE)
	n += 4

	rp.Data[n] = ((fui.F & 0x1) << 7) | ((fui.NRI & 0x3) << 5) | (fui.Type & 0x1f)
	n += 1
	rp.Data[n] = ((fuh.S & 0x1) << 7) | ((fuh.E & 0x1) << 6) | ((fuh.R & 0x1) << 5) | (fuh.Type & 0x1f)
	n += 1

	copy(rp.Data[n:], c.MsgData)
	n += int(c.MsgLength)

	//rs.log.Printf("V=%d, P=%d, X=%d, CC=%d, M=%d, PT=%d(%s), Seq=%d, TS=%d, SSRC=%d, Len=%d", rp.Version, rp.Padding, rp.Extension, rp.CsrcCount, rp.Marker, rp.PayloadType, rp.PtStr, rp.SeqNumber, rp.Timestamp, rp.Ssrc, rp.Len)
	return rp, nil
}

//一般都为视频, 极少能为音频
func RtpFuaPktCreate(rs *RtspStream, c *Chunk, n uint32) ([]*RtpPacket, error) {
	//sps+pps+keyframe, 只需要keyframe
	//nn, vd := GetNaluNum(nil, c, "h264")
	_, vd := GetNaluNum(nil, c, "h264")
	l := uint32(len(vd))
	//rs.log.Printf("nn=%d, vdLen=%d, vd=%x", nn, l, vd[:2])

	var nh NaluHeaderH264
	nh.F = (vd[0] & 0x80) >> 7
	nh.NRI = (vd[0] & 0x60) >> 5
	nh.Type = vd[0] & 0x1f
	//rs.log.Printf("h264 NaluHead=%x, %#v", vd[0], nh)

	vd = vd[1:] //去掉naluHeader
	l = uint32(len(vd))

	var ckArr []*Chunk
	var i uint32
	for i = 0; i < l; {
		ck := &Chunk{}
		ck.Timestamp = c.Timestamp * 90
		ck.MsgLength = 1460
		ck.MsgTypeId = c.MsgTypeId
		ck.MsgData = nil
		ck.DataType = c.DataType

		if l-i < 1460 {
			ck.MsgLength = l - i
		}
		ck.MsgData = vd[i : i+ck.MsgLength]
		i += ck.MsgLength
		ckArr = append(ckArr, ck)
	}
	l = uint32(len(ckArr))
	//rs.log.Printf("ckArrLen=%d, ckLen=%d", l, len(ckArr[l-1].MsgData))

	var fui FuIndicator
	fui.F = nh.F
	fui.NRI = nh.NRI
	fui.Type = 28

	var fuh FuHeader
	fuh.Type = nh.Type

	var rps []*RtpPacket
	var rp *RtpPacket
	for i = 0; i < l; i++ {
		if i == 0 {
			fuh.S = 1
			fuh.E = 0
		} else if i+1 == l {
			fuh.S = 0
			fuh.E = 1
		} else {
			fuh.S = 0
			fuh.E = 0
		}
		rp, _ = RtpFuaPktCreate1(rs, fui, fuh, ckArr[i])
		rps = append(rps, rp)
	}
	//rs.log.Printf("rpsLen=%d", len(rps))
	return rps, nil
}
