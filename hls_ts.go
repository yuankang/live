package main

import (
	"bufio"
	"container/list"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"utils"
)

const (
	H264ClockFrequency = 90 // ISO/IEC13818-1中指定, 时钟频率为90kHz
	TsPacketLen        = 188
	PatPid             = 0x0
	PmtPid             = 0x1001
	VideoPid           = 0x100
	AudioPid           = 0x101
	VideoStreamId      = 0xe0
	AudioStreamId      = 0xc0
)

type HlsInfo struct {
	HlsStorePath     string
	M3u8Path         string        // m3u8文件路径, 包含文件名
	M3u8File         *os.File      // m3u8文件描述符
	M3u8Data         string        // m3u8内容
	TsNum            uint32        // m3u8里ts的个数
	TsFirstSeq       uint32        // m3u8里第一个ts的序号
	TsLastSeq        uint32        // m3u8里最后一个ts的序号, 到最大值自动回滚到0
	TsList           *list.List    // 存储tsInfo内容, 双向链表, 删头增尾
	TsFirstTs        uint32        // ts文件中第一个时间戳
	TsExtInfo        float64       // ts文件的播放时长
	TsPath           string        // ts文件路径, 包含文件名
	TsFile           *os.File      // ts文件描述符
	TsFileBuf        *bufio.Writer //有缓存的
	TsData           []byte        //188byte, 可以复用
	TsPack           []byte        //188byte, 可以复用
	VideoCounter     uint8         // 4bit, 0x0 - 0xf 循环
	AudioCounter     uint8         // 4bit, 0x0 - 0xf 循环
	SpsPpsData       []byte        // 视频关键帧tsPacket
	AdtsData         []byte        // 音频tsPacket需要
	M3u8LivePath     string        // live m3u8文件路径, 包含文件名
	M3u8LiveFile     *os.File      // live m3u8文件描述符
	M3u8LiveData     string        // live m3u8内容
	TsLiveNum        uint32        // live m3u8里ts的个数
	TsLiveFirstSeq   uint32        // live m3u8里第一个ts的序号
	TsLiveLastSeq    uint32        // live m3u8里最后一个ts的序号, 到最大值自动回滚到0
	TsLiveList       *list.List    // live 存储tsInfo内容, 双向链表, 删头增尾
	TsLiveFirstTs    uint32        // live ts文件中第一个时间戳
	TsLiveExtInfo    float64       // live ts文件的播放时长
	TsLivePath       string        // live ts文件路径, 包含文件名
	TsLiveFile       *os.File      // live ts文件描述符
	TsLiveFileBuf    *bufio.Writer // live 有缓存的
	TsLivePack       []byte        // live 188byte, 可以复用
	VideoLiveCounter uint8         // live 4bit, 0x0 - 0xf 循环
	AudioLiveCounter uint8         // live 4bit, 0x0 - 0xf 循环
	SpsPpsLiveData   []byte        // 视频关键帧tsPacket
	AdtsLiveData     []byte        // 音频tsPacket需要
	TsLiveRemainName string        // live remain ts name about delay delete
}

// 全部填充0xff, 用于给188字节数据赋初值
var TsPackDefault = [188]byte{
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPat = [188]byte{
	0x47, 0x40, 0x00, 0x10, 0x00, //TS
	0x00, 0xb0, 0x0d, 0x00, 0x01, 0xc1, 0x00, 0x00, //PSI
	0x00, 0x01, 0xf0, 0x01, //PAT
	0x2e, 0x70, 0x19, 0x05, //CRC
	//stuffing 167 bytes
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtH264AAC = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, //TS
	0x02, 0xb0, 0x17, 0x00, 0x01, 0xc1, 0x00, 0x00, //PSI
	0xe1, 0x00, 0xf0, 0x00, //PMT
	0x1b, 0xe1, 0x00, 0xf0, 0x00, //H264
	0x0f, 0xe1, 0x01, 0xf0, 0x00, //AAC
	0x2f, 0x44, 0xb9, 0x9b, //CRC
	//stuffing 157 bytes
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtH265AAC = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, //TS
	0x02, 0xb0, 0x17, 0x00, 0x01, 0xc1, 0x00, 0x00, //PSI
	0xe1, 0x00, 0xf0, 0x00, //PMT
	0x24, 0xe1, 0x00, 0xf0, 0x00, //H265
	0x0f, 0xe1, 0x01, 0xf0, 0x00, //AAC
	0xc7, 0x72, 0xb7, 0xcb, //CRC
	//stuffing 157 bytes
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtH264MP3 = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, //TS
	0x02, 0xb0, 0x17, 0x00, 0x01, 0xc1, 0x00, 0x00, //PSI
	0xe1, 0x00, 0xf0, 0x00, //PMT
	0x1b, 0xe1, 0x00, 0xf0, 0x00, //H264
	0x03, 0xe1, 0x01, 0xf0, 0x00, //MP3
	0x4e, 0x59, 0x3d, 0x1e, //CRC
	//stuffing 157 bytes
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtH265MP3 = [188]byte{}

var TsPackPmtH264 = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, //TS
	0x02, 0xb0, 0x12, 0x00, 0x01, 0xc1, 0x00, 0x00, //PSI
	0xe1, 0x00, 0xf0, 0x00, //PMT
	0x1b, 0xe1, 0x00, 0xf0, 0x00, //H264
	0x15, 0xbd, 0x4d, 0x56, //CRC
	//stuffing 157 + 5 bytes
	0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtH265 = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, /* TS */
	0x02, 0xb0, 0x12, 0x00, 0x01, 0xc1, 0x00, 0x00, /* PSI */
	0xe1, 0x00, 0xf0, 0x00, /* PMT */
	0x24, 0xe1, 0x00, 0xf0, 0x00, /* hevc */
	0x2f, 0x00, 0x6e, 0xe7, /* CRC */
	/* stuffing 157 + 5 bytes */
	0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtAAC = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, /* TS */
	0x02, 0xb0, 0x12, 0x00, 0x01, 0xc1, 0x00, 0x00, /* PSI */
	0xe1, 0x01, 0xf0, 0x00, /* PMT */
	0x0f, 0xe1, 0x01, 0xf0, 0x00, /* aac */
	0xec, 0xe2, 0xb0, 0x94, /* CRC */
	/* stuffing 157 + 5 bytes */
	0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

var TsPackPmtMP3 = [188]byte{ //数据需要核对
	0x47, 0x50, 0x01, 0x10, 0x00, /* TS */
	0x02, 0xb0, 0x12, 0x00, 0x01, 0xc1, 0x00, 0x00, /* PSI */
	0xe1, 0x01, 0xf0, 0x00, /* PMT */
	0x03, 0xe1, 0x01, 0xf0, 0x00, /* mp3 */
	0x8d, 0xff, 0x34, 0x11, /* CRC */
	/* stuffing 157 + 5 bytes */
	0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
}

/*************************************************/
/* http
/*************************************************/
//rtmp://127.0.0.1/live/yuankang
//http://127.0.0.1/live/yuankang.flv
//GET /SP3bnx69BgxI/GSP3bnx69BgxI-avEc0oE4C4.m3u8?xxx=yyy
//GET /SP3bnx69BgxI/GSP3bnx69BgxI-avEc0oE4C4_20221124152056_7.ts?xxx=yyy

// 返回 app, streamid, filename
func GetPlayInfo(url string) (string, string, string) {
	p := strings.Split(url, "?")
	ext := path.Ext(p[0])
	switch ext {
	case ".m3u8", ".flv":
		s := strings.Split(p[0], "/")
		if len(s) < 3 {
			return "", "", ""
		}
		ss := strings.Split(s[2], ".")
		if len(ss) < 1 {
			return "", "", ""
		}
		return s[1], ss[0], s[2] // /app, streamid, fn
	case ".ts":
		dir := path.Dir(p[0]) // /app
		fn := path.Base(p[0]) // streamid_0.ts
		s := strings.Split(fn, "_")
		return dir, s[0], fn
	}
	return "", "", ""
}

func GetTs(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	//app, stream, fn := GetPlayInfo(r.URL.String())
	//file := fmt.Sprintf("%s/%s_%s/%s", conf.HlsLive.MemPath, app, stream, fn)
	_, stream, fn := GetPlayInfo(r.URL.String())
	file := fmt.Sprintf("%s/%s/%s", conf.HlsLive.MemPath, stream, fn)
	log.Println(file)

	d, err := utils.ReadAllFile(file)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return d, nil
}

// HTTP/1.1 GET /SP3bnx69BgxI/GSP3bnx69BgxI-gCec0oMfJT.m3u8?mediaServerIp=172.20.25.20&codeType=H264
func GetM3u8(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	//app, stream, fn := GetPlayInfo(r.URL.String())
	//file := fmt.Sprintf("%s/%s_%s/%s", conf.HlsLive.MemPath, app, stream, fn)
	_, stream, fn := GetPlayInfo(r.URL.String())
	file := fmt.Sprintf("%s/%s/%s", conf.HlsLive.MemPath, stream, fn)
	log.Println(file)

	d, err := utils.ReadAllFile(file)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	log.Println(string(d))
	return d, nil
}

/*************************************************/
/* m3u8
/*************************************************/
// hls协议规范
//这个各个版本都有
//https://datatracker.ietf.org/doc/html/draft-pantos-http-live-streaming-08
//这个只有最新版
//https://www.rfc-editor.org/rfc/rfc8216.html
//#EXT-X-VERSION标签大于等于3时, #EXTINF时长可以为小数

var m3u8Head = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:%d
#EXT-X-MEDIA-SEQUENCE:%d
`
var m3u8Body = `#EXTINF:%.2f,%s
%s`

//m3u8Body举例如下:
//#EXT-X-DISCONTINUITY
//#EXTINF:11.959,[avc|hevc]
//GSP3bnx69BgxI-avEc0oE4C4_20221123085120_6466.ts

//何时添加 #EXT-X-DISCONTINUITY 标签
//1 推流(包括断流重推)生成的第一个tsInfo前要添加
//  监控管理系统里 修改是否加密后 cc会控制重新推流
//2 流信息变更后, 虽然不断流 也需要添加
//  摄像头web管理后台 修改视频编码格式等信息后 摄像头会重新发送音视频头信息 流媒体要自动适配
//s.HlsAddDiscFlag = true 通过这个来控制什么时候添加

type TsInfo struct {
	TsInfoStr  string  // m3u8里ts的记录
	TsExtInfo  float64 // ts文件的播放时长
	TsFilepath string  // ts存储路径 包含文件名
	TsRecType  int     // 录制类型 取值同 RecordType
}

func M3u8Update(s *Stream) {
	s.log.Printf("%s done, #EXTINF:%.3f", path.Base(s.TsPath), s.TsExtInfo)
	if s.TsPath == "" {
		return
	}
	//s.TsNum 初始值为0, conf.Hls.M3u8TsNum 通常为6
	if s.TsNum >= uint32(conf.HlsRec.M3u8TsNum) {
		e := s.TsList.Front()
		// 如果不开启录制, 我们要删除ts文件
		// 如果开启录制, 我们不能删除ts文件, 由上传程序删除
		// 但是我们要 删除list中的ts
		if s.PubAuth.Data.HlsUpload != 1 {
			ti := (e.Value).(TsInfo)
			err := os.Remove(ti.TsFilepath)
			if err != nil {
				log.Println("delete ts error ", ti.TsFilepath, err)
			}
		}
		s.TsList.Remove(e)
		s.TsNum--
		s.TsFirstSeq++
	}

	var tiStr string
	switch s.VideoCodecType {
	case "H264":
		tiStr = fmt.Sprintf(m3u8Body, s.TsExtInfo, "avc", path.Base(s.TsPath))
	case "H265":
		tiStr = fmt.Sprintf(m3u8Body, s.TsExtInfo, "hevc", path.Base(s.TsPath))
	default:
		//sometimes publish stream has no video, use avc to padding
		tiStr = fmt.Sprintf(m3u8Body, s.TsExtInfo, "avc", path.Base(s.TsPath))
	}

	//根据需要 加入 #EXT-X-DISCONTINUITY
	if s.HlsAddDiscFlag == true {
		tiStr = fmt.Sprintf("#EXT-X-DISCONTINUITY\n%s", tiStr)
		s.HlsAddDiscFlag = false
	}
	//s.log.Println(tiStr)

	//ti := TsInfo{tiStr, s.TsExtInfo, "", 0}
	//if 中有多个 && 或者 || 的执行顺序是?
	//对于&&, 从左往右, 遇到一个false, 则停止其它条件的判断, 返回false
	//对于||, 从左往右, 如果遇到一个true, 则停止其它条件的判断, 返回true
	if conf.Mqtt.Enable == true && s.PubAuth.Data.HlsUpload == 1 {
		tiStrs := fmt.Sprintf("%s\n", tiStr)
		tis := TsInfo{tiStrs, s.TsExtInfo, "", 0}
		tis.TsRecType = s.PubAuth.Data.RecordType
		//这里通过chan发送tsInfo给mqtt
		if len(TsInfoChan) < 200 {
			TsInfoChan <- tis
			//s.log.Printf("send mqtt:%#v", tis)
		} else {
			s.log.Printf("TsInfoChan ChanNum=%d overflow", len(TsInfoChan))
		}
	}
	tiStr = fmt.Sprintf("%s\n", tiStr)
	ti := TsInfo{tiStr, s.TsExtInfo, "", 0}
	ti.TsFilepath = s.TsPath
	s.TsList.PushBack(ti)
	s.TsNum++

	var tsMaxTime float64
	var tis string
	for e := s.TsList.Front(); e != nil; e = e.Next() {
		ti = (e.Value).(TsInfo)
		if tsMaxTime < ti.TsExtInfo {
			tsMaxTime = ti.TsExtInfo
		}
		tis = fmt.Sprintf("%s%s", tis, ti.TsInfoStr)
	}

	//s.M3u8Data = fmt.Sprintf(m3u8Head, conf.Hls.TsMaxTime, s.TsFirstSeq)
	s.M3u8Data = fmt.Sprintf(m3u8Head, uint32(math.Ceil(tsMaxTime)), s.TsFirstSeq)
	s.M3u8Data = fmt.Sprintf("%s%s", s.M3u8Data, tis)
	//s.log.Println(s.M3u8Data)

	if utils.FileExist(s.M3u8Path) == false {
		if s.M3u8File != nil {
			s.M3u8File.Close()
			s.M3u8File = nil
		}
		dir := fmt.Sprintf("%s/%s", s.HlsStorePath, s.AmfInfo.StreamId)
		err := os.Mkdir(dir, 0755)
		if err != nil {
			s.log.Println(err)
			//no need return, because dir is exist
		}
		s.M3u8File, err = os.OpenFile(s.M3u8Path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		s.log.Println(s.M3u8Path)
		if err != nil {
			s.log.Println(err)
			return
		}
	} else {
		//清空文件
		err := s.M3u8File.Truncate(0)
		if err != nil {
			s.log.Println(err)
			return
		}
		_, err = s.M3u8File.Seek(0, 0)
		if err != nil {
			s.log.Println(err)
			return
		}
	}
	_, err := s.M3u8File.WriteString(s.M3u8Data)
	if err != nil {
		s.log.Printf("Write %s fail, %s", s.M3u8Path, err)
		return
	}
}

func M3u8Flush(s *Stream) {
	var n *list.Element
	for e := s.TsList.Front(); e != nil; e = n {
		ti := (e.Value).(TsInfo)
		err := os.Remove(ti.TsFilepath)
		if err != nil {
			log.Println("delete ts error ", ti.TsFilepath, err)
		}
		s.log.Printf("delete ts: %s", ti.TsFilepath)
		n = e.Next()
		s.TsList.Remove(e)
	}

	if s.TsPath != "" {
		s.log.Printf("delete last ts: %s", s.TsPath)
		err := os.Remove(s.TsPath)
		if err != nil {
			log.Println("delete ts error ", s.TsPath, err)
		}
		s.TsPath = ""
	}
}

/*************************************************/
/* tsFile里 tsPacket的顺序和结构
/*************************************************/
//rtmp流如何生成ts???
// xxx.ts文件 有很多个 188字节的ts包 组成
// 第1个tsPacket内容为: tsHeader + 0x00 + pat
// 第2个tsPacket内容为: tsHeader + 0x00 + pmt
// 每个关键帧都要有vps, sps和pps
// 关键帧的 PesPacketLength == 0x0000
// 第3个tsPacket内容为: tsHeader + adaptation(pcr) + pesHeader + 0x00000001 + 0x09 + 0xf0 + 0x00000001 + 0x67 + sps + 0x00000001 + 0x68 + pps + 0x00000001 + 0x65 + keyFrame
// 第4个tsPacket内容为: tsHeader + keyFrame
// ...
// 第387个tsPacket内容为: tsHeader + adaptation(填充) + keyFrame(尾部)
// 第388个tsPacket内容为: tsHeader + adaptation(pcr) + pesHeader + 0x00000001 + 0x09 + 0xf0 + 0x00000001 + 0x61 + interFrame
// 第389个tsPacket内容为: tsHeader + interFrame
// ...
// 第480个tsPacket内容为: tsHeader + adaptation(填充) + interFrame(尾部)
// 第481个tsPacket内容为: tsHeader + adaptation(无) + pesHeader + adts + aacFrame
// 第482个tsPacket内容为: tsHeader + aacFrame
// 第483个tsPacket内容为: tsHeader + adaptation(填充) + aacFrame(尾部)
// 第484个tsPacket内容为: tsHeader + aacFrame
// ...

/*************************************************/
/* PrepareSpsPps
/*************************************************/
//0x00000001 + 0x67 + sps + 0x00000001 + 0x68 + pps
//sps内容里第一个就是0x67, pps内容里第一个就是0x68
//h264			   sps:674d00, pps:68ee3c
//h265 vsp:40010c, sps:420101, pps:4401c1
func PrepareSpsPpsData(s *Stream, c *Chunk) {
	// 前5个字节上面已经处理，AVC sequence header从第6个字节开始
	// 0x17 0x00 0x00 0x00 0x00 0x01 0x4d 0x00 0x29 0x03 0x01 0x00 0x18
	// 0x67, 0x4d, 0x0, 0x29, 0x96, 0x35, 0x40, 0xf0, 0x4, 0x4f, 0xcb, 0x37, 0x01, 0x1, 0x1, 0x40, 0x0, 0x0, 0xfa, 0x0, 0x0, 0x17, 0x70, 0x01
	// 0x01 0x00 0x04 0x68 0xee 0x31 0xb2
	s.log.Printf("AVC body data:%v, %x", len(c.MsgData), c.MsgData)
	if len(c.MsgData) < 11 {
		s.log.Printf("AVC body no enough data:%v", len(c.MsgData))
		return
	}
	numOfSps := c.MsgData[10] & 0x1F // 5bit, 0xe1

	var temp uint32
	var spsLen [32]uint16
	var spsData [32][]byte
	var totalSpsLen uint32
	var i uint8
	for i = 0; i < numOfSps; i++ {
		if uint32(len(c.MsgData)) <= (13 + temp) {
			s.log.Printf("AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}
		spsLen[i] = ByteToUint16(c.MsgData[11+temp:13+temp], BE) // 16bit, 0x001c

		if uint32(len(c.MsgData)) <= (11 + temp + uint32(spsLen[i])) {
			s.log.Printf("AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}
		spsData[i] = c.MsgData[13+temp : 13+temp+uint32(spsLen[i])]
		temp += 2 + uint32(spsLen[i])
		totalSpsLen += uint32(spsLen[i])
	}
	EndPos := 11 + temp // 11 + 2 + 28

	numOfPps := c.MsgData[EndPos] // 8bit, 0x01
	var ppsData [256][]byte
	var ppsLen [256]uint16
	var totalPpsLen uint32
	temp = EndPos + 1
	for i = 0; i < numOfPps; i++ {
		if uint32(len(c.MsgData)) <= (2 + temp) {
			s.log.Printf("AVC body no enough data:%d, %v, %x", (2 + temp), len(c.MsgData), c.MsgData)
			return
		}
		ppsLen[i] = ByteToUint16(c.MsgData[temp:2+temp], BE) // 16bit, 0x0004
		if uint32(len(c.MsgData)) < (2 + uint32(ppsLen[i]) + temp) {
			s.log.Printf("AVC body no enough data:%d, %d, %x", (2 + uint32(ppsLen[i]) + temp), len(c.MsgData), c.MsgData)
			return
		}
		ppsData[i] = c.MsgData[2+temp : 2+temp+uint32(ppsLen[i])]
		temp += 2 + uint32(ppsLen[i])
		totalPpsLen += uint32(ppsLen[i])
	}

	s.log.Printf("numOfSps:%d, numOfPps%d, spsLen:%d, spsData:%x, ppsLen:%d, ppsData:%x", numOfSps, numOfPps, spsLen[0], spsData[0], ppsLen[0], ppsData[0])

	size := 4*uint32(numOfSps) + totalSpsLen + 4*uint32(numOfPps) + totalPpsLen
	s.SpsPpsData = make([]byte, size)
	//有些播放器的兼容性不好, 这里最好用0x00000001
	var len1 uint32
	var len2 uint32
	for i = 0; i < numOfSps; i++ {
		Uint32ToByte(0x00000001, s.SpsPpsData[len2:4+len2], BE)
		copy(s.SpsPpsData[4+len2:4+len2+uint32(spsLen[i])], spsData[i])
		len1 += uint32(spsLen[i])
		len2 = 4 + len1
	}

	for i = 0; i < numOfPps; i++ {
		Uint32ToByte(0x00000001, s.SpsPpsData[len2:4+len2], BE)
		copy(s.SpsPpsData[4+len2:4+len2+uint32(ppsLen[i])], ppsData[i])
		len1 += uint32(ppsLen[i])
		len2 = 4 + len1
	}
}

// NaluLen and NaluData may be not consistent, should parsing chunk data instead of using HevcC data
func PrepareSpsPpsDataH265(s *Stream, c *Chunk) {
	if len(c.MsgData) < 28 {
		s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
		return
	}
	//len(c.MsgData) = 135
	//1c 00 00 00 00
	//前5个字节上面已经处理，HEVC sequence header从第6个字节开始
	//01 01 60 00 00 00 80 00 00 00
	//00 00 78 f0 00 fc fd f8 f8 00
	//00 ff 03 20 00 01 00 17 40 01
	//0c 01 ff ff 01 60 00 00 03 00
	//80 00 00 03 00 00 03 00 78 ac
	//09 21 00 01 00 3c 42 01 01 01
	//60 00 00 03 00 80 00 00 03 00
	//00 03 00 78 a0 02 80 80 2d 1f
	//e3 6b bb c9 2e b0 16 e0 20 20
	//20 80 00 01 f4 00 00 30 d4 39
	//0e f7 28 80 3d 30 00 44 de 00
	//7a 60 00 89 bc 40 22 00 01 00
	//09 44 01 c1 72 b0 9c 38 76 24
	var HevcC HEVCDecoderConfigurationRecord
	HevcC.ConfigurationVersion = c.MsgData[5] // 8bit, 0x01
	// 中间这些字段, 我们不关心
	HevcC.NumOfArrays = c.MsgData[27] // 8bit, 一般为3
	//s.log.Printf("hevc %x, %x", HevcC.ConfigurationVersion, HevcC.NumOfArrays)

	var i, j, k uint16 = 0, 28, 0
	var hn HVCCNALUnit
	for ; i < uint16(HevcC.NumOfArrays); i++ {
		if len(c.MsgData) < int(j+3) {
			s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}

		hn.ArrayCompleteness = c.MsgData[j] >> 7
		hn.Reserved0 = (c.MsgData[j] >> 6) & 0x1
		hn.NALunitType = c.MsgData[j] & 0x3f
		j++
		//hn.NumNalus > 1 这种情况非常少, ffmpeg里只会写一个, srs代码里会判断是否为多个
		//协议规定可以有多个vps sps..., 建议使用第一个vps sps...,我们使用最后一个vps sps...
		hn.NumNalus = ByteToUint16(c.MsgData[j:j+2], BE)
		j += 2
		for k = 0; k < hn.NumNalus; k++ {
			if len(c.MsgData) < int(j+2) {
				s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				return
			}
			hn.NaluLen = ByteToUint16(c.MsgData[j:j+2], BE)
			j += 2
			if len(c.MsgData) < int(j+hn.NaluLen) {
				s.log.Printf("HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				return
			}
			hn.NaluData = c.MsgData[j : j+hn.NaluLen]
			j += hn.NaluLen
			s.log.Printf("%#v", hn)

			switch hn.NALunitType {
			case 32: // 0x20
				s.log.Printf("NaluType=%d is VPS", hn.NALunitType)
				HevcC.Vps = append(HevcC.Vps, hn)
			case 33: // 0x21
				s.log.Printf("NaluType=%d is SPS", hn.NALunitType)
				HevcC.Sps = append(HevcC.Sps, hn)
			case 34: // 0x22
				s.log.Printf("NaluType=%d is PPS", hn.NALunitType)
				HevcC.Pps = append(HevcC.Pps, hn)
			case 39: // 0x27
				s.log.Printf("NaluType=%d is SEI", hn.NALunitType)
				HevcC.Sei = append(HevcC.Sei, hn)
			default:
				s.log.Printf("NaluType=%d untreated", hn.NALunitType)
			}
		}
	}

	var size uint16
	for _, value := range HevcC.Vps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	for _, value := range HevcC.Sps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	for _, value := range HevcC.Pps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	//size := 12 + HevcC.Vps.NaluLen + HevcC.Sps.NaluLen + HevcC.Pps.NaluLen
	s.SpsPpsData = make([]byte, size)

	var sp uint16
	//有些播放器的兼容性不好, 这里最好用0x00000001
	for _, value := range HevcC.Vps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
	for _, value := range HevcC.Sps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
	for _, value := range HevcC.Pps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
}

/*************************************************/
/* PrepareAdts
/*************************************************/
// FF F9 50 80 2E 7F FC
// 11111111 11111001 01010000 10000000 00101110 01111111 11111100
// fff 1 00 1 01 0100 0 010 0 0 0 0 0000101110011 11111111111 00
// 366-2=364, 371-264=7, 7字节adts  0x173 = 371
//ProfileObjectType            uint8  // 2bit
// 0	Main profile
// 1	Low Complexity profile(LC)
// 2	Scalable Sampling Rate profile(SSR)
// 3	(reserved)
//SamplingFrequencyIndex       uint8  // 4bit, 使用的采样率下标
// 0: 96000 Hz
// 1: 88200 Hz
// 2: 64000 Hz
// 3: 48000 Hz
// 4: 44100 Hz
// 5: 32000 Hz
// 6: 24000 Hz
// 7: 22050 Hz
// 8: 16000 Hz
// 9: 12000 Hz
// 10: 11025 Hz
// 11: 8000 Hz
// 12: 7350 Hz
// 13: Reserved
// 14: Reserved
// 15: frequency is written explictly
//打包aac⾳频必须加上⼀个adts(Audio Data Transport Stream)头，共7Byte，adts包括fixed_header和variable_header两部分，各28bit。
// ADTS 定义在 ISO 14496-3, P122
// 固定头信息 + 可变头信息(home之后，不包括home)
//28bit固定头 + 28bit可变头 = 56bit, 7byte
type Adts struct {
	Syncword               uint16 // 12bit, 固定值0xfff
	Id                     uint8  // 1bit, 固定值0x1, 0:MPEG-4, 1:MPEG-2
	Layer                  uint8  // 2bit, 固定值00
	ProtectionAbsent       uint8  // 1bit, 0表示有CRC校验, 1表示没有CRC校验
	ProfileObjectType      uint8  // 2bit, 表示使用哪个级别的AAC，有些芯片只支持AAC LC
	SamplingFrequencyIndex uint8  // 4bit, 使用的采样率下标
	PrivateBit             uint8  // 1bit, 固定值0
	ChannelConfiguration   uint8  // 3bit, 表示声道数
	OriginalCopy           uint8  // 1bit, 固定值0
	Home                   uint8  // 1bit, 固定值0

	CopyrightIdentificationBit   uint8  // 1bit, 固定值0
	CopyrightIdentificationStart uint8  // 1bit, 固定值0
	AacFrameLength               uint16 // 13bit, 一个ADTS帧的长度包括ADTS头和AAC原始流, AacFrameLength = (ProtectionAbsent==1?7:9)+AACFrameSize
	AdtsBufferFullness           uint16 // 11bit, 0x7FF 说明是码率可变的码流
	NumberOfRawDataBlocksInFrame uint8  // 2bit, 固定值00, 表示ADTS帧中有NumberOfRawDataBlocksInFrame+1个AAC原始帧
}

// ffmpeg-4.4.1/libavcodec/adts_header.c
// ff_adts_header_parse() ffmpeg中解析adts的代码
func PrepareAdtsData(s *Stream, c *Chunk) {
	var AacC AudioSpecificConfig
	AacC.ObjectType = (c.MsgData[2] & 0xF8) >> 3 // 5bit
	AacC.SamplingIdx =
		((c.MsgData[2] & 0x7) << 1) | (c.MsgData[3] >> 7) // 4bit
	AacC.ChannelNum = (c.MsgData[3] & 0x78) >> 3     // 4bit
	AacC.FrameLenFlag = (c.MsgData[3] & 0x4) >> 2    // 1bit
	AacC.DependCoreCoder = (c.MsgData[3] & 0x2) >> 1 // 1bit
	AacC.ExtensionFlag = c.MsgData[3] & 0x1          // 1bit
	// 2, 4, 2, 0(1024), 0, 0
	s.log.Printf("%#v", AacC)

	//ff f9 50 80 00 ff fc  //自己测试文件自己代码生成的
	// 11111111 11111001 01010000 10000000 00000000 11111111 11111100
	// fff 1 00 1 01 0100 0 010 0 0 0 0 0000000000111 11111111111 00
	//FF F9 68 40 5C FF FC  //别人测试ts文件直接读取的
	// 11111111 11111001 01101000 01000000 01011100 11111111 11111100
	// fff 1 00 1 01 1010 0 001 0 0 0 0 0001011100111 11111111111 00
	var adts Adts
	adts.Syncword = 0xfff
	adts.Id = 0x1 // 1bit, MPEG Version: 0 is MPEG-4, 1 is MPEG-2
	adts.Layer = 0x0
	adts.ProtectionAbsent = 0x1
	adts.ProfileObjectType = AacC.ObjectType - 1
	adts.SamplingFrequencyIndex = AacC.SamplingIdx
	adts.PrivateBit = 0x0
	adts.ChannelConfiguration = AacC.ChannelNum
	adts.OriginalCopy = 0x0
	adts.Home = 0x0
	adts.CopyrightIdentificationBit = 0x0
	adts.CopyrightIdentificationStart = 0x0
	// 这里不知道aac数据长度, 所以先复制为0x7
	adts.AacFrameLength = 0x7
	adts.AdtsBufferFullness = 0x7ff
	adts.NumberOfRawDataBlocksInFrame = 0x0
	//s.log.Printf("%#v", adts)

	s.AdtsData = make([]byte, 7)
	s.AdtsData[0] = 0xff
	s.AdtsData[1] = 0xf0 | ((adts.Id & 0x1) << 3) | ((adts.Layer & 0x3) << 1) | (adts.ProtectionAbsent & 0x1)
	s.AdtsData[2] = ((adts.ProfileObjectType & 0x3) << 6) | ((adts.SamplingFrequencyIndex & 0xf) << 2) | ((adts.PrivateBit & 0x1) << 1) | ((adts.ChannelConfiguration & 0x4) >> 2)
	s.AdtsData[3] = ((adts.ChannelConfiguration & 0x3) << 6) | ((adts.OriginalCopy & 0x1) << 5) | ((adts.Home & 0x1) << 4) | ((adts.CopyrightIdentificationBit & 0x1) << 3) | ((adts.CopyrightIdentificationStart & 0x1) << 2) | uint8((adts.AacFrameLength>>11)&0x3)
	s.AdtsData[4] = uint8((adts.AacFrameLength >> 3) & 0xff)
	s.AdtsData[5] = (uint8(adts.AacFrameLength&0x7) << 5) | uint8((adts.AdtsBufferFullness>>6)&0x1f)
	s.AdtsData[6] = (uint8((adts.AdtsBufferFullness & 0x3f) << 2)) | (adts.NumberOfRawDataBlocksInFrame & 0x3)
	//s.log.Printf("AdtsData: %x", s.AdtsData)
}

func ParseAdtsData(s *Stream) Adts {
	var adts Adts
	data := s.AdtsData
	adts.Syncword = uint16(data[0])<<4 | uint16(data[1])>>4
	adts.Id = (data[1] >> 3) & 0x1
	adts.Layer = (data[1] >> 1) & 0x3
	adts.ProtectionAbsent = data[1] & 0x1
	adts.ProfileObjectType = (data[2] >> 6) & 0x3
	adts.SamplingFrequencyIndex = (data[2] >> 2) & 0xf
	adts.PrivateBit = (data[2] >> 1) & 0x1
	adts.ChannelConfiguration = ((data[2] & 0x1) << 2) | (data[3]>>6)&0x3
	adts.OriginalCopy = (data[3] >> 5) & 0x1
	adts.Home = (data[3] >> 4) & 0x1
	adts.CopyrightIdentificationBit = (data[3] >> 3) & 0x1
	adts.CopyrightIdentificationStart = (data[3] >> 2) & 0x1
	adts.AacFrameLength = ((uint16(data[3]) & 0x3) << 11) | uint16(data[4])<<3 | (uint16(data[5])>>5)&0x7
	adts.AdtsBufferFullness = ((uint16(data[5]) & 0x1f) << 6) | (uint16(data[6])>>2)&0x3f
	adts.NumberOfRawDataBlocksInFrame = data[6] & 0x3
	//s.log.Printf("%#v", adts)
	return adts
}

// size = adts头(7字节) + aac数据长度
// 函数A 把[]byte 传给函数B, B修改后 A里的值也会变
func SetAdtsLength(d []byte, size uint16) {
	d[3] = (d[3] & 0xfc) | uint8((size>>11)&0x2)      // 最右2bit
	d[4] = (d[4] & 0x00) | uint8((size>>3)&0xff)      // 8bit
	d[5] = (d[5] & 0x1f) | (uint8((size & 0x7) << 5)) // 最左3bit
}

/*************************************************/
/* crc
/*************************************************/
var crcTable = []uint32{
	0x00000000, 0x04c11db7, 0x09823b6e, 0x0d4326d9,
	0x130476dc, 0x17c56b6b, 0x1a864db2, 0x1e475005,
	0x2608edb8, 0x22c9f00f, 0x2f8ad6d6, 0x2b4bcb61,
	0x350c9b64, 0x31cd86d3, 0x3c8ea00a, 0x384fbdbd,
	0x4c11db70, 0x48d0c6c7, 0x4593e01e, 0x4152fda9,
	0x5f15adac, 0x5bd4b01b, 0x569796c2, 0x52568b75,
	0x6a1936c8, 0x6ed82b7f, 0x639b0da6, 0x675a1011,
	0x791d4014, 0x7ddc5da3, 0x709f7b7a, 0x745e66cd,
	0x9823b6e0, 0x9ce2ab57, 0x91a18d8e, 0x95609039,
	0x8b27c03c, 0x8fe6dd8b, 0x82a5fb52, 0x8664e6e5,
	0xbe2b5b58, 0xbaea46ef, 0xb7a96036, 0xb3687d81,
	0xad2f2d84, 0xa9ee3033, 0xa4ad16ea, 0xa06c0b5d,
	0xd4326d90, 0xd0f37027, 0xddb056fe, 0xd9714b49,
	0xc7361b4c, 0xc3f706fb, 0xceb42022, 0xca753d95,
	0xf23a8028, 0xf6fb9d9f, 0xfbb8bb46, 0xff79a6f1,
	0xe13ef6f4, 0xe5ffeb43, 0xe8bccd9a, 0xec7dd02d,
	0x34867077, 0x30476dc0, 0x3d044b19, 0x39c556ae,
	0x278206ab, 0x23431b1c, 0x2e003dc5, 0x2ac12072,
	0x128e9dcf, 0x164f8078, 0x1b0ca6a1, 0x1fcdbb16,
	0x018aeb13, 0x054bf6a4, 0x0808d07d, 0x0cc9cdca,
	0x7897ab07, 0x7c56b6b0, 0x71159069, 0x75d48dde,
	0x6b93dddb, 0x6f52c06c, 0x6211e6b5, 0x66d0fb02,
	0x5e9f46bf, 0x5a5e5b08, 0x571d7dd1, 0x53dc6066,
	0x4d9b3063, 0x495a2dd4, 0x44190b0d, 0x40d816ba,
	0xaca5c697, 0xa864db20, 0xa527fdf9, 0xa1e6e04e,
	0xbfa1b04b, 0xbb60adfc, 0xb6238b25, 0xb2e29692,
	0x8aad2b2f, 0x8e6c3698, 0x832f1041, 0x87ee0df6,
	0x99a95df3, 0x9d684044, 0x902b669d, 0x94ea7b2a,
	0xe0b41de7, 0xe4750050, 0xe9362689, 0xedf73b3e,
	0xf3b06b3b, 0xf771768c, 0xfa325055, 0xfef34de2,
	0xc6bcf05f, 0xc27dede8, 0xcf3ecb31, 0xcbffd686,
	0xd5b88683, 0xd1799b34, 0xdc3abded, 0xd8fba05a,
	0x690ce0ee, 0x6dcdfd59, 0x608edb80, 0x644fc637,
	0x7a089632, 0x7ec98b85, 0x738aad5c, 0x774bb0eb,
	0x4f040d56, 0x4bc510e1, 0x46863638, 0x42472b8f,
	0x5c007b8a, 0x58c1663d, 0x558240e4, 0x51435d53,
	0x251d3b9e, 0x21dc2629, 0x2c9f00f0, 0x285e1d47,
	0x36194d42, 0x32d850f5, 0x3f9b762c, 0x3b5a6b9b,
	0x0315d626, 0x07d4cb91, 0x0a97ed48, 0x0e56f0ff,
	0x1011a0fa, 0x14d0bd4d, 0x19939b94, 0x1d528623,
	0xf12f560e, 0xf5ee4bb9, 0xf8ad6d60, 0xfc6c70d7,
	0xe22b20d2, 0xe6ea3d65, 0xeba91bbc, 0xef68060b,
	0xd727bbb6, 0xd3e6a601, 0xdea580d8, 0xda649d6f,
	0xc423cd6a, 0xc0e2d0dd, 0xcda1f604, 0xc960ebb3,
	0xbd3e8d7e, 0xb9ff90c9, 0xb4bcb610, 0xb07daba7,
	0xae3afba2, 0xaafbe615, 0xa7b8c0cc, 0xa379dd7b,
	0x9b3660c6, 0x9ff77d71, 0x92b45ba8, 0x9675461f,
	0x8832161a, 0x8cf30bad, 0x81b02d74, 0x857130c3,
	0x5d8a9099, 0x594b8d2e, 0x5408abf7, 0x50c9b640,
	0x4e8ee645, 0x4a4ffbf2, 0x470cdd2b, 0x43cdc09c,
	0x7b827d21, 0x7f436096, 0x7200464f, 0x76c15bf8,
	0x68860bfd, 0x6c47164a, 0x61043093, 0x65c52d24,
	0x119b4be9, 0x155a565e, 0x18197087, 0x1cd86d30,
	0x029f3d35, 0x065e2082, 0x0b1d065b, 0x0fdc1bec,
	0x3793a651, 0x3352bbe6, 0x3e119d3f, 0x3ad08088,
	0x2497d08d, 0x2056cd3a, 0x2d15ebe3, 0x29d4f654,
	0xc5a92679, 0xc1683bce, 0xcc2b1d17, 0xc8ea00a0,
	0xd6ad50a5, 0xd26c4d12, 0xdf2f6bcb, 0xdbee767c,
	0xe3a1cbc1, 0xe760d676, 0xea23f0af, 0xeee2ed18,
	0xf0a5bd1d, 0xf464a0aa, 0xf9278673, 0xfde69bc4,
	0x89b8fd09, 0x8d79e0be, 0x803ac667, 0x84fbdbd0,
	0x9abc8bd5, 0x9e7d9662, 0x933eb0bb, 0x97ffad0c,
	0xafb010b1, 0xab710d06, 0xa6322bdf, 0xa2f33668,
	0xbcb4666d, 0xb8757bda, 0xb5365d03, 0xb1f740b4,
}

func Crc32Create(src []byte) uint32 {
	crc32 := uint32(0xFFFFFFFF)
	j := byte(0)
	for i := 0; i < len(src); i++ {
		j = (byte(crc32>>24) ^ src[i]) & 0xff
		crc32 = uint32(uint32(crc32<<8) ^ uint32(crcTable[j]))
	}
	return crc32
}

/*************************************************/
/* tsHeader
/*************************************************/
//===> PID
//0x0000	表示PAT
//0x0001	表示CAT
//0x1fff	表示空包
//===> PayloadUnitStartIndicator
//PayloadUnitStartIndicator 表示负载数据前是否存在pointer_field
//PayloadUnitStartIndicator=1(表示存在pointer_field)且负载是PSI信息时
//pointer_field(8bits)的值为此字段之后到有效负载之间的字节数
//pointer_field=0 表示下一个字节即是有效负载的第一个字节
//PAT和PMT PayloadUnitStartIndicator=1 且有pointer_field
//视频(I/P/B)和音频数据的ts首包 PayloadUnitStartIndicator=1 但是无pointer_field
//视频(I/P/B)和音频数据的ts非首包 PayloadUnitStartIndicator=0
//===> AdaptationFieldControl
//0x0	是保留值
//0x1	无调整字段，仅含有效负载
//0x2	仅含调整字段，无有效负载
//0x3	调整字段后含有效负载
//如果AdaptationFieldControl=00或10, ContinuityCounter不增加, 因为不含payload
//1+2+1=4byte
type TsHeader struct {
	SyncByte                   uint8  // 8bit, 同步字节 固定值0x47, 后面的数据是不会出现0x47的
	TransportErrorIndicator    uint8  // 1bit, 传输错误标志, 一般传输错误的话就不会处理这个包了
	PayloadUnitStartIndicator  uint8  // 1bit, 负载数据前是否存在pointer_field
	TransportPriority          uint8  // 1bit, 传输优先级, 1表示高优先级
	PID                        uint16 // 13bit, TS包的数据类型
	TransportScramblingControl uint8  // 2bit, 传输加扰控制, 00表示未加密
	AdaptationFieldControl     uint8  // 2bit, 适应域控制
	ContinuityCounter          uint8  // 4bit, 连续计数器, 0x0-0xf循环
}

// 打包ts流时PAT和PMT表(属于文本数据)是没有adaptation field
// 音视频的帧的第一个和最后一个188字节中 要有 适应区
// 中间的ts包不加adaptation field
// 视频帧  第一个188字节中, 适应区要有pcr无填充
// 视频帧最后一个188字节中, 适应区要无pcr有填充
// 音频帧  第一个188字节中, 适应区要无pcr无填充
// 音频帧最后一个188字节中, 适应区要无pcr有填充
// 在测试的时候发现, 如果没有⾃适应区, ipad是可以播放的, 但vlc⽆法播放
// PAT和PMT表不需要adaptation field的, 不够长度直接补0xff即可
// PCR是节目时钟参考，也是一种音视频同步的时钟
// pcr/dts/pts都是对同⼀个系统时钟的采样值, pcr可以设置为dts值
// ⾳频数据不需要pcr
// ISO_IEC_13818-01_2007.pdf, 2.4.3.4 Adaptation field, P34 P22
// 8+8=16bit, 2Byte, pcr是6字节
type Adaptation struct {
	AdaptationFieldLength             uint8 // 8bit, 自适应区长度, 不含本字段, 含填充数据
	DiscontinuityIndicator            uint8 // 1bit, 1指示当前包的不连续状态为true; 0则当前包的不连续状态为false. 用于指示两种类型的不连续性: 系统时基的不连续性和连续计数器的不连续性
	RandomAccessIndicator             uint8 // 1bit, 1指示当前包和后续具有相同PID
	ElementaryStreamPriorityIndicator uint8 // 1bit, es优先级, 0为低优先级
	PcrFlag                           uint8 // 1bit, 1表示有pcr信息
	OpcrFlag                          uint8 // 1bit, 0即无OPCR
	SplicingPointFlag                 uint8 // 1bit, 0即无splice_countdown字段
	TransportPrivateDataFlag          uint8 // 1bit, 0即无private_data
	AdaptationFieldExtensionFlag      uint8 // 1bit, 0即无adaptation field扩展
}

func NewAdaptation(c Chunk, first bool) *Adaptation {
	var a Adaptation              //2byte + pcr是6byte
	a.AdaptationFieldLength = 0x1 //2-1=1, 没有pcr, 还要加上填充数据长度
	a.DiscontinuityIndicator = 0x0
	//RandomAccessIndicator的值 iso/iec-13818-1，协议没有明确规定
	//keyFrame时这个值为1, interFrame和aacFrame为0
	a.RandomAccessIndicator = 0x0
	if first && c.DataType == "VideoKeyFrame" {
		a.RandomAccessIndicator = 0x1
	}
	a.ElementaryStreamPriorityIndicator = 0x0
	a.PcrFlag = 0x0
	if first == true && (c.DataType == "VideoKeyFrame" || c.DataType == "VideoInterFrame") {
		a.AdaptationFieldLength = 0x7 //2-1+6=7, 有pcr, 还要加上填充数据长度
		a.PcrFlag = 0x1
	}
	a.OpcrFlag = 0x0
	a.SplicingPointFlag = 0x0
	a.TransportPrivateDataFlag = 0x0
	a.AdaptationFieldExtensionFlag = 0x0
	return &a
}

func PackPcr(d []byte, pcr uint64) {
	//pcr是6byte 33+6+9=48
	//program_clock_reference_base			33bit
	//Reserved								6bit
	//program_clock_reference_extension		9bit
	//pcr是如何写进去的?
	//pcrdata:
	//00000000 0000000a aaaaaaab bbbbbbbc cccccccd ddddddde
	//                         25       17       9       1
	//00000000 aaaaaaaa bbbbbbbb cccccccc dddddddd e0000000
	//                                           7
	//tsData:
	//                                   33     6         9
	//aaaaaaaa bbbbbbbb cccccccc dddddddd e1111110 00000000
	d[0] = uint8(pcr >> 25) //48-25=23, 取左边23位的低8位
	d[1] = uint8(pcr >> 17)
	d[2] = uint8(pcr >> 9)
	d[3] = uint8(pcr >> 1)
	d[4] = uint8(pcr<<7) | 0x7e //01111110
	d[5] = 0
}

// 标准规定在原始音频和视频流中，PTS的间隔不能超过0.7s，而出现在TS包头的PCR间隔不能超过0.1s。
// 假设a，b两人约定某个时刻去做某事，则需要一个前提，他们两人的手表必须是同步的，比如都是使用北京时间，如果他们的表不同步，就会错过约定时刻。pcr就是北京时间，编码器将自己的系统时钟采样，以pcr形式放入ts，解码器使用pcr同步自己的系统时钟，保证编码器和解码器的时钟同步。
// PCR 系统参考时钟, PCR 是 TS 流中才有的概念, 用于恢复出与编码端一致的系统时序时钟STC（System Time Clock）
// PCR多长时间循环一次, (0x1FFFFFFFF*300+299)/27000000/3600 约为 26.5 小时
// 33 + 6 + 9 = 48bit, 6Byte
type Pcr struct {
	ProgramClockReferenceBase      uint64 // 33bit
	Reserved                       uint8  // 6bit
	ProgramClockReferenceExtension uint16 // 9bit
}

// PCR的插入必须在PCR字段的最后离开复用器的那一时刻，同时把27MHz系统时钟的采样瞬时值作为PCR字段插入到相应的PCR域。是放在TS包头的自适应区中传送。27 MHz时钟经波形整理后分两路，一路是由27MHz脉冲直接触发计数器生成扩展域PCR_ext，长度为9bits。另一路经一个300分频器后的90 kHz脉冲送入一个33位计数器生成90KHZ基值，列入PCR_base（基值域），长度33bits，用于和PTS/DTS比较，产生解码和显示所需要的同步信号。这两部分被置入PCR域，共同组成了42位的PCR。

/*************************************************/
/* tsPakcet
/*************************************************/
//tsPacket固定188byte, tsHeader固定4byte
//返回tsData和写入tsData的字节数, 也就是data消耗了多少
//tsHeader(4) + [适应区(2+6)] + [填充数据(x)] + (pat/pmt/pes)数据(x)
func TsPacketCreate(s *Stream, c Chunk, data []byte, pcr uint64, first bool) ([]byte, int) {
	dataLen := len(data)

	var th TsHeader
	th.SyncByte = 0x47
	th.TransportErrorIndicator = 0x0
	th.PayloadUnitStartIndicator = 0x0
	if first {
		th.PayloadUnitStartIndicator = 0x1
	}
	th.TransportPriority = 0x0
	th.PID = VideoPid
	if c.DataType == "AudioAacFrame" {
		th.PID = AudioPid
	}
	th.TransportScramblingControl = 0x0
	th.AdaptationFieldControl = 0x1
	//最后一段数据 只有182字节可用于放数据, 188-tsHeader(4)-适应区(2)
	if first || dataLen <= 183 {
		th.AdaptationFieldControl = 0x3
	}

	/*
	   adaptation_field_length 的值将为 0~182 之间,
	   值为 0 是为了在 TS Packet 中插入单个的填充字节 (非常重要)
	   帧尾部数据剩余188字节 无适应区, 4+184 剩余4字节
	   帧尾部数据剩余187字节 无适应区, 4+184 剩余3字节
	   帧尾部数据剩余186字节 无适应区, 4+184 剩余2字节
	   帧尾部数据剩余185字节 无适应区, 4+184 剩余1字节
	   帧尾部数据剩余184字节 无适应区, 4+184 剩余0字节
	   帧尾部数据剩余183字节 有适应区, 4+1+183 填充0字节 (非常重要)
	   帧尾部数据剩余182字节 有适应区, 4+2+182 填充0字节
	   帧尾部数据剩余181字节 有适应区, 4+2+181 填充1字节
	   帧尾部数据剩余180字节 有适应区, 4+2+180 填充2字节
	*/

	switch th.PID {
	case AudioPid:
		th.ContinuityCounter = s.AudioCounter
		s.AudioCounter++
		if s.AudioCounter > 0xf {
			s.AudioCounter = 0x0
		}
	case VideoPid:
		th.ContinuityCounter = s.VideoCounter
		s.VideoCounter++
		if s.VideoCounter > 0xf {
			s.VideoCounter = 0x0
		}
	}

	var a *Adaptation
	if th.AdaptationFieldControl == 0x3 {
		a = NewAdaptation(c, first)

		if dataLen == 183 {
			a.AdaptationFieldLength = 0x0
		}
	}

	useLen := 0
	copy(s.TsPack, TsPackDefault[:])

	s.TsPack[0] = th.SyncByte
	s.TsPack[1] = ((th.TransportErrorIndicator & 0x1) << 7) | ((th.PayloadUnitStartIndicator & 0x1) << 6) | ((th.TransportPriority & 0x1) << 5) | (uint8((th.PID & 0x1f00) >> 8))
	s.TsPack[2] = uint8(th.PID & 0xff)
	s.TsPack[3] = ((th.TransportScramblingControl & 0x3) << 6) | ((th.AdaptationFieldControl & 0x3) << 4) | (th.ContinuityCounter & 0xf)
	useLen += 4

	if th.AdaptationFieldControl == 0x3 {
		s.TsPack[4] = a.AdaptationFieldLength
		useLen += 1

		if a.AdaptationFieldLength != 0x0 {
			s.TsPack[5] = ((a.DiscontinuityIndicator & 0x1) << 7) | ((a.RandomAccessIndicator & 0x1) << 6) | ((a.ElementaryStreamPriorityIndicator & 0x1) << 5) | ((a.PcrFlag & 0x1) << 4) | ((a.OpcrFlag & 0x1) << 3) | ((a.SplicingPointFlag & 0x1) << 2) | ((a.TransportPrivateDataFlag & 0x1) << 1) | (a.AdaptationFieldExtensionFlag & 0x1)
			useLen += 1
		}

		if a.PcrFlag == 0x1 {
			PackPcr(s.TsPack[useLen:], pcr)
			useLen += 6
		}
	}

	remainLen := 188 - useLen
	//s.log.Printf("dataLen=%d, freeBuffLen=%d", dataLen, freeBuffLen)
	if dataLen >= remainLen {
		dataLen = remainLen
		copy(s.TsPack[useLen:useLen+remainLen], data)
	} else {
		padLen := 188 - useLen - dataLen
		if th.AdaptationFieldControl == 0x3 {
			s.TsPack[4] = a.AdaptationFieldLength + uint8(padLen)
		}
		copy(s.TsPack[188-dataLen:], data)
	}
	return s.TsPack, dataLen
}

func TsPacketCreatePatPmt(s *Stream, pid uint16, data []byte) ([]byte, int) {
	var th TsHeader
	th.SyncByte = 0x47                  // 8bit
	th.TransportErrorIndicator = 0x0    // 1bit
	th.PayloadUnitStartIndicator = 0x1  // 1bit
	th.TransportPriority = 0x0          // 1bit
	th.PID = pid                        // 13bit
	th.TransportScramblingControl = 0x0 // 2bit
	th.AdaptationFieldControl = 0x1     // 2bit
	th.ContinuityCounter = 0x0          // 4bit

	copy(s.TsPack, TsPackDefault[:])

	s.TsPack[0] = th.SyncByte
	s.TsPack[1] = ((th.TransportErrorIndicator & 0x1) << 7) | ((th.PayloadUnitStartIndicator & 0x1) << 6) | ((th.TransportPriority & 0x1) << 5) | (uint8((th.PID & 0x1f00) >> 8))
	s.TsPack[2] = uint8(th.PID & 0xff)
	s.TsPack[3] = ((th.TransportScramblingControl & 0x3) << 6) | ((th.AdaptationFieldControl & 0x3) << 4) | (th.ContinuityCounter & 0xf)

	//tsHeader和pat之间有1字节的分隔
	//tsHeader和pmt之间有1字节的分隔
	//tsHeader和pes之间无1字节的分隔
	s.TsPack[4] = 0x0 //pointer_field???

	dataLen := len(data)
	copy(s.TsPack[5:5+dataLen], data)
	return s.TsPack, dataLen
}

/*************************************************/
/* pes
/*************************************************/

// rtmp里面的数据是ES(h264/aac)裸流, tsFile里是PES
// rtmp的message转换为pes, 一个pes就是一帧数据(关键帧/非关键帧/音频帧)
func PesHeaderCreate(s *Stream, c Chunk) (*PesHeader, []byte) {
	//rtmp里的Timestamp是dts 是毫秒
	dts := uint64(c.Timestamp) * H264ClockFrequency
	pts := dts
	var CompositionTime uint32
	if c.MsgTypeId == MsgTypeIdVideo { // 9
		CompositionTime = ByteToUint32(c.MsgData[2:5], BE) // 24bit
		pts = dts + uint64(CompositionTime*H264ClockFrequency)
	}
	//s.log.Printf("c.DataType=%s, pts=%d, dts=%d, CompositionTime=%d", c.DataType, pts, dts, CompositionTime)

	var pes PesHeader
	pes.PacketStartCodePrefix = 0x000001
	switch c.MsgTypeId {
	case MsgTypeIdAudio: //8
		pes.StreamId = AudioStreamId
	case MsgTypeIdVideo: //9
		pes.StreamId = VideoStreamId
	}
	pes.PtsDtsFlags = 0x2 //只有PTS, 40bit
	pes.PesPacketLength = 3 + 5
	pes.PesHeaderDataLength = 5
	if pts != dts {
		pes.PtsDtsFlags = 0x3 // 有PTS 有DTS, 40bit + 40bit
		pes.PesPacketLength = 3 + 10
		pes.PesHeaderDataLength = 10
	}

	//var oPesHeader OptionalPesHeader
	if pes.PesHeaderDataLength != 0 {
		pes.FixedValue0 = 0x2
		pes.PesScramblingControl = 0x0
		pes.PesPriority = 0x0
		pes.DataAlignmentIndicator = 0x0
		pes.Copyright = 0x0
		pes.OriginalOrCopy = 0x0
		//pes.PtsDtsFlags = 0
		pes.EscrFlag = 0x0
		pes.EsRateFlag = 0x0
		pes.DsmTrickModeFlag = 0x0
		pes.AdditionalCopyInfoFlag = 0x0
		pes.PesCrcFlag = 0x0
		pes.PesExtensionFlag = 0x0
		//pes.PesHeaderDataLength
	}
	pes.Pts = pts
	pes.Dts = dts
	//pcr和dts是什么关系??? 为什么差值 63000/90=700ms
	//ffmpeg的pcr取值和dts的差是可配置，默认值pcr和dts相同
	//nginx-rtmp的pcr取值dts-63000, 减63000后PCR和PTS的差值较大,
	//EasyICE时间戳图表中 显示一条水平线 但此时值都是正确的
	pes.Pcr = dts
	//pes.Pcr = dts - 63000
	//s.log.Printf("pts:%d, dts:%d, pcr:%d", pes.Pts, pes.Dts, pes.Pcr)

	pesLen := 6 + pes.PesPacketLength
	pesData := make([]byte, pesLen)

	Uint24ToByte(pes.PacketStartCodePrefix, pesData[0:3], BE)
	pesData[3] = pes.StreamId
	//这里还不知道负载数据的长度, 所以先赋值为0
	//后续用 SetPesPakcetLength() 重新赋值
	pes.PesPacketLength = 0x0
	Uint16ToByte(pes.PesPacketLength, pesData[4:6], BE)
	pesData[6] = ((pes.FixedValue0 & 0x3) << 6) | ((pes.PesScramblingControl & 0x3) << 4) | ((pes.PesPriority & 0x1) << 3) | ((pes.DataAlignmentIndicator & 0x1) << 2) | ((pes.Copyright & 0x1) << 1) | (pes.OriginalOrCopy & 0x1)
	pesData[7] = ((pes.PtsDtsFlags & 0x3) << 6) | ((pes.EscrFlag & 0x1) << 5) | ((pes.EsRateFlag & 0x1) << 4) | ((pes.DsmTrickModeFlag & 0x1) << 3) | ((pes.AdditionalCopyInfoFlag & 0x1) << 2) | ((pes.PesCrcFlag & 0x1) << 1) | (pes.PesExtensionFlag & 0x1)
	pesData[8] = pes.PesHeaderDataLength

	pes.MarkerBit0 = 0x1
	pes.MarkerBit1 = 0x1
	pes.MarkerBit2 = 0x1
	if pes.PtsDtsFlags == 0x2 { // 只有PTS, 40bit
		pes.FixedValue1 = 0x2
		pesData[9] = ((pes.FixedValue1 & 0xf) << 4) | uint8((pes.Pts>>29)&0xe) | (pes.MarkerBit0 & 0x1)
		pesData[10] = uint8((pes.Pts >> 22) & 0xff)
		pesData[11] = uint8((pes.Pts>>14)&0xfe) | (pes.MarkerBit1 & 0x1)
		pesData[12] = uint8((pes.Pts >> 7) & 0xff)
		pesData[13] = uint8(((pes.Pts & 0x7F) << 1)) | (pes.MarkerBit2 & 0x1)
	}
	if pes.PtsDtsFlags == 0x3 { // 有PTS 有DTS, 40bit + 40bit
		pes.FixedValue1 = 0x3
		pesData[9] = ((pes.FixedValue1 & 0xf) << 4) | uint8((pes.Pts>>29)&0xe) | (pes.MarkerBit0 & 0x1)
		pesData[10] = uint8((pes.Pts >> 22) & 0xff)
		pesData[11] = uint8((pes.Pts>>14)&0xfe) | (pes.MarkerBit1 & 0x1)
		pesData[12] = uint8((pes.Pts >> 7) & 0xff)
		pesData[13] = uint8(((pes.Pts & 0x7F) << 1)) | (pes.MarkerBit2 & 0x1)
		pes.FixedValue1 = 0x1
		pesData[14] = ((pes.FixedValue1 & 0xf) << 4) | uint8((pes.Dts>>29)&0xe) | (pes.MarkerBit0 & 0x1)
		pesData[15] = uint8((pes.Dts >> 22) & 0xff)
		pesData[16] = uint8((pes.Dts>>14)&0xfe) | (pes.MarkerBit1 & 0x1)
		pesData[17] = uint8((pes.Dts >> 7) & 0xff)
		pesData[18] = uint8(((pes.Dts & 0x7F) << 1)) | (pes.MarkerBit2 & 0x1)
	}
	//s.log.Printf("%#v", pes)
	return &pes, pesData
}

func SetPesPakcetLength(d []byte, size int) {
	// 16bit, 最大值65536, 如果放不下可以为0
	if size > 0xffff {
		return
	}
	Uint16ToByte(uint16(size), d[4:6], BE)
}

/*
0x000001, 0x00000001 分别在什么情况下使用? 详见
T-REC-H.264-201602-I!!PDF-E.pdf, B.1.2, P331 P309
视频数据打es包时, 是按照 Annexb 格式封装的, 即在每一个 NALU 前都需要插入
0x00000001(4字节, 帧首) 或 0x000001(3字节, 帧中)
举例: the first ts message of apple sample:
annexb 4B header, 2B aud(nal_unit_type:6)(0x09 0xf0)(AUD)
annexb 3B header, 12B nalu(nal_unit_type:6)(SEI)
annexb 3B header, 19B sps(nal_unit_type:7)(SPS)
annexb 3B header, 4B pps(nal_unit_type:8)(PPS)
annexb 3B header, 2762B nalu(nal_unit_type:5)(IDR)
举例: the second ts message of apple ts sample:
annexb 4B header, 2B aud(nal_unit_type:6)(0x09 0xf0)(AUD)
annexb 3B header, 21B nalu(nal_unit_type:6)(SEI)
annexb 3B header, 379B nalu(nal_unit_type:1)(non-IDR,P/B)
annexb 3B header, 406B nalu(nal_unit_type:1)(non-IDR,P/B)
*/

// 打包es层数据时 pes头和es数据 之间要加入一个 aud(type=9)的nalu, 关键帧slice前必须要加入sps(type=7)和pps(type=8)的nalu, 而且是紧邻的, 非关键帧不需要sps和pps
// H264分隔符: 0x00000001 + 0x09 + 0xf0, 0x09后面的1字节可以随意, ffmpeg转出的ts有这6个字节 好像没有也可以
// H265分隔符: 0x00000001 + 0x46 + 0x01 + 0x50
//
//	关键帧: 0x00000001 + 0x67 + sps + 0x00000001 + 0x68 + pps + 0x00000001 + 0x65 + keyFrame(这里可能有多个nalu, 每个nalu前都要有起始码0x00000001)
//
// 非关键帧: 0x00000001 + 0x61 + interFrame(这里可能有多个nalu, 每个nalu前都要有起始码0x00000001)
func PesDataCreateVideoFrame(s *Stream, c Chunk, phd []byte) []byte {
	pesHeaderDataLen := uint32(len(phd))

	var SpsPpsDataLen uint32
	SpsPpsDataLen = 0
	if c.DataType == "VideoKeyFrame" {
		SpsPpsDataLen = uint32(len(s.SpsPpsData))
	}

	MsgDataLen := c.MsgLength - 5
	dataLen := pesHeaderDataLen + SpsPpsDataLen + 6 + MsgDataLen
	if s.VideoCodecType == "H265" {
		dataLen += 1
	}

	data := make([]byte, dataLen)

	var ss uint32
	var ee uint32
	ss = 0
	ee = pesHeaderDataLen
	if ee > dataLen {
		return nil
	}
	copy(data[ss:ee], phd)

	ss = ee
	ee += 4
	if ee > dataLen {
		return nil
	}
	Uint32ToByte(0x00000001, data[ss:ee], BE)

	ss = ee
	switch s.VideoCodecType {
	case "H264":
		ee += 2
		if ee > dataLen {
			return nil
		}
		Uint16ToByte(0x09f0, data[ss:ee], BE)
	case "H265":
		ee += 3
		if ee > dataLen {
			return nil
		}
		Uint24ToByte(0x460150, data[ss:ee], BE)
	}

	if c.DataType == "VideoKeyFrame" {
		ss = ee
		ee += SpsPpsDataLen
		if ee > dataLen {
			return nil
		}
		copy(data[ss:ee], s.SpsPpsData)
	}

	//这里可能有多个nalu, 每个nalu前都要有起始码0x00000001
	//如果是nalu都是数据, 应该第一个nalu起始码用0x00000001
	//后续nalu起始码用0x000001, 不过都用4字节01 播放器和解码器也能处理
	//详见 GetNaluNum() 说明
	//不能改变原始数据, 所以要分段拷贝
	var s0 uint32
	var e0 uint32
	s0 = 5
	e0 = s0 + 4
	var i, naluLen uint32
	for i = 0; i < c.NaluNum; i++ {
		if e0 > c.MsgLength {
			return nil
		}
		naluLen = ByteToUint32(c.MsgData[s0:e0], BE)

		ss = ee
		ee += 4
		if ee > dataLen {
			return nil
		}
		Uint32ToByte(0x00000001, data[ss:ee], BE)

		ss = ee
		ee += naluLen
		s0 = e0
		e0 += naluLen
		//s.log.Printf("pes %v, %v, %v, %v, %v, %v, %v, %v, %v", i, c.NaluNum, dataLen, c.MsgLength, naluLen, ss, ee, s0, e0)
		if ee > dataLen || e0 > c.MsgLength {
			return nil
		}
		copy(data[ss:ee], c.MsgData[s0:e0])

		s0 = e0
		e0 += 4
	}

	if strings.Contains(s.Key, conf.Debug.StreamId) {
		s.log.Printf("%s write in dType=%s, NaluNum=%d, dLen=%d", path.Base(s.TsPath), c.DataType, c.NaluNum, len(data))
	}
	return data
}

func PesDataCreateAacFrame(s *Stream, c Chunk, phd []byte) []byte {
	pesHeaderDataLen := uint32(len(phd))

	var MsgDataLen uint32 = 0
	if c.MsgLength > 2 {
		MsgDataLen = c.MsgLength - 2
	}
	dataLen := pesHeaderDataLen + 7 + MsgDataLen

	//ParseAdtsData(s)
	if len(s.AdtsData) < 7 {
		s.log.Printf("hls adts header len %d less then 7", len(s.AdtsData))
		return nil
	}
	SetAdtsLength(s.AdtsData, uint16(7+MsgDataLen))

	data := make([]byte, dataLen)

	var ss uint32
	var ee uint32
	ss = 0
	ee = pesHeaderDataLen
	copy(data[ss:ee], phd)

	ss = ee
	ee += 7
	copy(data[ss:ee], s.AdtsData)

	ss = ee
	if MsgDataLen > 0 && len(c.MsgData) > 2 {
		ee += MsgDataLen
		copy(data[ss:], c.MsgData[2:])
	}
	return data
}

/*************************************************/
/* pat
/*************************************************/
type PatProgram struct {
	ProgramNumber uint16 // 16bit, arr 4byte,  0 is NetworkPid
	Reserved2     uint8  // 3bit, arr
	PID           uint16 // 13bit, arr, NetworkPid or ProgramMapPid
}

// 1+2+2+3+4+4=16byte
type Pat struct {
	TableId                uint8  // 8bit, 固定值0x00, 表示是PAT
	SectionSyntaxIndicator uint8  // 1bit, 固定值0x1
	Zero                   uint8  // 1bit, 0x0
	Reserved0              uint8  // 2bit, 0x3
	SectionLength          uint16 // 12bit, 表示后面还有多少字节 包括CRC32
	TransportStreamId      uint16 // 16bit, 传输流id, 区别与其他路流id
	Reserved1              uint8  // 2bit, 保留位
	VersionNumber          uint8  // 5bit, 范围0-31，表示PAT的版本号
	CurrentNextIndicator   uint8  // 1bit, 是当前有效还是下一个有效
	SectionNumber          uint8  // 8bit, PAT可能分为多段传输，第一段为00，以后每个分段加1，最多可能有256个分段
	LastSectionNumber      uint8  // 8bit, 最后一个分段的号码
	ProgramNumber          uint16 // 16bit, arr 4byte,  0 is NetworkPid
	Reserved2              uint8  // 3bit, arr
	PID                    uint16 // 13bit, arr, NetworkPid or ProgramMapPid
	CRC32                  uint32 // 32bit
}

func PatCreate() (*Pat, []byte) {
	var pat Pat
	pat.TableId = 0x00
	pat.SectionSyntaxIndicator = 0x1
	pat.Zero = 0x0
	pat.Reserved0 = 0x3
	pat.SectionLength = 0xd //16-3=13
	pat.TransportStreamId = 0x1
	pat.Reserved1 = 0x3
	pat.VersionNumber = 0x0
	pat.CurrentNextIndicator = 0x1
	pat.SectionNumber = 0x0
	pat.LastSectionNumber = 0x0
	pat.ProgramNumber = 0x1
	pat.Reserved2 = 0x7
	pat.PID = PmtPid
	pat.CRC32 = 0

	patData := make([]byte, 16)
	patData[0] = pat.TableId
	patData[1] = ((pat.SectionSyntaxIndicator & 0x1) << 7) | ((pat.Zero & 0x1) << 6) | ((pat.Reserved0 & 0x3) << 4) | uint8(((pat.SectionLength & 0xf00) >> 8))
	patData[2] = uint8(pat.SectionLength & 0xff)
	Uint16ToByte(pat.TransportStreamId, patData[3:5], BE)
	patData[5] = ((pat.Reserved1 & 0x3) << 6) | ((pat.VersionNumber & 0x1f) << 1) | (pat.CurrentNextIndicator & 0x1)
	patData[6] = pat.SectionNumber
	patData[7] = pat.LastSectionNumber
	Uint16ToByte(pat.ProgramNumber, patData[8:10], BE)
	patData[10] = ((pat.Reserved2 & 0x7) << 5) | uint8(((pat.PID & 0x1f00) >> 8))
	patData[11] = uint8(pat.PID & 0xff)

	pat.CRC32 = Crc32Create(patData[:12])
	Uint32ToByte(pat.CRC32, patData[12:16], BE)
	return &pat, patData
}

/*************************************************/
/* pmt
/*************************************************/
//ISO/IEC 13818-7 Audio with ADTS transport syntax
//AVC video stream as defined in ITU-T Rec. H.264 | ISO/IEC 14496-10 Video
// StreamType             uint8  // 8bit, arr 5byte
// 0x03     MP3
// 0x0f		AAC, Audio with ADTS transport syntax
// 0x0f		AAC, 具有ADTS 传输句法的ISO/IEC 13818-7 音频
// 0x1b		ITU-T H.264 或 ISO/IEC 14496-10 中定义的AVC视频流
// 0x24		ITU-T H.265 或 ISO/IEC_23008-2  中定义的HEVC视频流
// 40bit = 5byte
type PmtStream struct {
	StreamType    uint8  // 8bit, 节目数据类型
	Reserved4     uint8  // 3bit,
	ElementaryPID uint16 // 13bit, 节目数据类型对应的pid
	Reserved5     uint8  // 4bit,
	EsInfoLength  uint16 // 12bit, 私有数据长度
}

// 1+2+2+3+2+2+4+(5*2)=26byte
type Pmt struct {
	TableId                uint8       // 8bit, 固定值0x02, 表示是PMT
	SectionSyntaxIndicator uint8       // 1bit, 固定值0x1
	Zero                   uint8       // 1bit, 固定值0x0
	Reserved0              uint8       // 2bit, 0x3
	SectionLength          uint16      // 12bit, 表示后面还有多少字节 包括CRC32
	ProgramNumber          uint16      // 16bit, 不同节目此值不同 依次递增
	Reserved1              uint8       // 2bit, 0x3
	VersionNumber          uint8       // 5bit, 指示当前TS流中program_map_secton 的版本号
	CurrentNextIndicator   uint8       // 1bit, 当该字段为1时表示当前传送的program_map_section可用，当该字段为0时，表示当前传送的program_map_section不可用，下一个TS的program_map_section有效。
	SectionNumber          uint8       // 8bit, 0x0
	LastSectionNumber      uint8       // 8bit, 0x0
	Reserved2              uint8       // 3bit, 0x7
	PcrPID                 uint16      // 13bit, pcr会在哪个pid包里出现，一般是视频包里，PcrPID设置为 0x1fff 表示没有pcr
	Reserved3              uint8       // 4bit, 0xf
	ProgramInfoLength      uint16      // 12bit, 节目信息描述的字节数, 通常为 0x0
	PmtStream              []PmtStream // 40bit, 节目信息
	CRC32                  uint32      // 32bit
}

func PmtCreate(s *Stream) (*Pmt, []byte) {
	var pmt Pmt
	pmt.TableId = 0x2
	pmt.SectionSyntaxIndicator = 0x1
	pmt.Zero = 0x0
	pmt.Reserved0 = 0x3
	pmt.SectionLength = 0x17 //26-3=23
	pmt.ProgramNumber = 0x1
	pmt.Reserved1 = 0x3
	pmt.VersionNumber = 0x0
	pmt.CurrentNextIndicator = 0x1
	pmt.SectionNumber = 0x0
	pmt.LastSectionNumber = 0x0
	pmt.Reserved2 = 0x7
	pmt.PcrPID = VideoPid
	pmt.Reserved3 = 0xf
	pmt.ProgramInfoLength = 0x0

	//首个ts没有PMTaudio, 直接把PMTaudo写死, 可能没有音频(测试播放正常)
	//备选方案: 生产首个ts时 sleep1秒 等待音视频头信息

	//由于第1次进来的太快, 可能只有 音频或视频
	//第2次进来的的时候, 肯定是正确的
	//s.log.Println(s.AudioCodecType, s.VideoCodecType)
	/*
		if s.AudioCodecType != "" && s.VideoCodecType == "" { //只有音频
			pmt.SectionLength -= 5
			pmt.PmtStream = make([]PmtStream, 1)
			pmt.PmtStream[0].StreamType = 0xf
			pmt.PmtStream[0].Reserved4 = 0x7
			pmt.PmtStream[0].ElementaryPID = AudioPid
			pmt.PmtStream[0].Reserved5 = 0xf
			pmt.PmtStream[0].EsInfoLength = 0x0
		}

		if s.AudioCodecType == "" && s.VideoCodecType != "" { //只有视频
			pmt.SectionLength -= 5
			pmt.PmtStream = make([]PmtStream, 1)
			pmt.PmtStream[0].StreamType = 0x1b
			if s.VideoCodecType == "H265" {
				pmt.PmtStream[0].StreamType = 0x24
			}
			pmt.PmtStream[0].Reserved4 = 0x7
			pmt.PmtStream[0].ElementaryPID = VideoPid
			pmt.PmtStream[0].Reserved5 = 0xf
			pmt.PmtStream[0].EsInfoLength = 0x0
		}
	*/

	//if s.AudioCodecType != "" && s.VideoCodecType != "" { //音视频都有
	pmt.PmtStream = make([]PmtStream, 2)
	pmt.PmtStream[0].StreamType = 0xf
	pmt.PmtStream[0].Reserved4 = 0x7
	pmt.PmtStream[0].ElementaryPID = AudioPid
	pmt.PmtStream[0].Reserved5 = 0xf
	pmt.PmtStream[0].EsInfoLength = 0x0
	pmt.PmtStream[1].StreamType = 0x1b
	if s.VideoCodecType == "H265" {
		pmt.PmtStream[1].StreamType = 0x24
	}
	pmt.PmtStream[1].Reserved4 = 0x7
	pmt.PmtStream[1].ElementaryPID = VideoPid
	pmt.PmtStream[1].Reserved5 = 0xf
	pmt.PmtStream[1].EsInfoLength = 0x0
	//}
	pmt.CRC32 = 0

	useLen := 0
	pmtData := make([]byte, 26)

	pmtData[0] = pmt.TableId
	pmtData[1] = ((pmt.SectionSyntaxIndicator & 0x1) << 7) | ((pmt.Zero & 0x1) << 6) | ((pmt.Reserved0 & 0x3) << 4) | uint8(((pmt.SectionLength & 0xf00) >> 8))
	pmtData[2] = uint8(pmt.SectionLength & 0xff)
	Uint16ToByte(pmt.ProgramNumber, pmtData[3:5], BE)
	pmtData[5] = ((pmt.Reserved1 & 0x3) << 6) | ((pmt.VersionNumber & 0x1f) << 1) | (pmt.CurrentNextIndicator & 0x1)
	pmtData[6] = pmt.SectionNumber
	pmtData[7] = pmt.LastSectionNumber
	pmtData[8] = ((pmt.Reserved2 & 0x7) << 5) | uint8(((pmt.PcrPID & 0x1f00) >> 8))
	pmtData[9] = uint8(pmt.PcrPID & 0xff)
	pmtData[10] = ((pmt.Reserved3 & 0xf) << 4) | uint8(((pmt.ProgramInfoLength & 0xf00) >> 8))
	pmtData[11] = uint8(pmt.ProgramInfoLength & 0xff)
	useLen += 12

	/*
		if s.AudioCodecType != "" && s.VideoCodecType == "" { //只有音频
			ps0 := pmt.PmtStream[0]
			pmtData[12] = ps0.StreamType
			pmtData[13] = ((ps0.Reserved4 & 0x7) << 5) | uint8(((ps0.ElementaryPID & 0x1f00) >> 8))
			pmtData[14] = uint8(ps0.ElementaryPID & 0xff)
			pmtData[15] = ((ps0.Reserved5 | 0xf) << 4) | uint8(((ps0.EsInfoLength & 0xf00) >> 8))
			pmtData[16] = uint8(ps0.EsInfoLength & 0xff)
			useLen += 5
		}

		if s.AudioCodecType == "" && s.VideoCodecType != "" { //只有视频
			ps1 := pmt.PmtStream[0]
			pmtData[12] = ps1.StreamType
			pmtData[13] = ((ps1.Reserved4 & 0x7) << 5) | uint8(((ps1.ElementaryPID & 0x1f00) >> 8))
			pmtData[14] = uint8(ps1.ElementaryPID & 0xff)
			pmtData[15] = ((ps1.Reserved5 | 0xf) << 4) | uint8(((ps1.EsInfoLength & 0xf00) >> 8))
			pmtData[16] = uint8(ps1.EsInfoLength & 0xff)
			useLen += 5
		}
	*/

	//if s.AudioCodecType != "" && s.VideoCodecType != "" { //音视频都有
	ps0 := pmt.PmtStream[0]
	ps1 := pmt.PmtStream[1]
	pmtData[12] = ps0.StreamType
	pmtData[13] = ((ps0.Reserved4 & 0x7) << 5) | uint8(((ps0.ElementaryPID & 0x1f00) >> 8))
	pmtData[14] = uint8(ps0.ElementaryPID & 0xff)
	pmtData[15] = ((ps0.Reserved5 | 0xf) << 4) | uint8(((ps0.EsInfoLength & 0xf00) >> 8))
	pmtData[16] = uint8(ps0.EsInfoLength & 0xff)

	pmtData[17] = ps1.StreamType
	pmtData[18] = ((ps1.Reserved4 & 0x7) << 5) | uint8(((ps1.ElementaryPID & 0x1f00) >> 8))
	pmtData[19] = uint8(ps1.ElementaryPID & 0xff)
	pmtData[20] = ((ps1.Reserved5 | 0xf) << 4) | uint8(((ps1.EsInfoLength & 0xf00) >> 8))
	pmtData[21] = uint8(ps1.EsInfoLength & 0xff)
	useLen += 10
	//}

	pmt.CRC32 = Crc32Create(pmtData[:useLen])
	Uint32ToByte(pmt.CRC32, pmtData[useLen:useLen+4], BE)
	return &pmt, pmtData
}

/*************************************************/
/* tsFile
/*************************************************/
func TsFileCreate(s *Stream, c Chunk) {
	if s.TsPath != "" {
		s.TsFileBuf.Flush()
		s.TsFile.Close()
		M3u8Update(s)
	}

	//GSP3bnx69BgxI-avEc0oE4C4_44.ts
	//GSP63nBbfmlbW-fnMebne7hU_20220222164158_29306.ts
	s.TsPath = fmt.Sprintf("%s/%s/%s_%s_%d.ts", s.HlsStorePath, s.AmfInfo.StreamId, s.AmfInfo.StreamId, utils.GetYMDHMS(), s.TsLastSeq)

	var err error
	s.TsFile, err = os.OpenFile(s.TsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		s.log.Println(err)
		//s.TsPath = ""
		return
	}
	//s.log.Printf("create %s", s.TsPath)

	//使用bufio要注意
	//s.TsFile.Close()时不会写入硬盘, 关闭前 必须调 s.TsFileBuf.Flush()
	//想写入硬盘 要么写入数据大于 bufSize, 要么手动调用 Flush()
	//bufSize=10, 写入数据为10个时, 不会写入硬盘
	//bufSize=10, 写入数据为11个时, 会写入硬盘
	//1MB = 1024 * 1024 * 1 = 1048576
	//2MB = 1024 * 1024 * 2 = 2097152
	//bufSize小, 内存使用少写的次数多; bufSize大, 内存使用多写的次数少;
	//bufSize的大小, 需要实际测试 找到合适值
	//1个ts文件大概在1MB到4MB之间, 实际测试bufSize=1MB比较合适
	//XXX: 最好能复用buf, 复用buf时 要把尾部数据长度处理好
	s.TsFileBuf = bufio.NewWriterSize(s.TsFile, 1048576)

	/*
		_, patData := PatCreate()
		_, _ = TsPacketCreatePatPmt(s, PatPid, patData)
		s.log.Printf("TsPackPatData:%x", s.TsPack)
		_, err = s.TsFileBuf.Write(s.TsPack)
	*/
	_, err = s.TsFileBuf.Write(TsPackPat[:])
	if err != nil {
		s.log.Println(err)
		s.TsPath = ""
		return
	}

	//XXX: 推荐使用 预定义值 TsPackPmtxxx
	_, pmtData := PmtCreate(s)
	_, _ = TsPacketCreatePatPmt(s, PmtPid, pmtData)
	//s.log.Printf("TsPackPmtData:%x", s.TsPack)
	_, err = s.TsFileBuf.Write(s.TsPack)
	if err != nil {
		s.log.Println(err)
		s.TsPath = ""
		return
	}

	TsFileAppend(s, c)
	if s.AudioChunk.DataType == "AudioAacFrame" {
		//s.log.Printf("create ts about add audio chunk after video")
		TsFileAppend(s, s.AudioChunk)
		s.AudioChunk.DataType = ""
	}
	s.TsFirstTs = c.Timestamp
	s.TsLastSeq++
}

func TsFileAppend(s *Stream, c Chunk) {
	pesHeader, pesHeaderData := PesHeaderCreate(s, c)

	var pesData []byte
	switch c.DataType {
	case "VideoKeyFrame":
		//pesData = PesDataCreateKeyFrame(s, c, pesHeaderData)
		pesData = PesDataCreateVideoFrame(s, c, pesHeaderData)
	case "VideoInterFrame":
		//pesData = PesDataCreateInterFrame(s, c, pesHeaderData)
		pesData = PesDataCreateVideoFrame(s, c, pesHeaderData)
	case "AudioAacFrame":
		pesData = PesDataCreateAacFrame(s, c, pesHeaderData)
	}

	pesDataLen := len(pesData)
	if pesDataLen < 6 {
		s.log.Printf("pes data not enough %v, type:%s", pesDataLen, c.DataType)
		return
	}
	SetPesPakcetLength(pesData, pesDataLen-6)

	first := true
	var sp, useLen int
	var err error
	for {
		s.TsPack, useLen = TsPacketCreate(s, c, pesData[sp:], pesHeader.Pcr, first)
		sp += useLen

		_, err = s.TsFileBuf.Write(s.TsPack)
		if err != nil {
			s.log.Println(err)
			return
		}

		if sp >= pesDataLen {
			//s.log.Printf("sp=%d, pesDataLen=%d", sp, pesDataLen)
			break
		}
		first = false
	}
}

//某些流时间戳有问题 日志打印太多, 好处是便于发现问题
//为了防止日志太大, publish*.log保存15天改为保存7天
//morethan: c.Ts(25632288) - FirstTs(25616688) = dv(15600)
//rollback: c.Ts(2268)     - FirstTs(95437044) = dv(8941)
//jumpback: c.Ts(80448821) - FirstTs(80449260) = dv(439)

// gateway 会把ps流中的时间戳 除以90后 单位为毫秒
// gateway ps流中   时间戳最大值有2个 0xffffffff 或 0x1ffffffff
// gateway rtmp流中 时间戳最大值有2个  0x2D82D82 或   0x5B05B05
//
//	47721858		  95443717
//
// ffmpeg/obs 推rtmp流的时间戳 单位为毫秒 最大值为 0xffffffff
// 24bit, 最大值  0xffffff=  16777215单位毫秒 约为 4.7小时 约为0.20天
// 32bit, 最大值0xffffffff=4294967295单位毫秒 约为1203小时 约为49.7天
// 新生成一个ts返回true, 否则返回false
func TsCreate(s *Stream, c Chunk) bool {
	var dv uint32
	if c.Timestamp >= s.TsFirstTs {
		//时间戳递增: c.Timestamp(46417939) - s.TsFirstTs(46286804) = dv(131135)
		//rtmp推流上来后 第一帧数据 会造成 s.TsExtInfo 值很大
		//如果后续不来帧, 那生成的 xxx_0.ts 时间会很大
		if s.TsPath == "" && s.TsFirstTs == 0 {
			dv = 0
		} else {
			dv = c.Timestamp - s.TsFirstTs
		}

		//视频帧率60fps, 帧间隔1000/60=16.7ms
		//视频帧率25fps, 帧间隔1000/25=  40ms
		//视频帧率20fps, 帧间隔1000/20=  50ms
		//视频帧率 2fps, 帧间隔1000/ 5= 500ms
		//视频帧率 1fps, 帧间隔1000/ 5=1000ms
		//音画相差400ms, 人类就能明显感觉到不同步
		//帧间隔跳跃是否太大的打印, 查看代码 bigjump 关键字
	} else {
		//时间戳回绕: c.Timestamp(8) - s.TsFirstTs(47720666) = dv(4247246637)
		//时间戳回跳: c.Timestamp(28362000) - s.TsFirstTs(28362676) = dv(47721182)
		//47720666 - 8        = 47720658 > 23860929
		//28362676 - 28362000 =      676 < 23860929
		dv = s.TsFirstTs - c.Timestamp
		//23860929 = 0x16C16C1 = 0x2D82D82/2
		if dv >= 23860929 { //回绕
			//8 + (47721858 - 47720666) = 1200
			dv = c.Timestamp + (0x2D82D82 - s.TsFirstTs)
			//8 + (95443717 - 47720666) = 47723059
			dv1 := c.Timestamp + (0x5B05B05 - s.TsFirstTs)
			//8 + (4294967295 - 47720666) = 4247246629
			dv2 := c.Timestamp + (0xFFFFFFFF - s.TsFirstTs)
			if dv > dv1 {
				dv = dv1
			}
			if dv > dv2 {
				dv = dv2
			}
			//s.log.Printf("dv=%d, dv1=%d, dv2=%d", dv, dv1, dv2)
			s.log.Printf("rollback: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsFirstTs, dv, c.DataType)
		} else { //回跳
			//s.TsFirstTs是视频关键帧的时间戳, 音频时间戳偶尔小幅度回跳是正常的
			//回跳时间差 FirstTs - c.Ts <= conf.Hls.TsMaxTime, 应该把数据写入ts
			//回跳时间差 FirstTs - c.Ts >  conf.Hls.TsMaxTime, 应该截断ts
			//28362676 - 28362000 = 676 <= 10000
			//dv = s.TsFirstTs - c.Timestamp
			s.log.Printf("jumpback: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsFirstTs, dv, c.DataType)
		}
	}
	//when delta is larger than 20 seconds, it will print
	/*if dv > 20*1000 && s.TsFirstTs != 0 {
		s.log.Printf("morethan: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsFirstTs, dv, c.DataType)
	}
	*/
	//这是最后的保障, 为了纠正上面对时间戳计算可能发生的错误 或 其他意外情况
	//1小时=3600秒=3600000毫秒
	if s.TsPath != "" && dv >= 3600000 {
		s.log.Printf("dv >= 3600000ms, force dv = %fms", conf.HlsRec.TsMaxTime*1000)
		dv = uint32(conf.HlsRec.TsMaxTime * 1000)
	}

	//rtmp里的timestamp单位是毫秒, 除以1000变为秒
	s.TsExtInfo = float64(dv) / 1000
	//TODO: when all of the video timestamp is equal to zero, force set ts duration.
	//when ts size is equal to 10MB, suppose bitrate is 3Mb, the duration is about 30s
	if dv == 0 {
		if s.TsMaxCutSize >= conf.HlsRec.TsMaxSize && c.DataType == "VideoKeyFrame" {
			s.TsExtInfo = 30
		} else if s.TsMaxCutSize >= 2*conf.HlsRec.TsMaxSize {
			s.TsExtInfo = 60
		}
	}

	s.TsMaxCutSize += uint32(len(c.MsgData))
	var ok bool
	//TODO: gateway的rtmp流里可能没有关键帧, 要防止产生大ts, 比如 30min时间限制 或 20MB文件限制
	//摄像头关键帧间隔一般为固定值50, 经过观察 发现线上有些流 关键帧间隔不固定 最长20分钟左右才有关键帧
	//TS文件大于10MB且是关键帧强制切割或者TS文件大于20MB强制切割
	//纯音频支持切片
	_, ok = s.GopCache.VideoHeader.Load(s.Key)
	//s.log.Printf("test andrew res:%v, key:%s, type:%s, %v", ok, s.Key, c.DataType, s.TsExtInfo)
	if s.TsPath == "" || (s.TsExtInfo >= float64(conf.HlsRec.TsMaxTime) && c.DataType == "VideoKeyFrame") || s.TsExtInfo >= float64(3*conf.HlsRec.TsMaxTime) ||
		(s.TsMaxCutSize >= conf.HlsRec.TsMaxSize && c.DataType == "VideoKeyFrame") || s.TsMaxCutSize >= 2*conf.HlsRec.TsMaxSize ||
		(ok == false && s.TsExtInfo >= float64(conf.HlsRec.TsMaxTime) && c.DataType == "AudioAacFrame") {
		//s.log.Printf("create ts previous data type:%s, cur data type: %s", s.AudioChunk.DataType, c.DataType)
		TsFileCreate(s, c)
		ok = true
		s.TsMaxCutSize = 0
	} else {
		//s.log.Printf("append ts previous data type:%s, cur data type: %s", s.AudioChunk.DataType, c.DataType)
		if c.DataType == "AudioAacFrame" {
			if s.AudioChunk.DataType == "AudioAacFrame" {
				TsFileAppend(s, s.AudioChunk)
			}
			s.AudioChunk = c
		} else {
			TsFileAppend(s, c)
		}
		ok = false
	}

	return ok
}

/*************************************************/
/* HlsCreator()
/*************************************************/
func HlsCreator(s *Stream) {
	defer s.Wg.Done()
	var err error
	dir := fmt.Sprintf("%s/%s", s.HlsStorePath, s.AmfInfo.StreamId)
	//data/hls/streamid目录及其下文件的删除说明
	//1 正常情况下, 不录制流 /data/hls/streamid/下的ts, Server来删
	//2 正常情况下, 录制流 /data/hls/streamid/下的ts, 谁录制谁来删
	//3 正常情况下, sLiveDelete周期检查并删除/data/hls/streamid/下3天前m3u8和ts
	//4 正常情况下, sLiveDelete周期检查并删除/data/hls/streamid空目录
	//5 上传失败时, sLiveDelete高水位触发删除/data/hls/streamid/下的ts
	//重新推流时, 不能删除/data/hls/streamid目录, s3上传失败的文件在这里
	/*
		err := os.RemoveAll(dir)
		if err != nil {
			log.Println(err)
			return
		}
		s.log.Printf("rm %s", dir)
	*/
	if utils.FileExist(dir) == false {
		err = os.Mkdir(dir, 0755)
		if err != nil {
			s.log.Println(err)
			return
		}
	}

	s.M3u8Path = fmt.Sprintf("%s/%s.m3u8", dir, s.AmfInfo.StreamId)
	s.log.Println(s.M3u8Path)
	//发生错误, 记得要关闭 s.M3u8File.Close()
	s.M3u8File, err = os.OpenFile(s.M3u8Path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		s.log.Println(err)
		return
	}

	s.TsList = list.New()
	s.TsPack = make([]byte, 188)

	var c Chunk
	var ok bool
	var i uint32
	for {
		select {
		case c, ok = <-s.HlsChan:
			if ok == false {
				s.log.Printf("%s HlsCreator stop", s.Key)
				return
			}
		case <-s.Ctx.Done():
			s.log.Printf("publish stop then hls stop")
			return
		}
		if strings.Contains(s.Key, conf.Debug.StreamId) {
			s.log.Printf("HlsMsgIdx=%d, fmt=%d, csid=%d, ts=%d, MsgLen=%d, MsgTypeId=%d, DataType=%s, NaluNum=%d", i, c.Fmt, c.Csid, c.Timestamp, c.MsgLength, c.MsgTypeId, c.DataType, c.NaluNum)
		}
		i++

		switch c.DataType {
		case "Metadata":
			continue // 此数据不该直接写入ts
		case "VideoHeader":
			switch s.VideoCodecType {
			case "H264":
				PrepareSpsPpsData(s, &c)
			case "H265":
				PrepareSpsPpsDataH265(s, &c)
			default:
			}
			s.HlsAddDiscFlag = true
			continue // 此数据不该直接写入ts
		case "AudioHeader":
			PrepareAdtsData(s, &c)
			//ParseAdtsData(s)
			s.HlsAddDiscFlag = true
			continue // 此数据不该直接写入ts
		default:
			//这里是 音视频数据 和 未定义类型数据
		}

		TsCreate(s, c)
	}
}

//////////////////////////////////////////////////
// hlslive
//////////////////////////////////////////////////
func HlsLiveClear(dir string, s *Stream) {
	if utils.FileExist(s.M3u8LivePath) == true {
		modTime, err := utils.GetFileModTime(s.M3u8LivePath)
		if err != nil {
			s.log.Println("hlslive get m3u8 file mode time error ", s.M3u8LivePath, err)
			return
		}

		curTime := time.Now().Unix()
		if uint32(curTime-modTime) > conf.DelayDeleteThred {
			s.log.Println("hlslive delete m3u8 ", s.M3u8LivePath)
			err = os.Remove(s.M3u8LivePath)
			if err != nil {
				s.log.Println("hlslive delete m3u8 error ", s.M3u8LivePath, err)
			}

			files, err := utils.GetAllFile(dir)
			if err != nil {
				s.log.Println("hlslive get m3u8 file error ", s.M3u8LivePath, err)
				return
			}
			for i := 0; i < len(files); i++ {
				err = os.Remove(files[i].Name)
				if err != nil {
					s.log.Println("hlslive delete ts error ", files[i].Name, err)
				}
			}
		} else {
			var TsFilepath string
			var tiStr string
			var tsName string
			var v uint64

			r := bufio.NewReader(s.M3u8LiveFile)

			for {
				line, err := r.ReadString('\n')
				line = strings.TrimSpace(line)
				if err != nil && err != io.EOF {
					s.log.Println(err)
					break
				}
				if err == io.EOF {
					s.log.Println(err)
					break
				}
				s.log.Println("hlslive ", line)

				if strings.Contains(line, "#EXT-X-MEDIA-SEQUENCE") == true {
					//#EXT-X-MEDIA-SEQUENCE:2
					ss := strings.Split(line, ":")
					if len(ss) < 2 {
						s.log.Println("hlslive EXT-X-MEDIA-SEQUENCE no standard:", line)
						continue
					}
					v, err = strconv.ParseUint(ss[1], 10, 32)
					if err != nil {
						s.log.Println("hlslive parse EXT-X-MEDIA-SEQUENCE error ", err)
						continue
					}
					s.TsLiveFirstSeq = uint32(v)
				} else if strings.Contains(line, "EXTINF") == true {
					//#EXTINF:2.00,avc
					ss := strings.Split(line, ":")
					if len(ss) < 2 {
						s.log.Println("hlslive EXTINF no standard:", line)
						continue
					}
					sss := strings.Split(ss[1], ",")
					//s.log.Println(len(sss), sss[0], sss[1])
					if len(sss) < 2 {
						s.log.Println("hlslive EXTINF value no standard:", ss[1])
						continue
					}
					s.TsLiveExtInfo, err = strconv.ParseFloat(sss[0], 64)
					if err != nil {
						s.log.Println("hlslive parse EXTINF error ", err)
						continue
					}
					tiStr = line
					//s.log.Println(tiStr)
				} else if strings.Contains(line, ".ts") == true {
					//test111_20230413111736_2.ts?mediaServerIp=172.20.47.84&codeType=H264
					ss := strings.Split(line, "?")
					if len(ss) > 1 {
						tsName = ss[0]
					} else {
						tsName = line
					}
					tis := fmt.Sprintf("%s\n%s\n", tiStr, line)
					TsFilepath = fmt.Sprintf("%s/%s/%s", conf.HlsLive.MemPath, s.AmfInfo.StreamId, tsName)

					ti := TsInfo{tis, s.TsLiveExtInfo, "", 0}
					ti.TsFilepath = TsFilepath
					//s.log.Println(ti)
					s.TsLiveList.PushBack(ti)
					s.TsLiveNum++

					ss = strings.Split(line, ".")
					sss := strings.Split(ss[0], "_")
					if len(sss) < 3 {
						s.log.Println("hlslive ts name no standard:", ss[0])
						continue
					}
					//s.log.Println(len(sss), sss[0], sss[1], sss[2])
					v, err = strconv.ParseUint(sss[2], 10, 32)
					if err != nil {
						s.log.Println("hlslive parse ts number error ", err)
						continue
					}
					s.TsLiveLastSeq = uint32(v)
					//s.log.Println("ts seq:", s.TsLiveLastSeq)
					if s.TsLiveNum > uint32(conf.HlsLive.M3u8TsNum) {
						e := s.TsLiveList.Front()
						ti := (e.Value).(TsInfo)
						s.TsLiveRemainName = ti.TsFilepath
						s.log.Printf("live %d, out ts %s, %s", s.TsLiveFirstSeq, s.TsLiveRemainName, ti.TsFilepath)
						s.TsLiveList.Remove(e)
						s.TsLiveNum--
						s.TsLiveFirstSeq++
					}
				}
			}

			//delete ts not in list
			files, err := utils.GetAllFile(dir)
			if err != nil {
				s.log.Println("hlslive get ts file error ", s.M3u8LivePath, err)
				return
			}
			var existFlag bool
			for i := 0; i < len(files); i++ {
				existFlag = false
				var n *list.Element
				for e := s.TsLiveList.Front(); e != nil; e = n {
					ti := (e.Value).(TsInfo)
					//s.log.Printf("live ts: %s", ti.TsFilepath)
					n = e.Next()
					if strings.Contains(ti.TsFilepath, files[i].Name) == true {
						existFlag = true
						break
					}
				}

				if existFlag == false {
					if strings.Contains(files[i].Name, ".m3u8") == true {
						continue
					}
					err = os.Remove(files[i].Name)
					if err != nil {
						s.log.Println("hlslive delete ts error ", files[i].Name, err)
					}
					s.log.Printf("hlslive delete ts out list: %s", files[i].Name)
				}
			}

			if s.TsLiveList.Len() > 0 {
				s.TsLiveLastSeq++
				var tsMaxTime float64
				var tis string
				for e := s.TsLiveList.Front(); e != nil; e = e.Next() {
					ti := (e.Value).(TsInfo)
					if tsMaxTime < ti.TsExtInfo {
						tsMaxTime = ti.TsExtInfo
					}
					tis = fmt.Sprintf("%s%s", tis, ti.TsInfoStr)
				}

				s.M3u8LiveData = fmt.Sprintf(m3u8Head, uint32(math.Ceil(tsMaxTime)), s.TsLiveFirstSeq)
				s.M3u8LiveData = fmt.Sprintf("%s%s", s.M3u8LiveData, tis)
				//s.log.Println(s.M3u8LiveData)
				//清空文件
				err = s.M3u8LiveFile.Truncate(0)
				if err != nil {
					s.log.Println("hlslive ", err)
					return
				}
				_, err = s.M3u8LiveFile.Seek(0, 0)
				if err != nil {
					s.log.Println("hlslive ", err)
					return
				}

				_, err = s.M3u8LiveFile.WriteString(s.M3u8LiveData)
				if err != nil {
					s.log.Printf("hlslive Write %s fail, %s", s.M3u8LivePath, err)
					return
				}
			}
		}
	}
}

func M3u8LiveUpdate(s *Stream) {
	s.log.Printf("hlslive %s done, #EXTINF:%.3f", path.Base(s.TsLivePath), s.TsLiveExtInfo)
	if s.TsLivePath == "" {
		return
	}
	if s.TsLiveRemainName != "" {
		err := os.Remove(s.TsLiveRemainName)
		if err != nil {
			log.Println("hlslive delete ts error ", s.TsLiveRemainName, err)
		}
		//s.log.Printf("live delete ts %s", s.TsLiveRemainName)
	}
	//s.TsNum 初始值为0, conf.HlsLive.M3u8TsNum 至少为3
	if s.TsLiveNum >= uint32(conf.HlsLive.M3u8TsNum) {
		e := s.TsLiveList.Front()
		ti := (e.Value).(TsInfo)
		s.TsLiveRemainName = ti.TsFilepath
		//s.log.Printf("live %d, out ts %s, %s", s.TsLiveFirstSeq, s.TsLiveRemainName, ti.TsFilepath)
		s.TsLiveList.Remove(e)
		s.TsLiveNum--
		s.TsLiveFirstSeq++
	}

	var tiStr string
	switch s.VideoCodecType {
	case "H264":
		tiStr = fmt.Sprintf(m3u8Body, s.TsLiveExtInfo, "avc", path.Base(s.TsLivePath))
	case "H265":
		tiStr = fmt.Sprintf(m3u8Body, s.TsLiveExtInfo, "hevc", path.Base(s.TsLivePath))
	default:
		//sometimes publish stream has no video, use avc to padding
		tiStr = fmt.Sprintf(m3u8Body, s.TsLiveExtInfo, "avc", path.Base(s.TsLivePath))
	}

	//根据需要 加入 #EXT-X-DISCONTINUITY
	if s.HlsLiveAddDiscFlag == true {
		tiStr = fmt.Sprintf("#EXT-X-DISCONTINUITY\n%s", tiStr)
		s.HlsLiveAddDiscFlag = false
	}
	//s.log.Println(tiStr)

	//ti := TsInfo{tiStr, s.TsLiveExtInfo, "", 0}
	tiStr = fmt.Sprintf("%s?mediaServerIp=%s&codeType=%s\n", tiStr, conf.IpInner, s.VideoCodecType)
	ti := TsInfo{tiStr, s.TsLiveExtInfo, "", 0}
	ti.TsFilepath = s.TsLivePath
	s.TsLiveList.PushBack(ti)
	s.TsLiveNum++

	var tsMaxTime float64
	var tis string
	for e := s.TsLiveList.Front(); e != nil; e = e.Next() {
		ti = (e.Value).(TsInfo)
		if tsMaxTime < ti.TsExtInfo {
			tsMaxTime = ti.TsExtInfo
		}
		tis = fmt.Sprintf("%s%s", tis, ti.TsInfoStr)
	}

	//s.M3u8LiveData = fmt.Sprintf(m3u8Head, conf.HlsLive.TsMaxTime, s.TsFirstSeq)
	s.M3u8LiveData = fmt.Sprintf(m3u8Head, uint32(math.Ceil(tsMaxTime)), s.TsLiveFirstSeq)
	s.M3u8LiveData = fmt.Sprintf("%s%s", s.M3u8LiveData, tis)
	//s.log.Println(s.M3u8LiveData)

	if utils.FileExist(s.M3u8LivePath) == false {
		if s.M3u8LiveFile != nil {
			s.M3u8LiveFile.Close()
			s.M3u8LiveFile = nil
		}
		dir := fmt.Sprintf("%s/%s", conf.HlsLive.MemPath, s.AmfInfo.StreamId)
		err := os.Mkdir(dir, 0755)
		if err != nil {
			s.log.Println(err)
			//no need return, because dir is exist
		}
		s.M3u8LiveFile, err = os.OpenFile(s.M3u8LivePath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		s.log.Println("hlslive create ", s.M3u8LivePath)
		if err != nil {
			s.log.Println(err)
			return
		}
	} else {
		//清空文件
		err := s.M3u8LiveFile.Truncate(0)
		if err != nil {
			s.log.Println("hlslive ", err)
			return
		}
		_, err = s.M3u8LiveFile.Seek(0, 0)
		if err != nil {
			s.log.Println("hlslive ", err)
			return
		}
	}
	_, err := s.M3u8LiveFile.WriteString(s.M3u8LiveData)
	if err != nil {
		s.log.Printf("hlslive Write %s fail, %s", s.M3u8LivePath, err)
		return
	}
}

func M3u8LiveFlush(s *Stream) {
	//TODO: last ts not in m3u8
	//M3u8LiveUpdate(s)

	var n *list.Element
	for e := s.TsLiveList.Front(); e != nil; e = n {
		ti := (e.Value).(TsInfo)
		s.log.Printf("hlslive delete list ts: %s", ti.TsFilepath)
		n = e.Next()
		s.TsLiveList.Remove(e)
	}
}

func TsLivePacketCreatePatPmt(s *Stream, pid uint16, data []byte) ([]byte, int) {
	var th TsHeader
	th.SyncByte = 0x47                  // 8bit
	th.TransportErrorIndicator = 0x0    // 1bit
	th.PayloadUnitStartIndicator = 0x1  // 1bit
	th.TransportPriority = 0x0          // 1bit
	th.PID = pid                        // 13bit
	th.TransportScramblingControl = 0x0 // 2bit
	th.AdaptationFieldControl = 0x1     // 2bit
	th.ContinuityCounter = 0x0          // 4bit

	copy(s.TsLivePack, TsPackDefault[:])

	s.TsLivePack[0] = th.SyncByte
	s.TsLivePack[1] = ((th.TransportErrorIndicator & 0x1) << 7) | ((th.PayloadUnitStartIndicator & 0x1) << 6) | ((th.TransportPriority & 0x1) << 5) | (uint8((th.PID & 0x1f00) >> 8))
	s.TsLivePack[2] = uint8(th.PID & 0xff)
	s.TsLivePack[3] = ((th.TransportScramblingControl & 0x3) << 6) | ((th.AdaptationFieldControl & 0x3) << 4) | (th.ContinuityCounter & 0xf)

	//tsHeader和pat之间有1字节的分隔
	//tsHeader和pmt之间有1字节的分隔
	//tsHeader和pes之间无1字节的分隔
	s.TsLivePack[4] = 0x0 //pointer_field???

	dataLen := len(data)
	copy(s.TsLivePack[5:5+dataLen], data)
	return s.TsLivePack, dataLen
}

/*************************************************/
/* tsFile
/*************************************************/
func TsLiveFileCreate(s *Stream, c Chunk) {
	if s.TsLivePath != "" {
		s.TsLiveFileBuf.Flush()
		s.TsLiveFile.Close()
		M3u8LiveUpdate(s)
	}

	//GSP3bnx69BgxI-avEc0oE4C4_44.ts
	//GSP63nBbfmlbW-fnMebne7hU_20220222164158_29306.ts
	s.TsLivePath = fmt.Sprintf("%s/%s/%s_%s_%d.ts", conf.HlsLive.MemPath, s.AmfInfo.StreamId, s.AmfInfo.StreamId, utils.GetYMDHMS(), s.TsLiveLastSeq)

	var err error
	s.TsLiveFile, err = os.OpenFile(s.TsLivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		s.log.Println(err)
		//s.TsLivePath = ""
		return
	}
	//s.log.Printf("hlslive create %s", s.TsLivePath)

	//使用bufio要注意
	//s.TsFile.Close()时不会写入硬盘, 关闭前 必须调 s.TsFileBuf.Flush()
	//想写入硬盘 要么写入数据大于 bufSize, 要么手动调用 Flush()
	//bufSize=10, 写入数据为10个时, 不会写入硬盘
	//bufSize=10, 写入数据为11个时, 会写入硬盘
	//1MB = 1024 * 1024 * 1 = 1048576
	//2MB = 1024 * 1024 * 2 = 2097152
	//bufSize小, 内存使用少写的次数多; bufSize大, 内存使用多写的次数少;
	//bufSize的大小, 需要实际测试 找到合适值
	//1个ts文件大概在1MB到4MB之间, 实际测试bufSize=1MB比较合适
	//XXX: 最好能复用buf, 复用buf时 要把尾部数据长度处理好
	s.TsLiveFileBuf = bufio.NewWriterSize(s.TsLiveFile, 524288)

	/*
		_, patData := PatCreate()
		_, _ = TsLivePacketCreatePatPmt(s, PatPid, patData)
		s.log.Printf("TsPackPatData:%x", s.TsLivePack)
		_, err = s.TsFileBuf.Write(s.TsLivePack)
	*/
	_, err = s.TsLiveFileBuf.Write(TsPackPat[:])
	if err != nil {
		s.log.Println(err)
		s.TsLivePath = ""
		return
	}

	//XXX: 推荐使用 预定义值 TsPackPmtxxx
	_, pmtData := PmtCreate(s)
	_, _ = TsLivePacketCreatePatPmt(s, PmtPid, pmtData)
	//s.log.Printf("TsPackPmtData:%x", s.TsLivePack)
	_, err = s.TsLiveFileBuf.Write(s.TsLivePack)
	if err != nil {
		s.log.Println(err)
		s.TsLivePath = ""
		return
	}

	TsLiveFileAppend(s, c)
	if s.AudioLiveChunk.DataType == "AudioAacFrame" {
		//s.log.Printf("create ts about add audio chunk after video")
		TsLiveFileAppend(s, s.AudioLiveChunk)
		s.AudioLiveChunk.DataType = ""
	}
	s.TsLiveFirstTs = c.Timestamp
	s.TsLiveLastSeq++
}

func PrepareSpsPpsLiveData(s *Stream, c *Chunk) {
	// 前5个字节上面已经处理，AVC sequence header从第6个字节开始
	// 0x17 0x00 0x00 0x00 0x00 0x01 0x4d 0x00 0x29 0x03 0x01 0x00 0x18
	// 0x67, 0x4d, 0x0, 0x29, 0x96, 0x35, 0x40, 0xf0, 0x4, 0x4f, 0xcb, 0x37, 0x01, 0x1, 0x1, 0x40, 0x0, 0x0, 0xfa, 0x0, 0x0, 0x17, 0x70, 0x01
	// 0x01 0x00 0x04 0x68 0xee 0x31 0xb2
	s.log.Printf("hlslive AVC body data:%v, %x", len(c.MsgData), c.MsgData)
	if len(c.MsgData) < 11 {
		s.log.Printf("hlslive AVC body no enough data:%v", len(c.MsgData))
		return
	}
	numOfSps := c.MsgData[10] & 0x1F // 5bit, 0xe1

	var temp uint16
	var spsLen [32]uint16
	var spsData [32][]byte
	var totalSpsLen uint32
	var i uint8
	for i = 0; i < numOfSps; i++ {
		if len(c.MsgData) <= int(13+temp) {
			s.log.Printf("hlslive AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}
		spsLen[i] = ByteToUint16(c.MsgData[11+temp:13+temp], BE) // 16bit, 0x001c

		if len(c.MsgData) <= int(11+temp+uint16(spsLen[i])) {
			s.log.Printf("hlslive AVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}
		spsData[i] = c.MsgData[13+temp : 13+temp+uint16(spsLen[i])]
		temp += 2 + spsLen[i]
		totalSpsLen += uint32(spsLen[i])
	}
	EndPos := 11 + temp // 11 + 2 + 28

	numOfPps := c.MsgData[EndPos] // 8bit, 0x01
	var ppsData [256][]byte
	var ppsLen [256]uint16
	var totalPpsLen uint32
	temp = EndPos + 1
	for i = 0; i < numOfPps; i++ {
		if len(c.MsgData) <= int(2+temp) {
			s.log.Printf("hlslive AVC body no enough data:%d, %v, %x", (2 + temp), len(c.MsgData), c.MsgData)
			return
		}
		ppsLen[i] = ByteToUint16(c.MsgData[temp:2+temp], BE) // 16bit, 0x0004
		if len(c.MsgData) < int(2+ppsLen[i]+temp) {
			s.log.Printf("hlslive AVC body no enough data:%d, %d, %x", (2 + ppsLen[i] + temp), len(c.MsgData), c.MsgData)
			return
		}
		ppsData[i] = c.MsgData[2+temp : 2+temp+uint16(ppsLen[i])]
		temp += 2 + uint16(ppsLen[i])
		totalPpsLen += uint32(ppsLen[i])
	}

	s.log.Printf("hlslive numOfSps:%d, numOfPps%d, spsLen:%d, spsData:%x, ppsLen:%d, ppsData:%x", numOfSps, numOfPps, spsLen[0], spsData[0], ppsLen[0], ppsData[0])

	size := 4*uint32(numOfSps) + totalSpsLen + 4*uint32(numOfPps) + totalPpsLen
	s.SpsPpsLiveData = make([]byte, size)
	//有些播放器的兼容性不好, 这里最好用0x00000001
	var len1 uint32
	var len2 uint32
	for i = 0; i < numOfSps; i++ {
		Uint32ToByte(0x00000001, s.SpsPpsLiveData[len2:4+len2], BE)
		copy(s.SpsPpsLiveData[4+len2:4+len2+uint32(spsLen[i])], spsData[i])
		len1 += uint32(spsLen[i])
		len2 = 4 + len1
	}

	for i = 0; i < numOfPps; i++ {
		Uint32ToByte(0x00000001, s.SpsPpsLiveData[len2:4+len2], BE)
		copy(s.SpsPpsLiveData[4+len2:4+len2+uint32(ppsLen[i])], ppsData[i])
		len1 += uint32(ppsLen[i])
		len2 = 4 + len1
	}
}

// NaluLen and NaluData may be not consistent, should parsing chunk data instead of using HevcC data
func PrepareSpsPpsLiveDataH265(s *Stream, c *Chunk) {
	if len(c.MsgData) < 28 {
		s.log.Printf("hlslive HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
		return
	}
	//len(c.MsgData) = 135
	//1c 00 00 00 00
	//前5个字节上面已经处理，HEVC sequence header从第6个字节开始
	//01 01 60 00 00 00 80 00 00 00
	//00 00 78 f0 00 fc fd f8 f8 00
	//00 ff 03 20 00 01 00 17 40 01
	//0c 01 ff ff 01 60 00 00 03 00
	//80 00 00 03 00 00 03 00 78 ac
	//09 21 00 01 00 3c 42 01 01 01
	//60 00 00 03 00 80 00 00 03 00
	//00 03 00 78 a0 02 80 80 2d 1f
	//e3 6b bb c9 2e b0 16 e0 20 20
	//20 80 00 01 f4 00 00 30 d4 39
	//0e f7 28 80 3d 30 00 44 de 00
	//7a 60 00 89 bc 40 22 00 01 00
	//09 44 01 c1 72 b0 9c 38 76 24
	var HevcC HEVCDecoderConfigurationRecord
	HevcC.ConfigurationVersion = c.MsgData[5] // 8bit, 0x01
	// 中间这些字段, 我们不关心
	HevcC.NumOfArrays = c.MsgData[27] // 8bit, 一般为3
	//s.log.Printf("hevc %x, %x", HevcC.ConfigurationVersion, HevcC.NumOfArrays)

	var i, j, k uint16 = 0, 28, 0
	var hn HVCCNALUnit
	for ; i < uint16(HevcC.NumOfArrays); i++ {
		if len(c.MsgData) < int(j+3) {
			s.log.Printf("hlslive HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
			return
		}

		hn.ArrayCompleteness = c.MsgData[j] >> 7
		hn.Reserved0 = (c.MsgData[j] >> 6) & 0x1
		hn.NALunitType = c.MsgData[j] & 0x3f
		j++
		//hn.NumNalus > 1 这种情况非常少, ffmpeg里只会写一个, srs代码里会判断是否为多个
		//协议规定可以有多个vps sps..., 建议使用第一个vps sps...,我们使用最后一个vps sps...
		hn.NumNalus = ByteToUint16(c.MsgData[j:j+2], BE)
		j += 2
		for k = 0; k < hn.NumNalus; k++ {
			if len(c.MsgData) < int(j+2) {
				s.log.Printf("hlslive HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				return
			}
			hn.NaluLen = ByteToUint16(c.MsgData[j:j+2], BE)
			j += 2
			if len(c.MsgData) < int(j+hn.NaluLen) {
				s.log.Printf("hlslive HEVC body no enough data:%v, %x", len(c.MsgData), c.MsgData)
				return
			}
			hn.NaluData = c.MsgData[j : j+hn.NaluLen]
			j += hn.NaluLen
			s.log.Printf("%#v", hn)

			switch hn.NALunitType {
			case 32: // 0x20
				s.log.Printf("hlslive NaluType=%d is VPS", hn.NALunitType)
				HevcC.Vps = append(HevcC.Vps, hn)
			case 33: // 0x21
				s.log.Printf("hlslive NaluType=%d is SPS", hn.NALunitType)
				HevcC.Sps = append(HevcC.Sps, hn)
			case 34: // 0x22
				s.log.Printf("hlslive NaluType=%d is PPS", hn.NALunitType)
				HevcC.Pps = append(HevcC.Pps, hn)
			case 39: // 0x27
				s.log.Printf("hlslive NaluType=%d is SEI", hn.NALunitType)
				HevcC.Sei = append(HevcC.Sei, hn)
			default:
				s.log.Printf("hlslive NaluType=%d untreated", hn.NALunitType)
			}
		}
	}

	var size uint16
	for _, value := range HevcC.Vps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	for _, value := range HevcC.Sps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	for _, value := range HevcC.Pps {
		size += 4 + value.NaluLen
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
	}
	//size := 12 + HevcC.Vps.NaluLen + HevcC.Sps.NaluLen + HevcC.Pps.NaluLen
	s.SpsPpsLiveData = make([]byte, size)

	var sp uint16
	//有些播放器的兼容性不好, 这里最好用0x00000001
	for _, value := range HevcC.Vps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsLiveData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsLiveData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
	for _, value := range HevcC.Sps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsLiveData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsLiveData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
	for _, value := range HevcC.Pps {
		//s.log.Printf("index:%d, naluLen:%d", index, value.NaluLen)
		Uint32ToByte(0x00000001, s.SpsPpsLiveData[sp:sp+4], BE)
		sp += 4
		copy(s.SpsPpsLiveData[sp:sp+value.NaluLen], value.NaluData)
		sp += value.NaluLen
	}
}

func PrepareAdtsLiveData(s *Stream, c *Chunk) {
	var AacC AudioSpecificConfig
	AacC.ObjectType = (c.MsgData[2] & 0xF8) >> 3 // 5bit
	AacC.SamplingIdx =
		((c.MsgData[2] & 0x7) << 1) | (c.MsgData[3] >> 7) // 4bit
	AacC.ChannelNum = (c.MsgData[3] & 0x78) >> 3     // 4bit
	AacC.FrameLenFlag = (c.MsgData[3] & 0x4) >> 2    // 1bit
	AacC.DependCoreCoder = (c.MsgData[3] & 0x2) >> 1 // 1bit
	AacC.ExtensionFlag = c.MsgData[3] & 0x1          // 1bit
	// 2, 4, 2, 0(1024), 0, 0
	s.log.Printf("hlslive: %#v", AacC)

	//ff f9 50 80 00 ff fc  //自己测试文件自己代码生成的
	// 11111111 11111001 01010000 10000000 00000000 11111111 11111100
	// fff 1 00 1 01 0100 0 010 0 0 0 0 0000000000111 11111111111 00
	//FF F9 68 40 5C FF FC  //别人测试ts文件直接读取的
	// 11111111 11111001 01101000 01000000 01011100 11111111 11111100
	// fff 1 00 1 01 1010 0 001 0 0 0 0 0001011100111 11111111111 00
	var adts Adts
	adts.Syncword = 0xfff
	adts.Id = 0x1 // 1bit, MPEG Version: 0 is MPEG-4, 1 is MPEG-2
	adts.Layer = 0x0
	adts.ProtectionAbsent = 0x1
	adts.ProfileObjectType = AacC.ObjectType - 1
	adts.SamplingFrequencyIndex = AacC.SamplingIdx
	adts.PrivateBit = 0x0
	adts.ChannelConfiguration = AacC.ChannelNum
	adts.OriginalCopy = 0x0
	adts.Home = 0x0
	adts.CopyrightIdentificationBit = 0x0
	adts.CopyrightIdentificationStart = 0x0
	// 这里不知道aac数据长度, 所以先复制为0x7
	adts.AacFrameLength = 0x7
	adts.AdtsBufferFullness = 0x7ff
	adts.NumberOfRawDataBlocksInFrame = 0x0
	//s.log.Printf("%#v", adts)

	s.AdtsLiveData = make([]byte, 7)
	s.AdtsLiveData[0] = 0xff
	s.AdtsLiveData[1] = 0xf0 | ((adts.Id & 0x1) << 3) | ((adts.Layer & 0x3) << 1) | (adts.ProtectionAbsent & 0x1)
	s.AdtsLiveData[2] = ((adts.ProfileObjectType & 0x3) << 6) | ((adts.SamplingFrequencyIndex & 0xf) << 2) | ((adts.PrivateBit & 0x1) << 1) | ((adts.ChannelConfiguration & 0x4) >> 2)
	s.AdtsLiveData[3] = ((adts.ChannelConfiguration & 0x3) << 6) | ((adts.OriginalCopy & 0x1) << 5) | ((adts.Home & 0x1) << 4) | ((adts.CopyrightIdentificationBit & 0x1) << 3) | ((adts.CopyrightIdentificationStart & 0x1) << 2) | uint8((adts.AacFrameLength>>11)&0x3)
	s.AdtsLiveData[4] = uint8((adts.AacFrameLength >> 3) & 0xff)
	s.AdtsLiveData[5] = (uint8(adts.AacFrameLength&0x7) << 5) | uint8((adts.AdtsBufferFullness>>6)&0x1f)
	s.AdtsLiveData[6] = (uint8((adts.AdtsBufferFullness & 0x3f) << 2)) | (adts.NumberOfRawDataBlocksInFrame & 0x3)
	//s.log.Printf("AdtsLiveData: %x", s.AdtsLiveData)
}

func PesLiveDataCreateVideoFrame(s *Stream, c Chunk, phd []byte) []byte {
	pesHeaderDataLen := uint32(len(phd))

	var SpsPpsDataLen uint32 = 0
	if c.DataType == "VideoKeyFrame" {
		SpsPpsDataLen = uint32(len(s.SpsPpsLiveData))
	}

	MsgDataLen := c.MsgLength - 5
	dataLen := pesHeaderDataLen + SpsPpsDataLen + 6 + MsgDataLen
	if s.VideoCodecType == "H265" {
		dataLen += 1
	}

	data := make([]byte, dataLen)

	var ss uint32
	var ee uint32
	ss = 0
	ee = pesHeaderDataLen
	if ee > dataLen {
		return nil
	}
	copy(data[ss:ee], phd)

	ss = ee
	ee += 4
	if ee > dataLen {
		return nil
	}
	Uint32ToByte(0x00000001, data[ss:ee], BE)

	ss = ee
	switch s.VideoCodecType {
	case "H264":
		ee += 2
		if ee > dataLen {
			return nil
		}
		Uint16ToByte(0x09f0, data[ss:ee], BE)
	case "H265":
		ee += 3
		if ee > dataLen {
			return nil
		}
		Uint24ToByte(0x460150, data[ss:ee], BE)
	}

	if c.DataType == "VideoKeyFrame" {
		ss = ee
		ee += SpsPpsDataLen
		if ee > dataLen {
			return nil
		}
		copy(data[ss:ee], s.SpsPpsLiveData)
	}

	//这里可能有多个nalu, 每个nalu前都要有起始码0x00000001
	//如果是nalu都是数据, 应该第一个nalu起始码用0x00000001
	//后续nalu起始码用0x000001, 不过都用4字节01 播放器和解码器也能处理
	//详见 GetNaluNum() 说明
	//不能改变原始数据, 所以要分段拷贝
	var s0 uint32
	var e0 uint32
	s0 = 5
	e0 = s0 + 4
	var i, naluLen uint32
	for i = 0; i < c.NaluNum; i++ {
		if e0 > c.MsgLength {
			return nil
		}
		naluLen = ByteToUint32(c.MsgData[s0:e0], BE)

		ss = ee
		ee += 4
		if ee > dataLen {
			return nil
		}
		Uint32ToByte(0x00000001, data[ss:ee], BE)

		ss = ee
		ee += naluLen
		s0 = e0
		e0 += naluLen
		//s.log.Printf("pes %v, %v, %v, %v, %v, %v, %v, %v, %v", i, c.NaluNum, dataLen, c.MsgLength, naluLen, ss, ee, s0, e0)
		if ee > dataLen || e0 > c.MsgLength {
			return nil
		}
		copy(data[ss:ee], c.MsgData[s0:e0])

		s0 = e0
		e0 += 4
	}

	if strings.Contains(s.Key, conf.Debug.StreamId) {
		s.log.Printf("hlslive %s write in dType=%s, NaluNum=%d, dLen=%d", path.Base(s.TsLivePath), c.DataType, c.NaluNum, len(data))
	}
	return data
}

func PesLiveDataCreateAacFrame(s *Stream, c Chunk, phd []byte) []byte {
	pesHeaderDataLen := uint32(len(phd))

	var MsgDataLen uint32 = 0
	if c.MsgLength > 2 {
		MsgDataLen = c.MsgLength - 2
	}
	dataLen := pesHeaderDataLen + 7 + MsgDataLen

	//ParseAdtsData(s)
	if len(s.AdtsLiveData) < 7 {
		s.log.Printf("hlslive adts header len %d less then 7", len(s.AdtsLiveData))
		return nil
	}
	SetAdtsLength(s.AdtsLiveData, uint16(7+MsgDataLen))

	data := make([]byte, dataLen)

	var ss uint32
	var ee uint32
	ss = 0
	ee = pesHeaderDataLen
	copy(data[ss:ee], phd)

	ss = ee
	ee += 7
	copy(data[ss:ee], s.AdtsLiveData)

	ss = ee
	if MsgDataLen > 0 && len(c.MsgData) > 2 {
		ee += MsgDataLen
		copy(data[ss:], c.MsgData[2:])
	}
	return data
}

func TsLivePacketCreate(s *Stream, c Chunk, data []byte, pcr uint64, first bool) ([]byte, int) {
	dataLen := len(data)

	var th TsHeader
	th.SyncByte = 0x47
	th.TransportErrorIndicator = 0x0
	th.PayloadUnitStartIndicator = 0x0
	if first {
		th.PayloadUnitStartIndicator = 0x1
	}
	th.TransportPriority = 0x0
	th.PID = VideoPid
	if c.DataType == "AudioAacFrame" {
		th.PID = AudioPid
	}
	th.TransportScramblingControl = 0x0
	th.AdaptationFieldControl = 0x1
	//最后一段数据 只有182字节可用于放数据, 188-tsHeader(4)-适应区(2)
	if first || dataLen <= 183 {
		th.AdaptationFieldControl = 0x3
	}

	/*
	   adaptation_field_length 的值将为 0~182 之间,
	   值为 0 是为了在 TS Packet 中插入单个的填充字节 (非常重要)
	   帧尾部数据剩余188字节 无适应区, 4+184 剩余4字节
	   帧尾部数据剩余187字节 无适应区, 4+184 剩余3字节
	   帧尾部数据剩余186字节 无适应区, 4+184 剩余2字节
	   帧尾部数据剩余185字节 无适应区, 4+184 剩余1字节
	   帧尾部数据剩余184字节 无适应区, 4+184 剩余0字节
	   帧尾部数据剩余183字节 有适应区, 4+1+183 填充0字节 (非常重要)
	   帧尾部数据剩余182字节 有适应区, 4+2+182 填充0字节
	   帧尾部数据剩余181字节 有适应区, 4+2+181 填充1字节
	   帧尾部数据剩余180字节 有适应区, 4+2+180 填充2字节
	*/

	switch th.PID {
	case AudioPid:
		th.ContinuityCounter = s.AudioLiveCounter
		s.AudioLiveCounter++
		if s.AudioLiveCounter > 0xf {
			s.AudioLiveCounter = 0x0
		}
	case VideoPid:
		th.ContinuityCounter = s.VideoLiveCounter
		s.VideoLiveCounter++
		if s.VideoLiveCounter > 0xf {
			s.VideoLiveCounter = 0x0
		}
	}

	var a *Adaptation
	if th.AdaptationFieldControl == 0x3 {
		a = NewAdaptation(c, first)

		if dataLen == 183 {
			a.AdaptationFieldLength = 0x0
		}
	}

	useLen := 0
	copy(s.TsLivePack, TsPackDefault[:])

	s.TsLivePack[0] = th.SyncByte
	s.TsLivePack[1] = ((th.TransportErrorIndicator & 0x1) << 7) | ((th.PayloadUnitStartIndicator & 0x1) << 6) | ((th.TransportPriority & 0x1) << 5) | (uint8((th.PID & 0x1f00) >> 8))
	s.TsLivePack[2] = uint8(th.PID & 0xff)
	s.TsLivePack[3] = ((th.TransportScramblingControl & 0x3) << 6) | ((th.AdaptationFieldControl & 0x3) << 4) | (th.ContinuityCounter & 0xf)
	useLen += 4

	if th.AdaptationFieldControl == 0x3 {
		s.TsLivePack[4] = a.AdaptationFieldLength
		useLen += 1

		if a.AdaptationFieldLength != 0x0 {
			s.TsLivePack[5] = ((a.DiscontinuityIndicator & 0x1) << 7) | ((a.RandomAccessIndicator & 0x1) << 6) | ((a.ElementaryStreamPriorityIndicator & 0x1) << 5) | ((a.PcrFlag & 0x1) << 4) | ((a.OpcrFlag & 0x1) << 3) | ((a.SplicingPointFlag & 0x1) << 2) | ((a.TransportPrivateDataFlag & 0x1) << 1) | (a.AdaptationFieldExtensionFlag & 0x1)
			useLen += 1
		}

		if a.PcrFlag == 0x1 {
			PackPcr(s.TsLivePack[useLen:], pcr)
			useLen += 6
		}
	}

	remainLen := 188 - useLen
	//s.log.Printf("dataLen=%d, freeBuffLen=%d", dataLen, freeBuffLen)
	if dataLen >= remainLen {
		dataLen = remainLen
		copy(s.TsLivePack[useLen:useLen+remainLen], data)
	} else {
		padLen := 188 - useLen - dataLen
		if th.AdaptationFieldControl == 0x3 {
			s.TsLivePack[4] = a.AdaptationFieldLength + uint8(padLen)
		}
		copy(s.TsLivePack[188-dataLen:], data)
	}
	return s.TsLivePack, dataLen
}

func TsLiveFileAppend(s *Stream, c Chunk) {
	pesHeader, pesHeaderData := PesHeaderCreate(s, c)

	var pesData []byte
	switch c.DataType {
	case "VideoKeyFrame":
		//pesData = PesDataCreateKeyFrame(s, c, pesHeaderData)
		pesData = PesLiveDataCreateVideoFrame(s, c, pesHeaderData)
	case "VideoInterFrame":
		//pesData = PesDataCreateInterFrame(s, c, pesHeaderData)
		pesData = PesLiveDataCreateVideoFrame(s, c, pesHeaderData)
	case "AudioAacFrame":
		pesData = PesLiveDataCreateAacFrame(s, c, pesHeaderData)
	}

	pesDataLen := len(pesData)
	if pesDataLen < 6 {
		s.log.Printf("hlslive pes data not enough %v, type:%s", pesDataLen, c.DataType)
		return
	}
	SetPesPakcetLength(pesData, pesDataLen-6)

	first := true
	var sp, useLen int
	var err error
	for {
		s.TsLivePack, useLen = TsLivePacketCreate(s, c, pesData[sp:], pesHeader.Pcr, first)
		sp += useLen

		_, err = s.TsLiveFileBuf.Write(s.TsLivePack)
		if err != nil {
			s.log.Println(err)
			return
		}

		if sp >= pesDataLen {
			//s.log.Printf("sp=%d, pesDataLen=%d", sp, pesDataLen)
			break
		}
		first = false
	}
}

func TsLiveCreate(s *Stream, c Chunk) bool {
	var dv uint32
	if c.Timestamp >= s.TsLiveFirstTs {
		//时间戳递增: c.Timestamp(46417939) - s.TsLiveFirstTs(46286804) = dv(131135)
		//rtmp推流上来后 第一帧数据 会造成 s.TsLiveExtInfo 值很大
		//如果后续不来帧, 那生成的 xxx_0.ts 时间会很大
		if s.TsLivePath == "" && s.TsLiveFirstTs == 0 {
			dv = 0
		} else {
			dv = c.Timestamp - s.TsLiveFirstTs
		}

		//视频帧率60fps, 帧间隔1000/60=16.7ms
		//视频帧率25fps, 帧间隔1000/25=  40ms
		//视频帧率20fps, 帧间隔1000/20=  50ms
		//视频帧率 2fps, 帧间隔1000/ 5= 500ms
		//视频帧率 1fps, 帧间隔1000/ 5=1000ms
		//音画相差400ms, 人类就能明显感觉到不同步
		//帧间隔跳跃是否太大的打印, 查看代码 bigjump 关键字
	} else {
		//时间戳回绕: c.Timestamp(8) - s.TsLiveFirstTs(47720666) = dv(4247246637)
		//时间戳回跳: c.Timestamp(28362000) - s.TsLiveFirstTs(28362676) = dv(47721182)
		//47720666 - 8        = 47720658 > 23860929
		//28362676 - 28362000 =      676 < 23860929
		dv = s.TsLiveFirstTs - c.Timestamp
		//23860929 = 0x16C16C1 = 0x2D82D82/2
		if dv >= 23860929 { //回绕
			//8 + (47721858 - 47720666) = 1200
			dv = c.Timestamp + (0x2D82D82 - s.TsLiveFirstTs)
			//8 + (95443717 - 47720666) = 47723059
			dv1 := c.Timestamp + (0x5B05B05 - s.TsLiveFirstTs)
			//8 + (4294967295 - 47720666) = 4247246629
			dv2 := c.Timestamp + (0xFFFFFFFF - s.TsLiveFirstTs)
			if dv > dv1 {
				dv = dv1
			}
			if dv > dv2 {
				dv = dv2
			}
			//s.log.Printf("dv=%d, dv1=%d, dv2=%d", dv, dv1, dv2)
			s.log.Printf("hlslive rollback: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsLiveFirstTs, dv, c.DataType)
		} else { //回跳
			//s.TsFirstTs是视频关键帧的时间戳, 音频时间戳偶尔小幅度回跳是正常的
			//回跳时间差 FirstTs - c.Ts <= conf.HlsLive.TsMaxTime, 应该把数据写入ts
			//回跳时间差 FirstTs - c.Ts >  conf.HlsLive.TsMaxTime, 应该截断ts
			//28362676 - 28362000 = 676 <= 10000
			//dv = s.TsLiveFirstTs - c.Timestamp
			s.log.Printf("hlslive jumpback: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsLiveFirstTs, dv, c.DataType)
		}
	}
	//when delta is larger than 20 seconds, it will print
	/*	if dv > 20*1000 && s.TsLiveFirstTs != 0 {
			s.log.Printf("live morethan: c.Ts(%d) - FirstTs(%d) = dv(%d), %s", c.Timestamp, s.TsLiveFirstTs, dv, c.DataType)
		}
	*/
	//这是最后的保障, 为了纠正上面对时间戳计算可能发生的错误 或 其他意外情况
	//1小时=3600秒=3600000毫秒
	if s.TsLivePath != "" && dv >= 3600000 {
		s.log.Printf("hlslive dv >= 3600000ms, force dv = %fms", conf.HlsLive.TsMaxTime*1000)
		dv = uint32(conf.HlsLive.TsMaxTime * 1000)
	}

	//rtmp里的timestamp单位是毫秒, 除以1000变为秒
	s.TsLiveExtInfo = float64(dv) / 1000
	//TODO: when all of the video timestamp is equal to zero, force set ts duration.
	//when ts size is equal to 10MB, suppose bitrate is 3Mb, the duration is about 30s
	if dv == 0 {
		if s.TsLiveMaxCutSize >= conf.HlsLive.TsMaxSize && c.DataType == "VideoKeyFrame" {
			s.TsLiveExtInfo = 5
		} else if s.TsLiveMaxCutSize >= 2*conf.HlsLive.TsMaxSize {
			s.TsLiveExtInfo = 10
		}
	}

	s.TsLiveMaxCutSize += uint32(len(c.MsgData))
	var ok bool
	//TODO: gateway的rtmp流里可能没有关键帧, 要防止产生大ts, 比如 30min时间限制 或 20MB文件限制
	//摄像头关键帧间隔一般为固定值50, 经过观察 发现线上有些流 关键帧间隔不固定 最长20分钟左右才有关键帧
	//TS文件大于10MB且是关键帧强制切割或者TS文件大于20MB强制切割
	//纯音频支持切片
	_, ok = s.GopCache.VideoHeader.Load(s.Key)
	//s.log.Printf("test andrew res:%v, key:%s, type:%s, %v, %s", ok, s.Key, c.DataType, s.TsLiveExtInfo, s.TsLivePath)
	if s.TsLivePath == "" || (s.TsLiveExtInfo >= float64(conf.HlsLive.TsMaxTime) && c.DataType == "VideoKeyFrame") || s.TsLiveExtInfo >= 60 ||
		(s.TsLiveMaxCutSize >= conf.HlsLive.TsMaxSize && c.DataType == "VideoKeyFrame") || s.TsLiveMaxCutSize >= 2*conf.HlsLive.TsMaxSize ||
		(ok == false && s.TsLiveExtInfo >= float64(conf.HlsLive.TsMaxTime) && c.DataType == "AudioAacFrame") {
		//s.log.Printf("create ts previous data type:%s, cur data type: %s", s.AudioLiveChunk.DataType, c.DataType)
		TsLiveFileCreate(s, c)
		ok = true
		s.TsLiveMaxCutSize = 0
	} else {
		//s.log.Printf("append ts previous data type:%s, cur data type: %s", s.AudioLiveChunk.DataType, c.DataType)
		if c.DataType == "AudioAacFrame" {
			if s.AudioLiveChunk.DataType == "AudioAacFrame" {
				TsLiveFileAppend(s, s.AudioLiveChunk)
			}
			s.AudioLiveChunk = c
		} else {
			TsLiveFileAppend(s, c)
		}
		ok = false
	}

	return ok
}

func HlsLiveCreator(s *Stream) {
	defer s.Wg.Done()
	dir := fmt.Sprintf("%s/%s", conf.HlsLive.MemPath, s.AmfInfo.StreamId)
	err := utils.DirExist(dir, true)
	if err != nil {
		s.log.Println(err)
		return
	}
	s.M3u8LivePath = fmt.Sprintf("%s/%s.m3u8", dir, s.AmfInfo.StreamId)
	s.log.Println(s.M3u8LivePath)

	s.TsLiveList = list.New()

	//发生错误, 记得要关闭 s.M3u8File.Close()
	s.M3u8LiveFile, err = os.OpenFile(s.M3u8LivePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		s.log.Println(err)
		return
	}

	HlsLiveClear(dir, s)

	s.TsLivePack = make([]byte, 188)

	var c Chunk
	var ok bool
	var i uint32
	for {
		select {
		case c, ok = <-s.HlsLiveChan:
			if ok == false {
				s.log.Printf("hlslive %s HlsLiveCreator stop", s.Key)
				return
			}
		case <-s.Ctx.Done():
			s.log.Printf("publish stop then hls live stop")
			return

		}
		//if strings.Contains(s.Key, conf.Debug.StreamId) {
		s.log.Printf("hlslive HlsMsgIdx=%d, fmt=%d, csid=%d, ts=%d, MsgLen=%d, MsgTypeId=%d, DataType=%s, NaluNum=%d", i, c.Fmt, c.Csid, c.Timestamp, c.MsgLength, c.MsgTypeId, c.DataType, c.NaluNum)
		//}
		i++

		switch c.DataType {
		case "Metadata":
			continue // 此数据不该直接写入ts
		case "VideoHeader":
			switch s.VideoCodecType {
			case "H264":
				PrepareSpsPpsLiveData(s, &c)
			case "H265":
				PrepareSpsPpsLiveDataH265(s, &c)
			default:
			}
			s.HlsLiveAddDiscFlag = true
			continue // 此数据不该直接写入ts
		case "AudioHeader":
			PrepareAdtsLiveData(s, &c)
			//ParseAdtsData(s)
			s.HlsLiveAddDiscFlag = true
			continue // 此数据不该直接写入ts
		default:
			//这里是 音视频数据 和 未定义类型数据
		}

		TsLiveCreate(s, c)
	}
}
