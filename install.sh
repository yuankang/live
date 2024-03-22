#!/bin/bash

#pid=`ps -ef | grep "slivegateway.*-c" | grep -v grep | grep -v vi | awk '{print $2}'`
#kill -9 $pid
#echo "======================================"

go build -ldflags "-X main.CompileDate=`date +%Y%m%d%H%M%S`" sms
echo "======================================"

mkdir -p /usr/local/sms/
/usr/local/sms/sms -u
rm -rf /usr/local/sms/sms
rm -rf /usr/local/sms/sms*.log
rm -rf /usr/local/sms/sms.json
rm -rf /usr/local/sms/streamlog
rm -rf /usr/local/sms/record
cp sms /usr/local/sms/sms
cp sms.json /usr/local/sms/sms.json
/usr/local/sms/sms -d

sleep 1s
echo "ps -ef | grep -v grep | grep -E \"watcher.sh|slivegateway|sms\""
ps -ef | grep -v grep | grep -E "watcher.sh|slivegateway|sms"
echo "======================================"

echo "vim streamlog/GSPq4nchhCcSk-acuv6p3262/GbPub_20240318.log"
echo "more /usr/local/slivegateway/log/slivegateway.log"
echo "cp /usr/local/watcher/watcher0.sh /usr/local/watcher/watcher.sh"
echo "sh /usr/local/watcher/stop.sh"
echo "sh /usr/local/watcher/start.sh"
echo "/usr/local/sms/sms -u"
echo "/usr/local/slivegateway/slivegateway -c /usr/local/slivegateway/slivegateway0.yaml"
echo "/usr/local/slivegateway/slivegateway -c /usr/local/slivegateway/slivegateway.yaml"
