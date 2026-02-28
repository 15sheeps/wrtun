#!/bin/bash

GOOS=linux GOARCH=arm64 go build -o wrtun-linux-arm64 ./cmd/main
GOOS=linux GOARCH=amd64 go build -o wrtun-linux-amd64 ./cmd/main

expect <<EOF
spawn scp -P 2222 wrtun-linux-arm64 vladimir@localhost:~/Desktop
expect "password:"
send "toor\r"
expect eof
EOF

echo "SCP transfer complete"