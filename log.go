package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"livegateway/utils"
)

/*
## 日志路径和命名
/usr/local/appname/appname.log
无法区分是发布者还是播放者时的临时文件名
/usr/local/appname/streamlog/stream_rtmp_1695047824109454545(纳秒).log
/usr/local/appname/streamlog/stream_rtmp_uuid(随机唯一字符串).log
可以区分是发布者还是播放者时的正式文件名
/usr/local/appname/streamlog/streamid/publish_rtmp_20230918.log
/usr/local/appname/streamlog/streamid/play_rtmp_127.0.0.1:36620.log
/usr/local/appname/streamlog/streamid/publish_rtsp_20230918.log
/usr/local/appname/streamlog/streamid/play_rtsp_127.0.0.1:36620.log
/usr/local/appname/streamlog/streamid/rtsp_rtmp_127.0.0.1:1935.log
*/

/*************************************************/
/* 日志文件名
/*************************************************/
//ptcl is protocol
func GetTempLogFn(ptcl string) string {
	ts := utils.GetTimestamp("ns")
	s := fmt.Sprintf("%s/stream_%s_%d.log", conf.Log.StreamLogPath, ptcl, ts)
	log.Printf("TempLogFn: %s", s)
	return s
}

//磁盘满时, 日志创建失败, 没做判断 继续执行 会导致打印日志时崩溃
func StreamLogCreate(fn string) (*log.Logger, *os.File, error) {
	err := os.MkdirAll(path.Dir(fn), 0755)
	if err != nil {
		log.Println(err)
		return nil, nil, err
	}

	//不用的时候 需要关闭f
	f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Println(err)
		return nil, nil, err
	}

	//2022/12/26 20:40:15.811797 log.go:27: xxx
	l := log.New(f, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)
	return l, f, nil
}

func StreamLogRename(oldfn, newfn string) error {
	err := os.MkdirAll(path.Dir(newfn), 0755)
	if err != nil {
		log.Println(err)
		return err
	}

	//文件打开状态下 也可以重命名
	err = os.Rename(oldfn, newfn)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("%s rename to %s", oldfn, newfn)
	return nil
}

/*************************************************/
/* 定期清理不更新的播放者日志
/*************************************************/
func TryDelFile(fis []utils.FileInfo) {
	var err error
	var ts string
	var dt int64
	var dfn int

	ct := utils.GetTimestamp("s")
	pdt := ct - int64(conf.Log.PubLogSaveDay*86400)
	fdt := ct - int64(conf.Log.PlayLogDelete*60)
	l := len(fis)

	for i := 0; i < l; i++ {
		if strings.Contains(fis[i].Name, "publish_") {
			dt = pdt
		} else {
			dt = fdt
		}
		if fis[i].Mtime <= dt {
			ts = utils.TimestampToTimestring(fis[i].Mtime)
			log.Printf("rm %s, mtime:%s", fis[i].Name, ts)

			err = os.Remove(fis[i].Name)
			if err != nil {
				log.Println(err)
				continue
			}
			dfn++
		}
	}
	log.Printf("--- have %d file, delete %d file ---", l, dfn)
}

func PlayerLogDeleteTimer() {
	log.Println("LogDeleteTimer() start")
	err := utils.DirExist(conf.Log.StreamLogPath, true)
	if err != nil {
		log.Fatal(err)
		return
	}

	var fis []utils.FileInfo
	m := time.Duration(conf.Log.PlayLogCheck)
	for {
		log.Println("=== delete PlayLogFile start ===")
		//获取所有文件全路径和最后修改时间
		fis, err = utils.GetAllFile(conf.Log.StreamLogPath)
		if err != nil {
			log.Println(err)
			time.Sleep(m * time.Minute)
			continue
		}

		//删除一小时之前的播放日志文件 和 7天之前的publish日志
		TryDelFile(fis)
		//删除streamlog/streamid空目录
		utils.DelEmptyDir(conf.Log.StreamLogPath)

		log.Println("=== delete PlayLogFile stop ===")
		time.Sleep(m * time.Minute)
	}
}

/*************************************************/
/* 每天0点分割并清理发布者日志
/*************************************************/
func PuberLogCutoffTimer() {
	log.Println("LogCutoffTimer() start")
	err := utils.DirExist(conf.Log.StreamLogPath, true)
	if err != nil {
		log.Fatal(err)
		return
	}

	date := fmt.Sprintf("%s000000", utils.GetYMD())
	sTime := utils.TimestringToTimestamp(date) + 86400
	cTime := utils.GetTimestamp("s")
	iTime := sTime - cTime

	log.Printf("=== slepp %d second, cutoff publish log ===", iTime)
	time.Sleep(time.Second * time.Duration(iTime))

	var p *RtmpStream
	for {
		StreamMap.Range(func(k, v interface{}) bool {
			p, _ = v.(*RtmpStream)
			p.LogCutoff = true
			return true
		})

		log.Println("=== slepp 86400 second, cutoff publish log ===")
		time.Sleep(time.Second * 86400)
	}
}

//该协程启动时获取系统时间, 算出到24:00的时间差(之后的时间差都是1天)
//sleep时间差, 然后 通过chan 发送日志切割消息给
func LogCutoffAction(s *RtmpStream, pType string) error {
	/*
		folder := fmt.Sprintf("%s/%s", conf.Log.StreamLogPath, s.AmfInfo.StreamId)
		fn := fmt.Sprintf("%s/publish_%s_%s.log", folder, pType, utils.GetYMD())

		f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			log.Println(err)
			return err
		}

		if s.logFp != nil {
			log.Printf("close %s", s.LogFn)
			s.logFp.Close()
			s.logFp = nil
		}
		s.logFp = f
		s.LogFn = fn

		s.log = log.New(f, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)
		log.Printf("create %s", s.LogFn)
	*/
	return nil
}
