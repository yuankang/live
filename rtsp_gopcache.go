package main

import (
	"log"
)

//缓存至少一组gop的rtp包, 用于rtsp快速启播
//iFrame, pFrame, pFrame, pFrame, iFrame, pFrame, pFrame, pFrame
//GopCacheMax, 最多缓存GopCacheMax组gop
//GopCacheRsv, iFrame+pFrame有GopCacheRsv个时, 才清理上一组gop

//发送RtpGop给对方时候, 先发送RtpSpsPps
func RtpSpsPpsPkgCreate(rs *RtspStream) (*RtpPacket, error) {
	spsData := rs.SpsData
	ppsData := rs.PpsData
	if spsData == nil {
		spsData = rs.Sdp.SpsData
		ppsData = rs.Sdp.PpsData
	}
	log.Printf("spsData:%x", spsData)
	log.Printf("ppsData:%x", ppsData)
	return nil, nil
}

func RtpSpsPpsPkgGet(rs *RtspStream) (*RtpPacket, error) {
	if rs.RtpSpsPpsPkt != nil {
		return rs.RtpSpsPpsPkt, nil
	}

	rp, err := RtpSpsPpsPkgCreate(rs)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	rs.RtpSpsPpsPkt = rp
	return rp, nil
}

//发送缓存时, 要修改RtpSpsPpsPkg(一般是第一个rtp包)的seq和timestamp
//更新RtpPkgGopCache时更新RtpSpsPps的序号和时间戳
func RtpSpsPpsPkgUpdate(rs *RtspStream, rp *RtpPacket) {
	e := rs.RtpGopCache.Front()
	p := (e.Value).(*RtpPacket)

	//65533 65534 65535 0 1 2
	rp.SeqNum = p.SeqNum - 1
	if p.SeqNum == 0 {
		rp.SeqNum = 65535
	}
	rp.Timestamp = p.Timestamp

	RtpPkt2Byte(rp)
}

func RtpGopCacheDelete() {

}

//RtpGopCache 的更新在 RtspRtpCacheSort() -> RtspRtps2AvPacket() 里做
//因为可以按 组成一帧的Rtp包 来批量操作 效率更高
func RtpGopCacheUpdate(rs *RtspStream, rps []*RtpPacket, dType string) {
	switch dType {
	case "VideoKeyFrame":
		rs.RtpGopAvPkgNum = 1
		rs.RtpGopCacheNum++

		if rs.RtpGopAvPkgNum >= conf.Rtsp.GopCacheRsv {
			RtpGopCacheDelete()
		}
	case "VideoInterFrame":
		rs.RtpGopAvPkgNum++
		fallthrough
	case "AudioAacFrame", "AudioG711aFrame", "AudioG711uFrame":
	default:
		log.Println("undefined AvPacketType %s", dType)
	}

	for i := 0; i < len(rps); i++ {
		rs.RtpGopCache.PushBack(rps[i])
	}
}
