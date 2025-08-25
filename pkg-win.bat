@echo off

echo Building for Windows...
go build -o bin\lf-file-transfer-windows-amd64.exe main\main.go

echo Building for Mac (Intel)...
set GOOS=darwin
set GOARCH=amd64
go build -o bin\lf-file-transfer-darwin-amd64 main\main.go

echo Building for Mac (Apple Silicon)...
set GOOS=darwin
set GOARCH=arm64
go build -o bin\lf-file-transfer-darwin-arm64 main\main.go

echo Building for Linux...
set GOOS=linux
set GOARCH=amd64
go build -o bin\lf-file-transfer-linux-amd64 main\main.go

echo All builds completed!
