## GB28181 sip和测试环境

协议版本            GB/T28181-2016
SIP服务器ID         11000000122000000034
SIP服务器域         1100000012
SIP服务器地址       10.3.220.68
SIP服务器端口       62097
SIP用户名           11010000121310000034
SIP用户认证ID       11010000121310000034
密码                123456
注册有效期          3600 秒
心跳周期            60 秒
本地SIP端口         5060
注册间隔            60 秒
最大心跳超时次数    3

// 摄像头   10.3.220.151    5060
// DCN      10.3.220.68     62097 50280

// 流媒体   172.20.25.20    62097
// tcpdump -nnvvv -i eth0 port 62097 -w sipSms.pcap
// scp root@172.20.25.20:/root/sipSms.pcap .

// sip      172.20.25.28    50280
// ssh root@172.20.25.28    Lty20@Ltsk21
// tcpdump -nnvvv -i eth0 host 10.3.220.151 -w sipSvr.pcap
// scp root@172.20.25.28:/root/sipSvr.pcap .

## 能力
接收rtsp推流                完成
    rtsp播放(发gop)         90%
    转推rtmp流              完成
http触发rtsp拉流            完成
    rtsp播放(发gop)         90%
    转推rtmp流              完成
rtsp播放触发rtmp拉流        完成
    rtmp拉流rtsp发布播放    完成
    rtsp播放(发gop)         90%
    n分钟无播放断流         0%
支持h264                    完成
支持h265                    0%
支持AAC                     完成
支持G711x                   0%
rtsp支持RtpTcp单端口        完成
rtsp支持RtpUdp单端口        0%
rtsp支持RtpTcp多端口        0%
rtsp支持RtpUdp多端口        0%
rtcp协议支持                0%
日志整理                    80%
资源回收                    0%
支持音频转码(重采样)        40%


## 开发目标
1 GW和SVR合并 走内存传数据 提高单机性能
2 GW和CP配合 避免GW升级引起IPC推流中断
3 GW支持 独立运行 和 配合CP运行 两种模式

## GW功能点梳理
1 跟cc业务交互          2   95%
2 未知ssrc上报          1   0%
2 支持RtpTcp收数据      5   100%
3 支持RtpUdp收数据      1   20%
4 接收数据异常处理      2   0%
4 支持固定端口收数据    0   60%
5 支持范围端口收数据    3   0%
6 录像回看暂停保持      0   0%
6 数据错乱定位          1   0%
7 rtmp网络推流给Svr     4   100%
8 走内存把数据给Svr     2   0%
9 等音频发送metadata    2   0%
10 支持h264             4   100%
11 支持h265             2   10%
12 音频转码迁移至SVR    4   30%
12 支持G711a            1   100%
13 支持G711u            0   0%
14 支持Aac              1   0%
15 支持rtcp             4   0%
16 流维度日志0点分割    2   80%
17 配合CP运行           x   0%
18 资源释放测试         8   10%
19 多ipc兼容测试        4   0%
20 日志和性能优化       5   0%
21 稳定和压力测试       3   0%

## 测试说明
sip服务器 [tcp|udp]://172.20.25.20:62090
tcpdump -nnvvv -i eth0 port 62090 -w sip.pcap

流媒体服务器 [tcp|udp]://172.20.25.20:62091
tcpdump -nnvvv -i eth0 host 10.3.220.151 -w rtp.pcap

摄像头 10.3.220.151

DCN 10.3.220.68
端口转发
10.3.220.68:62090 换发给 172.20.25.20:62090
10.3.220.68:62091 换发给 172.20.25.20:62091

sip协议交互流程, 参考下面2个文档
GBT-28181_2011.pdf
GBT-28181_2016.pdf

## rtsp推路命令
ffmpeg -rtsp_transport tcp -i rtsp://125.39.179.77:2554/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa -c:v copy -c:a copy -f flv -y "rtmp://192.168.16.171:1945/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb?owner=Spy2023Zjr"
ffplay rtmp://192.168.16.171:1945/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb
ffplay http://192.168.16.171:1995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb.flv
ffplay http://192.168.16.171:1995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb.m3u8

ffmpeg -rtsp_transport tcp -i rtsp://125.39.179.77:2554/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKa -c:v copy -c:a copy -f rtsp -rtsp_transport tcp rtsp://192.168.16.171:2995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb
ffplay rtmp://192.168.16.171:1945/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb
ffplay http://192.168.16.171:1995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb.flv
ffplay http://192.168.16.171:1995/SPq3pr6f6kNa/RSPq3pr6f6kNa-eQW54pIhKb.m3u8

## rtsp播放触发rtmp拉流过程
1 接收rtsp播放, 检查是否有rtsp发布
2 如果有rtsp发布, 直接发送数据给播放者
3 如果没有rtsp发布, 发起rtmp拉流, rtsp播放周期性检查是否有rtsp发布
4 rtmp拉流失败, 重试5次每次间隔1秒
5 rtmp拉流成功, 通过内存发布rtsp
6 rtsp播放检查到有rtsp发布, 把直接挂到rtsp发布的播放者列表中
7 rtsp发布者 发送数据给每个rtsp播放者

## 音频转码
所以输入音频统一转码为aac 11025Hz mono s16l
    aac音频转码aac
    g711a音频转码aac
    g711u音频转码aac
存在问题
国标音频G711x采样率应该都为8KHz, 但是有些流是16KHz, 国标sdp中无音频采样率信息, 所以GW只能默认按8KHz处理
Rtsp音频G711x采样率应该都为8KHz, 但是有些流是16KHz, RtspSdp中有音频采样率信息, 所以可以正确处理
但是 RtspSdp中为8KHz时, 实际音频采样率可能为16KHz, 这会导致处理错误
通过尝试转码音频出来的包个数 来判断是8KHz还是16KHz

大家好，2023.9.20天津东丽流媒体服务器升级后，在80个流媒体服务器中，有53个设备的流媒体服务器检测到流有异常时间戳跳变。
其中有156路国标流检测到音频时间戳有异常，需要纠正，原因是音频每秒22个包，时间戳增量为92，定期需要回跳和视频包进行时间戳同步。
有127路rtsp流检测到音视频时间戳有异常，需要纠正。原因分为三种：1）音视频时间戳都有异常来回跳跃；2）音视频时间戳都有异常，其中视频时间戳delta dts很多值都为零；3）音频每秒22个包，时间戳增量为92，定期需要回跳和视频包进行时间戳同步

## sLiveGateway业务
1 提供给cc的接口										一期
	1 create_pullChannel;  2 start_pullChannel;
	3 delete_pullChannel;  4 get_pushChannels;
2 调用cc的接口											一期
	1 升级重启上报; 2 流状态上报; 3 未知ssrc上报;
	http://www.cc.com:20093/api/stream/gbGateWayStart
	http://www.cc.com:20093/api/stream/gbStreamStateChange
	http://www.cc.com:20093/api/stream/blackSsrcReport
3 国标收流数据量30秒上报一次							一期
	http://127.0.0.1:8999/api/v1/flowReport
4 国标推流给上级										不做

sLiveGateway功能
1 tcp收流, 丢包率统计
	1 作为tcp服务端, 被动单端口收流 net.ListenTCP()		一期
	  使用fd来区分不同流的数据, 记录首包来源ip
	2 作为tcp客户端, 主动多(随机)端口收流 net.DialTcp()	二期
	3 作为tcp服务端, 被动多端口收流 net.ListenTCP()  	不做
2 udp收流, 丢包率统计	
	1 作为udp服务端, 被动单端口收流 net.ListenUDP()		一期
	  使用ssrc来区分不同流的数据, 记录首包来源ip
	2 作为udp客户端, 主动单端口收流 net.DialUDP()		不存在
	3 作为udp服务端, 被动多端口收流 net.ListenUDP()		不做
3 rtmp协议处理
	1 rtmp推流											一期
	2 rtmp播放											二期
4 收流超时和推流超时									一期
	1 国标收流10秒超时
	2 国标本地录像回看收流, 超时cc指定并传给server
	3 rtmp推流5秒超时
5 音频转AAC 11025Hz, 合理重打音频时间戳					一期
	1 非AAC的音频转AAC
	2 低采样率的转11025Hz采样率
	3 高采样率的转11025Hz采样率
6 日志打印优化											一期
	1 流维度日志打印
	2 日志的分割和删除
7 rtp数据录制											一期
8 rtcp协议处理											二期


### 需要解决的问题
1 播放发送数据，要修改fmt值和修改协议头
2 多个播放在一个协程里，如何避免互相阻塞
golang网络通信超时设置
https://www.cnblogs.com/lanyangsh/p/10852755.html

### sms  
Stream Media Server  

### 流媒体服务器
https://m7s.live/
https://j.m7s.live/player.html
https://www.freecodecamp.org/
https://chinese.freecodecamp.org/
https://www.topgoer.com/

### 编译命令  
go build -o sms main.go http.go rtmp.go  

### 支持的协议  
rtmp -> sms -> rtmp  

### rtmp参考资料
https://blog.csdn.net/lightfish_zhang/article/details/88681828
https://www.cnblogs.com/jimodetiantang/p/8974075.html

### 测试时使用到的命令
go build -o slivehlsupload_mac main.go http.go sqlite.go upload.go mqtt.go stream.go hls.go s3.go
ffmpeg -i GSPm8n46bmUqT-cf6dbn1ehZ_20220105103534_42840.ts -ss 0 -frames:v 1 -f image2 -y test.jpg
ffmpeg -re -stream_loop -1 -i fruit.mp4 -vcodec copy -acodec copy -f flv rtmp://127.0.0.1/live/cctv1
ffmpeg -re -stream_loop -1 -i fruit.mp4 -vcodec copy -acodec copy -f flv rtmp://172.20.25.20/SP63nBbfmlbW/GSP63nBbfmlbW-fnMebne7hV
ffplay rtmp://172.20.25.20/SP63nBbfmlbW/GSP63nBbfmlbW-fnMebne7hU
ffplay http://172.20.25.20:8082/SP63nBbfmlbW/GSP63nBbfmlbW-fnMebne7hU.flv?mediaServerIp=172.20.25.20&codeType=H264
ffplay http://172.20.25.20:8082/SP63nBbfmlbW/GSP63nBbfmlbW-fnMebne7hV.m3u8

### sms待解决问题
1 rtmp收流，rtmp/flv/hls播放
2 支持gateway推流，检查ts生成是否正常
3 支持obs推流，检查ts生成是否正常
4 支持h265
5 支持http截图请求
6 支持flv播放加密
7 停止推流程序崩溃
8 内存使用优化
9 流量统计和上报
10 推流鉴权
11 流状态变更上报
12 支持http获取推流的流id
13 支持http获取每路流的播放个数
14 rtmp推流
15 map要使用sync.Map
	https://pkg.go.dev/sync@go1.17.8
16 使用mqtt发布tsinfo
17 rtp(udp)收流
18 rtp(tcp)收流
19 rtp(udp)推流
20 rtp(tcp)推流
21 rtcp客户端实现
22 rtcp服务端实现
23 音频重采样，出固定采样率
24 rtsp支持
25 webrtc支持
26 websocket支持
