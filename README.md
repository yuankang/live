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
