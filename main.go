package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"utils"

	//调试时使用，线上最好关闭
	//_ "net/http/pprof"
	"github.com/kardianos/service"
	"github.com/natefinch/lumberjack"
)

const (
	AppName    = "sms"
	AppVersion = "0.0.1"
	AppConf    = "/usr/local/sms/sms.json"
	BackDoor   = "owner=Spy2023Zjr"
)

var (
	CompileDate     string
	h, v, d, u      bool
	c, RunPath      string
	conf            Config
	RtmpPuberMap    sync.Map //所有rtmp发布者, 含其他协议推rtmp
	RtspPuberMap    sync.Map //所有rtsp发布者, 含其他协议推rtsp
	RtspRtpPortMap  sync.Map //RtpTcp多端口, RtpUdp单/多端口
	GB28181PuberMap sync.Map //所有gb28181发布者, 含其他协议推gb28181

	//SSL/TLS协议信息泄露漏洞(CVE-2016-2183)
	//解决方法 建议：避免使用DES算法
	//MinVersion: tls.VersionTLS13,
	CsArr = []uint16{
		//tls.TLS_RSA_WITH_RC4_128_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		//tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		//tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
		tls.TLS_FALLBACK_SCSV}
)

//嵌套json的解析 有2个要点 否则 获取不到内层json的值
//1 结构体字段名 必须跟json外层字段一样, 数据类型名 可以随意
//2 结构体不能用匿名字段, 必须写字段名名和数据类型名, 两者可以一样
type Config struct {
	IpInner   string
	IpOuter   string
	Cpu       CpuConf
	Log       LogConf
	Http      HttpConf
	Https     HttpsConf
	GB28181   GB28181Conf
	RtpRtcp   RtpRtcpConf
	Rtsp      RtspConf
	Rtmp      RtmpConf
	Flv       FlvConf
	HlsLive   HlsLiveConf
	HlsRec    HlsRecConf
	Mqtt      MqttConf
	StreamRec StreamRecConf
	Debug     DebugConf
	Nt        NetworkTrafficConf
	Cc        CcConf

	AdjustDts          bool
	AdjustPktNum       uint32
	DelayDeleteThred   uint32
	PlayStockMax       int
	DataStockMax       int
	NaluNumPrintEnable bool
	StreamStatekMax    int
	PlayStockWarn      int
	PlaySendBlockMax   int
	MaxPlayerNum       int
	DelayDeleteTime    int
}

type CpuConf struct {
	Enable bool
	UseNum int
}

type LogConf struct {
	FileName      string
	FileSize      int
	FileNum       int
	SaveDay       int
	StreamLogPath string
	PubLogSaveDay int64
	PlayLogCheck  int64
	PlayLogDelete int64
}

type HttpConf struct {
	PortApi  string
	PortMng  string
	PortPlay string
}

type HttpsConf struct {
	Enable   bool
	PortApi  string
	PortMng  string
	PortPlay string
	PubKey   string //公钥 private.key
	PriKey   string //私钥 certificate.crt
}

type GB28181Conf struct {
	SipIp        string
	SipPort      string
	SipIpProxy   string
	SipPortProxy string
	SipId        string
	SipDom       string
}

type RtpRtcpConf struct {
	FixedRtpPort  int
	FixedRtcpPort int
	RangePortMin  int
	RangePortMax  int
}

type RtspConf struct {
	Port              string
	RtpPortMin        int
	RtpPortMax        int
	Rtp2RtspChanNum   int
	Rtp2RtmpChanNum   int
	AvPkt2RtspChanNum int
	AvPkt2RtmpChanNum int
	GopCacheMax       int //最多缓存几个Gop
	GopCacheRsv       int //下一个Gop中有GopCacheRsv个AvPacket才清理上一个Gop, Rsv is Reserve 预留保留的意思
	PushBackDoor      bool
	ReportUrl         string
}

type RtmpConf struct {
	Port              string
	ChunkSize         uint32
	Msg2RtmpChanNum   int
	AvPkt2RtspChanNum int
	GopCacheMax       int
	GopFrameNum       uint32
	BitrateGopNum     uint32
	PublishTimeout    int
}

type FlvConf struct {
	FlvSendDataSize uint32
}

type HlsLiveConf struct {
	Enable    bool
	M3u8TsNum uint32
	TsMaxTime float32
	TsMaxSize uint32
	MemPath   string
	DiskPath  string
}

type HlsRecConf struct {
	Enable      bool
	M3u8TsNum   uint32
	TsMaxTime   float32
	TsMaxSize   uint32
	MemPath     string
	DiskPath    string
	HlsStockMax int
	HlsStoreUse string //"Disk"
}

type MqttConf struct {
	Enable    bool
	Server    string
	ClientId  string
	TopicRec  string //常规录制任务的发布主题
	TopicAi   string //AI录制任务的发布主题
	LogEnable bool
	LogFile   string
}

type StreamRecConf struct {
	Enable   bool
	StreamId string
	SavePath string
}

type DebugConf struct {
	StreamId string
}

type NetworkTrafficConf struct {
	Enable   bool
	Server   string
	Interval int64
}

type CcConf struct {
	Server    string
	ApiAuth   string
	ApiReport string
	ApiKey    string
}

func InitConf(file string) error {
	s, err := utils.ReadAllFile(file)
	if err != nil {
		log.Println(err)
		return err
	}

	err = json.Unmarshal(s, &conf)
	if err != nil {
		log.Println(err)
		return err
	}

	if conf.Cpu.Enable == true {
		ncpu := runtime.NumCPU()
		if conf.Cpu.UseNum < ncpu {
			ncpu = conf.Cpu.UseNum
		}
		runtime.GOMAXPROCS(ncpu)
	}

	err = utils.DirExist(conf.Log.StreamLogPath, true)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}

func InitLog(file string) {
	//return // 前台打印日志
	l := new(lumberjack.Logger)
	l.Filename = conf.Log.FileName
	l.MaxSize = conf.Log.FileSize   // 200BM
	l.MaxBackups = conf.Log.FileNum // 10
	l.MaxAge = conf.Log.SaveDay     // 15

	log.SetOutput(l)
	log.Printf("========================================")
	log.Printf("== AppName: %s", AppName)
	log.Printf("== Version: %s", AppVersion)
	log.Printf("== CompileDate: %s", CompileDate)
	log.Printf("== RuntimePath: %s", RunPath)
	log.Printf("== ByteOrder: %s", GetByteOrder())
	log.Printf("========================================")
	log.Printf("Args: h=%t, v=%t, d=%t, u=%t", h, v, d, u)
	log.Printf("ConfigFile: %s", c)
	log.Printf("%#v", conf)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for {
			<-ch
			l.Rotate()
		}
	}()
}

/*************************************************/
/* 守护进程 且 注册为系统服务(开机启动)
/*************************************************/
type program struct{}

func (p *program) run() {
	RunPath, _ = utils.GetRunPath()

	err := InitConf(c)
	if err != nil {
		log.Println(err)
		return
	}

	InitLog(conf.Log.FileName)
	//go PlayerLogDeleteTimer() //定期清理不更新的播放者日志
	//go PuberLogCutoffTimer() //每天0点分割清理发布者日志
	//go NetworkTrafficTimer() //流量统计与上报

	go SipServerTcp() //for gb28181
	go SipServerUdp() //for gb28181
	go RtpServerTcp() //for gb28181
	go RtpServerUdp() //for gb28181
	go RtspServer()
	go RtmpServer()

	HttpServer() //api, mng, flvPlay, hlsPlay
	select {}
}

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *program) Stop(s service.Service) error {
	return nil
}

func main() {
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)

	flag.BoolVar(&h, "h", false, "print help")
	flag.BoolVar(&v, "v", false, "print version")
	flag.BoolVar(&d, "d", false, "run in deamon")
	flag.BoolVar(&u, "u", false, "stop in deamon")
	flag.StringVar(&c, "c", AppConf, "config file")
	flag.Parse()
	//flag.Usage()
	log.Println("Args:", h, v, d, u, c)
	if h {
		flag.PrintDefaults()
		return
	}
	if v {
		log.Println(AppVersion)
		return
	}

	sc := new(service.Config)
	sc.Name = AppName
	sc.DisplayName = AppName
	sc.Description = AppName

	prg := new(program)
	s, err := service.New(prg, sc)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	if u {
		err = service.Control(s, "stop")
		if err != nil {
			log.Println(err)
		} else {
			log.Println("service stopped")
		}
		err = service.Control(s, "uninstall")
		if err != nil {
			log.Println(err)
		} else {
			log.Println("service uninstalled")
		}
		return
	}

	if !d {
		prg.run()
		return
	}

	err = service.Control(s, "stop")
	if err != nil {
		log.Println(err)
	} else {
		log.Println("service stopped")
	}
	err = service.Control(s, "uninstall")
	if err != nil {
		log.Println(err)
	} else {
		log.Println("service uninstalled")
	}
	err = service.Control(s, "install")
	if err != nil {
		log.Println(err)
	} else {
		log.Println("service installed")
	}
	err = service.Control(s, "start")
	if err != nil {
		log.Println(err)
	} else {
		log.Println("service started")
	}
}
