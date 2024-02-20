#!/bin/bash

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
echo "ps -ef | grep -v grep | grep sms"
ps -ef | grep -v grep | grep -E "sms"
