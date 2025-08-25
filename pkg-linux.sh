#!/bin/bash

# 构建Windows版本
echo "Building for Windows..."
GOOS=windows GOARCH=amd64 go build -o bin/lf-file-transfer-windows-amd64.exe main.go

# 构建Mac版本 (Intel)
echo "Building for Mac (Intel)..."
GOOS=darwin GOARCH=amd64 go build -o bin/lf-file-transfer-darwin-amd64 main.go

# 构建Mac版本 (Apple Silicon)
echo "Building for Mac (Apple Silicon)..."
GOOS=darwin GOARCH=arm64 go build -o bin/lf-file-transfer-darwin-arm64 main.go

# 构建Linux版本
echo "Building for Linux..."
GOOS=linux GOARCH=amd64 go build -o bin/lf-file-transfer-linux-amd64 main.go

echo "All builds completed!"
