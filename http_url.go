package main

import (
	"fmt"
	"log"
	"strings"
)

type UrlArgs struct {
	Url  string //完整url
	Auth string //认证信息, 如 username:password@ip:port
	Ptcl string //协议 rtsp/rtmp/http等
	Ip   string
	Port string
	Path []string //port和?之间的内容
	Args string   //?后面的内容
	Key  string   //ip_port_path[1]_path[n]
}

func UrlParse(url string) (UrlArgs, error) {
	var ua UrlArgs
	var err error
	if url == "" {
		err = fmt.Errorf("url is empty")
		log.Println(err)
		return ua, err
	}
	ua.Url = url

	sArr := strings.Split(url, "?")
	Url := sArr[0]
	if len(sArr) > 1 {
		ua.Args = sArr[1]
	}

	sArr = strings.Split(Url, "/")
	b := []byte(sArr[0])
	l := len(b)
	ua.Ptcl = string(b[:l-1])
	if len(sArr) < 3 {
		err = fmt.Errorf("url %s format error", url)
		log.Println(err)
		return ua, err
	}

	s1 := strings.Split(sArr[2], "@")
	if len(s1) == 1 {
		s1 = strings.Split(s1[0], ":")
		ua.Ip = s1[0]
		ua.Port = s1[1]
	} else if len(s1) == 2 {
		ua.Auth = s1[0]
		s1 = strings.Split(s1[1], ":")
		ua.Ip = s1[0]
		ua.Port = s1[1]
	}

	ua.Key = fmt.Sprintf("%s_%s", ua.Ip, ua.Port)
	if len(sArr) > 3 {
		for i := 3; i < len(sArr); i++ {
			ua.Path = append(ua.Path, sArr[i])
			ua.Key = fmt.Sprintf("%s_%s", ua.Key, sArr[i])
		}
	}
	return ua, nil
}
