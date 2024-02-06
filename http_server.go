package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func HttpGetHandler(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var rsps []byte
	var err error

	url := r.URL.String()
	if strings.Contains(url, "/api/version") {
		rsps, err = GetVersion(w, r)
		w.Header().Set("Content-Type", "application/json")
	} else if strings.Contains(url, ".flv") {
		rsps, err = GetFlv(w, r)
	} else if strings.Contains(url, ".m3u8") {
		rsps, err = GetM3u8(w, r)
		//safari地址栏输入播放地址  必须有这个 才能播放
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.Contains(url, ".ts") {
		rsps, err = GetTs(w, r)
	} else if strings.Contains(url, "action=get_pushChannels") {
		rsps, err = GB28181StreamList(w, r)
		w.Header().Set("Content-Type", "application/json")
	} else {
		err = fmt.Errorf("undefined GET request")
		return nil, err
	}
	return rsps, err
}

func HttpPostHandler(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	d, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	log.Printf("PostData: %s", string(d))

	var rsps []byte
	url := r.URL.String()

	if strings.Contains(url, "action=create_pullChannel") {
		rsps, err = GB28181Create(w, r, d)
	} else if strings.Contains(url, "action=start_pullChannel") {
		rsps, err = GB28181Start(w, r, d)
	} else if strings.Contains(url, "action=delete_pullChannel") {
		rsps, err = GB28181Delete(w, r, d)
	} else if strings.Contains(url, "action=create_streamProxy") {
		rsps, err = HttpApiRtspPullCreate(w, r, d)
	} else if strings.Contains(url, "action=delete_streamProxy") {
		rsps, err = HttpApiRtspPullDelete(w, r, d)
	} else {
		err = fmt.Errorf("undefined POST request")
		return nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	return rsps, err
}

func HttpHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("====== new httpApi request ======")
	log.Println(r.Proto, r.Method, r.URL, r.RemoteAddr, r.Host)

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Server", AppName)

	var rsps []byte
	var err error
	if r.Method == "GET" {
		rsps, err = HttpGetHandler(w, r)
	} else if r.Method == "POST" {
		rsps, err = HttpPostHandler(w, r)
	} else {
		err = fmt.Errorf("undefined %s request", r.Method)
	}
	if err != nil {
		log.Println(err)
		goto ERR
	}

	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Content-length", strconv.Itoa(len(rsps)))
	w.Write(rsps)
	return
ERR:
	rsps = GetRsps(500, err.Error())
	w.Header().Set("Content-length", strconv.Itoa(len(rsps)))
	w.Write(rsps)
}

func HttpServer() {
	http.HandleFunc("/", HttpHandler)

	addr := fmt.Sprintf("%s:%s", "0.0.0.0", conf.Http.PortApi)
	log.Printf("==> http listen on %s", addr)
	go func() {
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Fatal(err)
		}
	}()

	if conf.Https.Enable == true {
		addr = fmt.Sprintf("%s:%s", "0.0.0.0", conf.Https.PortApi)
		log.Println("==> https listen on %s", addr)
		go func() {
			https := &http.Server{
				Addr: addr,
				TLSConfig: &tls.Config{
					CipherSuites: CsArr,
				},
			}
			err := https.ListenAndServeTLS(conf.Https.PubKey, conf.Https.PriKey)
			if err != nil {
				log.Fatal(err)
			}
		}()
	}
}
