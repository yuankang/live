#!/bin/bash

go build -ldflags "-X main.CompileDate=`date +%Y%m%d%H%M%S`" livegateway
mv livegateway slivegateway

pid=`ps -ef | grep "slivegateway.*-c" | grep -v grep | grep -v vi | awk '{print $2}'`
echo "kill -9 $pid"
kill -9 $pid

echo "======================================"
mkdir -p /usr/local/slivegateway/
/usr/local/slivegateway/slivegateway -u
rm -rf /usr/local/slivegateway/slivegateway
rm -rf /usr/local/slivegateway/slivegateway*.log
rm -rf /usr/local/slivegateway/slivegateway.json
rm -rf /usr/local/slivegateway/streamlog
rm -rf /usr/local/slivegateway/record
cp slivegateway /usr/local/slivegateway/slivegateway
cp slivegateway.json /usr/local/slivegateway/slivegateway.json
/usr/local/slivegateway/slivegateway -d

sleep 1s
echo "ps -ef | grep -v grep | grep slivegateway"
ps -ef | grep -v grep | grep -E "slivegateway|sliveserver"
