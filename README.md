# LF Open File Transfer

一个基于Go语言开发的P2P文件和文字传输工具，支持内网和公网环境下的实时文字聊天和大文件传输。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## 目录

- [功能特性](#功能特性)
- [技术架构](#技术架构)
- [安装与运行](#安装与运行)
- [使用说明](#使用说明)
- [API接口](#api接口)
- [项目结构](#项目结构)
- [跨平台构建](#跨平台构建)
- [许可证](#许可证)

## 功能特性

- **实时文字传输**: 支持多用户实时文字聊天
- **大文件传输**: 支持传输任意大小的文件（仅受限于磁盘空间）
- **断点续传**: 支持大文件分块传输和断点续传功能
- **P2P传输**: 文件在连接的客户端之间同步传输
- **临时存储**: 文件在服务器上临时存储，当所有客户端断开连接后自动删除
- **拖拽上传**: 支持拖拽文件到页面进行上传
- **多文件支持**: 可同时传输多个文件
- **进度显示**: 实时显示文件传输进度

## 技术架构

### 后端技术栈

- [Go](https://golang.org/) - 核心编程语言
- [Gin](https://github.com/gin-gonic/gin) - Web框架
- [Gorilla WebSocket](https://github.com/gorilla/websocket) - WebSocket支持
- [Google UUID](https://github.com/google/uuid) - UUID生成

### 前端技术栈

- HTML/CSS/JavaScript - 前端界面
- WebSocket - 实时通信
- 原生JavaScript - 无前端框架依赖

## 安装与运行

### 环境要求

- Go 1.19 或更高版本

### 安装步骤

1. 克隆项目代码:
   ```bash
   git clone https://github.com/yourname/lf-open-file-transfer.git
   cd lf-open-file-transfer
   ```

2. 安装依赖:
   ```bash
   go mod tidy
   ```

3. 运行项目:
   ```bash
   go run main.go
   ```

4. 访问应用:
   打开浏览器访问 `http://localhost:9555`

### 构建可执行文件

```bash
go build -o lf-file-transfer main.go
./lf-file-transfer
```

## 使用说明

### 主界面

应用启动后，访问 `http://localhost:9555` 进入主界面，可以选择两种传输模式：

1. **文字传输**: 实时文字聊天功能
2. **文件传输**: 文件传输功能

### 文字传输

1. 点击"文字传输"标签页
2. 系统会自动生成一个共享链接
3. 将链接分享给其他人
4. 多人可以同时在同一个会话中进行文字聊天

### 文件传输

1. 点击"文件传输"标签页
2. 系统会自动生成一个共享链接
3. 将链接分享给其他人
4. 任一用户可以通过以下方式上传文件：
   - 点击上传区域选择文件
   - 拖拽文件到上传区域
5. 文件会自动同步给所有连接到同一会话的用户
6. 接收方可以下载传输的文件

### 断点续传

系统支持大文件的断点续传功能：
- 文件会自动分块传输
- 如果传输中断，可以从中断处继续传输
- 提高大文件传输的可靠性

## API接口

### WebSocket接口

- `ws://localhost:9555/ws/:sessionID` - WebSocket连接端点

### HTTP API

- `POST /api/session` - 创建新会话
- `GET /api/session/:sessionID/history` - 获取会话历史
- `POST /api/upload/start` - 开始断点续传
- `POST /api/upload/chunk` - 上传文件块
- `GET /api/upload/status/:sessionID/:fileName` - 获取上传状态
- `POST /api/upload/complete/:sessionID/:fileName` - 完成上传

## 项目结构lf

```
lf-open-file-transfer/
├── main.go           # 程序入口文件
├── public/               # 前端资源目录
│   ├── static/           # 静态资源
│   │   ├── css/          # 样式文件
│   │   └── js/           # JavaScript文件
│   └── templates/        # HTML模板文件
├── temp/                 # 临时文件存储目录
├── go.mod               # Go模块文件
├── go.sum               # Go模块校验文件
└── README.md            # 项目说明文件
```

## 跨平台构建

本项目使用Go语言开发，可以轻松构建不同操作系统的可执行文件。

### 构建Mac、Linux和Windows可执行文件

在任何支持Go的平台上，你可以构建针对其他平台的可执行文件。

#### 为Windows构建可执行文件:

```bash
GOOS=windows GOARCH=amd64 go build -o lf-file-transfer-windows-amd64.exe main.go
```

#### 为Mac构建可执行文件:

```bash
# Intel芯片Mac
GOOS=darwin GOARCH=amd64 go build -o lf-file-transfer-darwin-amd64 main.go

# Apple Silicon (M1/M2)芯片Mac
GOOS=darwin GOARCH=arm64 go build -o lf-file-transfer-darwin-arm64 main.go
```

#### 为Linux构建可执行文件:

```bash
# 64位Linux
GOOS=linux GOARCH=amd64 go build -o lf-file-transfer-linux-amd64 main.go

# ARM架构Linux
GOOS=linux GOARCH=arm64 go build -o lf-file-transfer-linux-arm64 main.go
```

### 批量构建脚本

你也可以创建一个简单的脚本来批量构建所有平台的可执行文件：

#### build.sh (Linux/Mac):
```bash
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
```

#### build.bat (Windows):
```batch
@echo off

echo Building for Windows...
go build -o bin\lf-file-transfer-windows-amd64.exe main.go

echo Building for Mac (Intel)...
set GOOS=darwin
set GOARCH=amd64
go build -o bin\lf-file-transfer-darwin-amd64 main.go

echo Building for Mac (Apple Silicon)...
set GOOS=darwin
set GOARCH=arm64
go build -o bin\lf-file-transfer-darwin-arm64 main.go

echo Building for Linux...
set GOOS=linux
set GOARCH=amd64
go build -o bin\lf-file-transfer-linux-amd64 main.go

echo All builds completed!
```

### 运行构建的可执行文件

构建完成后，将可执行文件与[public](file:///E:/idea-project/lf-open-file-transfer/public)目录一起打包发布即可。用户只需要运行对应平台的可执行文件，无需安装Go环境。

例如，在Linux上运行：
```bash
./lf-file-transfer-linux-amd64
```

在Windows上运行：
```cmd
lf-file-transfer-windows-amd64.exe
```

在Mac上运行：
```bash
./lf-file-transfer-darwin-amd64
```

## 许可证

本项目采用MIT许可证，详情请见[LICENSE](LICENSE)文件。
