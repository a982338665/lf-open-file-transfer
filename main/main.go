package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 会话存储
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// Session 会话
type Session struct {
	ID             string
	Clients        map[*Client]bool
	TextContent    string
	FileInfo       *FileInfo
	ReceivedFiles  map[string]*FileInfo      // 添加已接收文件映射，支持多个文件
	ReceivingFiles map[string]*ReceivingFile // 添加正在接收的文件映射
	mu             sync.RWMutex
}

// Client 客户端连接
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// FileInfo 文件信息
type FileInfo struct {
	Name         string   `json:"name"`
	Size         int64    `json:"size"`
	Data         []byte   `json:"data"`
	Chunks       [][]byte `json:"chunks,omitempty"`       // 添加分块数据字段
	TotalChunks  int      `json:"totalChunks,omitempty"`  // 总块数
	CurrentChunk int      `json:"currentChunk,omitempty"` // 当前块索引
	TempFilePath string   `json:"tempFilePath,omitempty"` // 临时文件路径
}

// Message 消息结构
type Message struct {
	Type         string      `json:"type"`
	Content      string      `json:"content,omitempty"`
	Name         string      `json:"name,omitempty"`
	Size         int64       `json:"size,omitempty"`
	Data         interface{} `json:"data,omitempty"`
	SessionID    string      `json:"sessionID"`
	Timestamp    time.Time   `json:"timestamp"`
	Clients      int         `json:"clients,omitempty"`      // 添加在线人数字段
	Chunks       [][]byte    `json:"chunks,omitempty"`       // 添加分块数据字段
	TotalChunks  int         `json:"totalChunks,omitempty"`  // 总块数
	CurrentChunk int         `json:"currentChunk,omitempty"` // 当前块索引
	IsLastChunk  bool        `json:"isLastChunk,omitempty"`  // 是否为最后一块
	TempFilePath string      `json:"tempFilePath,omitempty"` // 临时文件路径
}

// 添加一个用于存储正在接收的文件块的结构
type ReceivingFile struct {
	Name           string
	Size           int64
	Chunks         map[int][]byte
	TotalChunks    int
	ReceivedChunks int
	TempFile       *os.File // 临时文件句柄
}

// 断点续传文件配置
type ResumableFileConfig struct {
	FileName     string                `json:"fileName"`
	FileSize     int64                 `json:"fileSize"`
	FileHash     string                `json:"fileHash"`
	ChunkSize    int64                 `json:"chunkSize"`
	TotalChunks  int                   `json:"totalChunks"`
	Chunks       map[string]*ChunkInfo `json:"chunks"`
	TempFilePath string                `json:"tempFilePath"`
	CreatedAt    time.Time             `json:"createdAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	mu           sync.RWMutex          `json:"-"`
}

// 分片信息
type ChunkInfo struct {
	ChunkIndex int    `json:"chunkIndex"`
	Size       int64  `json:"size"`
	Hash       string `json:"hash"`
	Completed  bool   `json:"completed"`
	Offset     int64  `json:"offset"`
}

// 上传开始请求
type UploadStartRequest struct {
	SessionID string `json:"sessionID" binding:"required"`
	FileName  string `json:"fileName" binding:"required"`
	FileSize  int64  `json:"fileSize" binding:"required"`
	FileHash  string `json:"fileHash"`
}

// 上传开始响应
type UploadStartResponse struct {
	UploadID      string `json:"uploadID"`
	ChunkSize     int64  `json:"chunkSize"`
	TotalChunks   int    `json:"totalChunks"`
	MissingChunks []int  `json:"missingChunks"`
	ConfigPath    string `json:"configPath"`
}

// 上传状态查询响应
type UploadStatusResponse struct {
	UploadID        string  `json:"uploadID"`
	FileName        string  `json:"fileName"`
	FileSize        int64   `json:"fileSize"`
	ChunkSize       int64   `json:"chunkSize"`
	TotalChunks     int     `json:"totalChunks"`
	CompletedChunks int     `json:"completedChunks"`
	MissingChunks   []int   `json:"missingChunks"`
	Progress        float64 `json:"progress"`
	Completed       bool    `json:"completed"`
}

// 分片上传响应
type ChunkUploadResponse struct {
	ChunkIndex    int     `json:"chunkIndex"`
	Completed     bool    `json:"completed"`
	Progress      float64 `json:"progress"`
	MissingChunks []int   `json:"missingChunks,omitempty"`
}

// 定义最大文件大小限制 (100GB)
const MaxFileSize = 100 * 1024 * 1024 * 1024 // 100GB

// 定义块大小
const ChunkSize = 1024 * 1024 * 5 // 5MB

// 临时文件目录
const TempDir = "../temp"

// 配置文件目录（与临时文件在同一目录）
const ConfigDir = "../temp"

// 全局文件写入锁，确保并发安全
var fileLocks = sync.Map{}

var store = &SessionStore{
	sessions: make(map[string]*Session),
}

func main() {
	r := gin.Default()

	// 创建临时目录（配置文件也存放在此目录）
	err := os.MkdirAll(TempDir, 0755)
	if err != nil {
		log.Fatal("无法创建临时目录:", err)
	}

	// 启动定期清理temp目录的goroutine
	go cleanupTempDir()

	// 设置静态文件服务
	r.Static("/static", "../public/static")
	r.LoadHTMLGlob("../public/templates/*")

	// 添加下载临时文件的路由
	r.GET("/download/:sessionID/:filename", func(c *gin.Context) {
		sessionID := c.Param("sessionID")
		filename := c.Param("filename")

		log.Printf("收到文件下载请求: 会话ID=%s, 文件名=%s", sessionID, filename)

		// 获取会话
		session := store.GetOrCreateSession(sessionID)
		session.mu.RLock()

		// 打印所有已接收的文件信息用于调试
		log.Printf("会话 %s 中已接收的文件列表:", sessionID)
		for name, info := range session.ReceivedFiles {
			log.Printf("  文件: %s, 大小: %d, 路径: %s", name, info.Size, info.TempFilePath)
		}

		// 从已接收文件列表中查找文件
		fileInfo, exists := session.ReceivedFiles[filename]
		session.mu.RUnlock()

		if !exists {
			log.Printf("文件未找到: %s", filename)
			c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
			return
		}

		log.Printf("找到文件信息: 名称=%s, 大小=%d, 路径=%s", fileInfo.Name, fileInfo.Size, fileInfo.TempFilePath)

		// 检查文件是否存在
		if _, err := os.Stat(fileInfo.TempFilePath); os.IsNotExist(err) {
			log.Printf("文件在磁盘上不存在: %s", fileInfo.TempFilePath)
			c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
			return
		}

		// 设置响应头
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Transfer-Encoding", "binary")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Header("Content-Type", "application/octet-stream")
		log.Printf("下载文件路径: " + fileInfo.TempFilePath)
		// 发送文件
		c.File(fileInfo.TempFilePath)
	})

	// 主页路由
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "P2P 文件和文字传输",
		})
	})

	// 文字传输页面
	r.GET("/text/:sessionID", func(c *gin.Context) {
		sessionID := c.Param("sessionID")
		c.HTML(http.StatusOK, "text.html", gin.H{
			"title":     "文字传输",
			"sessionID": sessionID,
		})
	})

	// 文件传输页面
	r.GET("/file/:sessionID", func(c *gin.Context) {
		sessionID := c.Param("sessionID")
		c.HTML(http.StatusOK, "file.html", gin.H{
			"title":     "文件传输",
			"sessionID": sessionID,
		})
	})

	// WebSocket端点
	r.GET("/ws/:sessionID", handleWebSocket)

	// API端点 - 创建会话
	r.POST("/api/session", createSession)

	// API端点 - 获取会话历史
	r.GET("/api/session/:sessionID/history", getSessionHistory)

	// 断点续传API端点
	r.POST("/api/upload/start", startResumableUpload)
	r.POST("/api/upload/chunk", uploadChunk)
	r.GET("/api/upload/status/:sessionID/:fileName", getUploadStatus)
	r.POST("/api/upload/complete/:sessionID/:fileName", completeUpload)

	port := ":9555"
	log.Printf("服务器启动在端口 %s...", port)
	if err := r.Run(port); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// 下载临时文件
func downloadTempFile(c *gin.Context) {
	sessionID := c.Param("sessionID")
	filename := c.Param("filename")

	session := store.GetOrCreateSession(sessionID)
	session.mu.RLock()

	// 从已接收文件列表中查找文件
	fileInfo, exists := session.ReceivedFiles[filename]
	session.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(fileInfo.TempFilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	// 设置响应头
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/octet-stream")
	log.Println("下载文件路径: " + fileInfo.TempFilePath)
	// 发送文件
	c.File(fileInfo.TempFilePath)
}

// 获取或创建会话
func (s *SessionStore) GetOrCreateSession(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		return session
	}

	session := &Session{
		ID:             sessionID,
		Clients:        make(map[*Client]bool),
		ReceivedFiles:  make(map[string]*FileInfo),      // 初始化已接收文件映射
		ReceivingFiles: make(map[string]*ReceivingFile), // 初始化正在接收的文件映射
	}
	s.sessions[sessionID] = session
	return session
}

// 删除客户端
func (s *SessionStore) RemoveClient(client *Client, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("尝试从会话 %s 删除客户端", sessionID)
	if session, exists := s.sessions[sessionID]; exists {
		session.mu.Lock()
		defer session.mu.Unlock()
		_, clientExists := session.Clients[client]
		if !clientExists {
			log.Printf("客户端在会话 %s 中不存在", sessionID)
			return
		}

		delete(session.Clients, client)
		log.Printf("客户端从会话 %s 断开，剩余客户端数: %d", sessionID, len(session.Clients))

		// 如果会话没有客户端了，清理资源
		if len(session.Clients) == 0 {
			log.Printf("会话 %s 没有客户端连接，开始清理资源", sessionID)

			// 清理临时文件
			if session.FileInfo != nil && session.FileInfo.TempFilePath != "" {
				log.Printf("准备清理临时文件: %s", session.FileInfo.TempFilePath)
				// 检查文件是否存在再删除
				if _, err := os.Stat(session.FileInfo.TempFilePath); err == nil {
					err := os.Remove(session.FileInfo.TempFilePath)
					if err != nil {
						log.Printf("删除临时文件失败: %v", err)
					} else {
						log.Printf("已删除临时文件: %s", session.FileInfo.TempFilePath)
					}
				} else {
					log.Printf("临时文件不存在: %s", session.FileInfo.TempFilePath)
				}
				session.FileInfo.TempFilePath = ""
			} else {
				log.Printf("会话中没有需要清理的临时文件")
			}

			// 清理正在接收的文件
			log.Printf("准备清理正在接收的文件，数量: %d", len(session.ReceivingFiles))
			for name, receivingFile := range session.ReceivingFiles {
				log.Printf("清理正在接收的文件: %s", name)
				if receivingFile.TempFile != nil {
					receivingFile.TempFile.Close()
					log.Printf("已关闭正在接收的文件句柄: %s", name)
				}
				// 删除临时文件
				tempFileName := filepath.Join(TempDir, session.ID+"_"+receivingFile.Name)
				log.Printf("准备删除正在接收的临时文件: %s", tempFileName)
				if _, err := os.Stat(tempFileName); err == nil {
					err := os.Remove(tempFileName)
					if err != nil {
						log.Printf("删除正在接收的临时文件失败: %v", err)
					} else {
						log.Printf("已删除正在接收的临时文件: %s", tempFileName)
					}
				} else {
					log.Printf("正在接收的临时文件不存在: %s", tempFileName)
				}
			}
			// 清空接收文件映射
			session.ReceivingFiles = make(map[string]*ReceivingFile)

			// 清理已接收的文件
			log.Printf("准备清理已接收的文件，数量: %d", len(session.ReceivedFiles))
			for name, fileInfo := range session.ReceivedFiles {
				log.Printf("清理已接收的文件: %s", name)
				if fileInfo.TempFilePath != "" {
					log.Printf("准备删除已接收的临时文件: %s", fileInfo.TempFilePath)
					if _, err := os.Stat(fileInfo.TempFilePath); err == nil {
						err := os.Remove(fileInfo.TempFilePath)
						if err != nil {
							log.Printf("删除已接收的临时文件失败: %v", err)
						} else {
							log.Printf("已删除已接收的临时文件: %s", fileInfo.TempFilePath)
						}
					} else {
						log.Printf("已接收的临时文件不存在: %s", fileInfo.TempFilePath)
					}
				}
			}
			// 清空已接收文件映射
			session.ReceivedFiles = make(map[string]*FileInfo)

			// 清理断点续传配置文件
			log.Printf("准备清理断点续传配置文件")
			cleanupResumableConfigs(sessionID)

			log.Printf("会话 %s 资源清理完成", sessionID)
		} else {
			log.Printf("会话 %s 仍有 %d 个客户端连接，不执行清理", sessionID, len(session.Clients))
		}
	} else {
		log.Printf("会话 %s 不存在", sessionID)
	}
}

// 定期清理temp目录中的残留临时文件
func cleanupTempDir() {
	// 启动两个定时器：快速清理和常规清理
	fastTicker := time.NewTicker(5 * time.Minute) // 每5分钟检查孤立文件
	slowTicker := time.NewTicker(24 * time.Hour)  // 每24小时检查老文件
	defer fastTicker.Stop()
	defer slowTicker.Stop()

	for {
		select {
		case <-fastTicker.C:
			cleanupOrphanedFiles()
		case <-slowTicker.C:
			cleanupOldFiles()
		}
	}
}

// 清理孤立的文件（没有对应会话的文件）
func cleanupOrphanedFiles() {
	log.Println("开始清理孤立的临时文件")

	// 检查temp目录是否存在
	if _, err := os.Stat(TempDir); os.IsNotExist(err) {
		return
	}

	// 读取temp目录中的所有文件
	entries, err := os.ReadDir(TempDir)
	if err != nil {
		log.Printf("读取temp目录失败: %v", err)
		return
	}

	// 获取所有活跃的会话ID
	store.mu.RLock()
	activeSessions := make(map[string]bool)
	for sessionID := range store.sessions {
		activeSessions[sessionID] = true
	}
	store.mu.RUnlock()

	// 遍历所有文件
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()

		// 检查是否是会话相关文件
		if sessionID := extractSessionIDFromFileName(fileName); sessionID != "" {
			// 如果会话不存在，则文件是孤立的
			if !activeSessions[sessionID] {
				filePath := filepath.Join(TempDir, fileName)
				log.Printf("删除孤立文件: %s (会话 %s 不存在)", fileName, sessionID)

				if err := os.Remove(filePath); err != nil {
					log.Printf("删除孤立文件失败: %v", err)
				} else {
					log.Printf("已删除孤立文件: %s", fileName)
				}
			}
		}
	}
}

// 清理超过24小时的老文件
func cleanupOldFiles() {
	log.Println("开始定期清理超过24小时的临时文件")

	// 检查temp目录是否存在
	if _, err := os.Stat(TempDir); os.IsNotExist(err) {
		log.Printf("temp目录不存在: %s", TempDir)
		return
	}

	// 读取temp目录中的所有文件
	entries, err := os.ReadDir(TempDir)
	if err != nil {
		log.Printf("读取temp目录失败: %v", err)
		return
	}

	// 获取当前时间
	now := time.Now()

	// 遍历所有文件
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// 获取文件信息
		fileInfo, err := entry.Info()
		if err != nil {
			log.Printf("获取文件信息失败: %v", err)
			continue
		}

		// 检查文件是否超过24小时
		if now.Sub(fileInfo.ModTime()) > 24*time.Hour {
			filePath := filepath.Join(TempDir, entry.Name())
			log.Printf("删除超过24小时的临时文件: %s", filePath)

			if err := os.Remove(filePath); err != nil {
				log.Printf("删除临时文件失败: %v", err)
			} else {
				log.Printf("已删除临时文件: %s", filePath)
			}
		}
	}

	log.Println("定期清理temp目录完成")
}

// 从文件名中提取会话ID
func extractSessionIDFromFileName(fileName string) string {
	// 文件名格式：sessionID_filename 或 sessionID_filename.json
	parts := strings.Split(fileName, "_")
	if len(parts) >= 2 {
		// 验证第一部分是否是有效的UUID格式
		sessionID := parts[0]
		if len(sessionID) == 36 && strings.Count(sessionID, "-") == 4 {
			return sessionID
		}
	}
	return ""
}

// 处理WebSocket连接
func handleWebSocket(c *gin.Context) {
	sessionID := c.Param("sessionID")

	// 升级到WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Print("upgrade failed: ", err)
		return
	}

	// 设置WebSocket连接的读取限制为最大文件大小
	conn.SetReadLimit(MaxFileSize)

	// 创建客户端
	client := &Client{
		conn: conn,
		send: make(chan []byte, 1024), // 增加缓冲区大小以处理大文件
	}

	// 获取或创建会话
	session := store.GetOrCreateSession(sessionID)

	// 注册客户端到会话
	session.mu.Lock()
	session.Clients[client] = true
	log.Printf("客户端连接到会话 %s，当前客户端数: %d", sessionID, len(session.Clients))

	// 发送历史数据给新客户端
	if session.TextContent != "" {
		historyMsg := Message{
			Type:      "text",
			Content:   session.TextContent,
			SessionID: sessionID,
			Timestamp: time.Now(),
		}
		if data, err := json.Marshal(historyMsg); err == nil {
			client.send <- data
		}
	}

	// 发送历史文件数据给新客户端
	// 发送最新的单个文件历史数据（为了向后兼容）
	if session.FileInfo != nil {
		log.Printf("发送历史文件数据给新客户端，文件名: %s", session.FileInfo.Name)
		historyMsg := Message{
			Type:         "file",
			Name:         session.FileInfo.Name,
			Size:         session.FileInfo.Size,
			SessionID:    sessionID,
			Timestamp:    time.Now(),
			TempFilePath: session.FileInfo.TempFilePath,
		}

		// 如果有临时文件路径，通知客户端可以通过下载链接获取文件
		if session.FileInfo.TempFilePath != "" {
			historyMsg.Data = "文件已保存在服务器上，可通过下载链接获取"
		}

		if data, err := json.Marshal(historyMsg); err == nil {
			client.send <- data
			log.Printf("历史文件数据发送完成")
		} else {
			log.Printf("序列化历史文件数据失败: %v", err)
		}
	} else {
		log.Printf("会话中没有历史文件数据")
	}

	// 发送所有已接收文件的历史数据
	log.Printf("发送所有已接收文件的历史数据，文件数量: %d", len(session.ReceivedFiles))
	for _, fileInfo := range session.ReceivedFiles {
		log.Printf("发送已接收文件历史数据，文件名: %s, 大小: %d, 路径: %s", fileInfo.Name, fileInfo.Size, fileInfo.TempFilePath)
		historyMsg := Message{
			Type:         "file",
			Name:         fileInfo.Name,
			Size:         fileInfo.Size,
			SessionID:    sessionID,
			Timestamp:    time.Now(),
			TempFilePath: fileInfo.TempFilePath,
			Data:         "文件已保存在服务器上，可通过下载链接获取",
		}

		if data, err := json.Marshal(historyMsg); err == nil {
			client.send <- data
			log.Printf("已接收文件历史数据发送完成: %s", fileInfo.Name)
		} else {
			log.Printf("序列化已接收文件历史数据失败: %v", err)
		}
	}

	// 广播客户端数量更新
	broadcastClientsCount(session)
	session.mu.Unlock()

	// 启动客户端处理goroutine
	go client.writePump(sessionID)
	go client.readPump(sessionID)

	// 发送欢迎消息
	welcomeMsg := Message{
		Type:      "system",
		Content:   "已连接到会话",
		SessionID: sessionID,
		Timestamp: time.Now(),
	}
	if data, err := json.Marshal(welcomeMsg); err == nil {
		client.send <- data
	}
}

// writePump 处理向客户端写入消息
func (c *Client) writePump(sessionID string) {
	defer func() {
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				// 通道已关闭
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}
		}
	}
}

// readPump 处理从客户端读取消息
func (c *Client) readPump(sessionID string) {
	defer func() {
		store.RemoveClient(c, sessionID)
		c.conn.Close()

		// 获取会话并广播客户端数量更新
		session := store.GetOrCreateSession(sessionID)
		session.mu.Lock()
		broadcastClientsCount(session)
		session.mu.Unlock()
	}()

	// 设置读取限制为最大文件大小
	c.conn.SetReadLimit(MaxFileSize)

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		// 解析消息
		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Println("unmarshal failed:", err)
			continue
		}

		// 获取会话
		session := store.GetOrCreateSession(sessionID)
		session.mu.Lock()

		// 根据消息类型处理
		switch msg.Type {
		case "text":
			// 更新会话文字内容
			session.TextContent = msg.Content

			// 广播给所有客户端
			broadcastMessage(message, session)

		case "file":
			// 创建临时文件存储大文件
			tempFileName := filepath.Join(TempDir, sessionID+"_"+msg.Name)
			tempFile, err := os.Create(tempFileName)
			if err != nil {
				log.Printf("创建临时文件失败: %v", err)
				session.mu.Unlock()
				break
			}

			// 转换数据
			var data []byte
			if dataArray, ok := msg.Data.([]interface{}); ok {
				data = make([]byte, len(dataArray))
				for i, v := range dataArray {
					if val, ok := v.(float64); ok {
						data[i] = byte(val)
					}
				}
			}

			// 写入临时文件
			_, err = tempFile.Write(data)
			if err != nil {
				log.Printf("写入临时文件失败: %v", err)
				// 关闭文件并同步状态
				tempFile.Close()
				session.mu.Unlock()
				break
			}

			// 立即关闭文件，确保数据写入磁盘
			tempFile.Close()

			// 更新会话文件信息到已接收文件列表
			fileInfo := &FileInfo{
				Name:         msg.Name,
				Size:         msg.Size,
				TempFilePath: tempFileName,
			}
			session.ReceivedFiles[msg.Name] = fileInfo

			log.Printf("更新已接收文件列表: 文件名=%s, 大小=%d, 路径=%s",
				fileInfo.Name, fileInfo.Size, fileInfo.TempFilePath)

			// 构造完整文件消息并广播
			fullMsg := Message{
				Type:         "file",
				Name:         msg.Name,
				Size:         msg.Size,
				SessionID:    sessionID,
				Timestamp:    time.Now(),
				TempFilePath: tempFileName,
				Data:         "文件已保存在服务器上，可通过下载链接获取",
			}

			// 广播给所有客户端
			log.Printf("准备广播文件传输完成消息，会话ID: %s, 文件名: %s", sessionID, msg.Name)
			broadcastMessage(fullMsg, session)
			log.Printf("文件 %s 传输完成并广播给所有客户端", msg.Name)

		case "file_chunk":
			// 处理文件块 - 修复大文件处理逻辑
			// 获取或创建正在接收的文件
			receivingFile, exists := session.ReceivingFiles[msg.Name]
			if !exists {
				// 创建临时文件
				tempFileName := filepath.Join(TempDir, sessionID+"_"+msg.Name)
				tempFile, err := os.Create(tempFileName)
				if err != nil {
					log.Printf("创建临时文件失败: %v", err)
					session.mu.Unlock()
					break
				}
				// 预先分配文件空间
				if err := tempFile.Truncate(msg.Size); err != nil {
					log.Printf("预分配文件空间失败: %v", err)
					tempFile.Close()
					session.mu.Unlock()
					break
				}

				receivingFile = &ReceivingFile{
					Name:           msg.Name,
					Size:           msg.Size,
					Chunks:         make(map[int][]byte),
					TotalChunks:    msg.TotalChunks,
					ReceivedChunks: 0,
					TempFile:       tempFile,
				}
				session.ReceivingFiles[msg.Name] = receivingFile
				log.Printf("开始接收文件块: %s, 总块数: %d, 文件大小: %d", msg.Name, msg.TotalChunks, msg.Size)
			} else {
				// 如果已经存在但TotalChunks为0，则更新它
				if receivingFile.TotalChunks == 0 && msg.TotalChunks > 0 {
					receivingFile.TotalChunks = msg.TotalChunks
					log.Printf("更新文件 %s 的总块数为: %d", msg.Name, msg.TotalChunks)
				}
			}

			// 转换数据
			var data []byte
			if dataArray, ok := msg.Data.([]interface{}); ok {
				data = make([]byte, len(dataArray))
				for i, v := range dataArray {
					if val, ok := v.(float64); ok {
						data[i] = byte(val)
					}
				}
			}

			// 存储文件块
			receivingFile.Chunks[msg.CurrentChunk] = data
			receivingFile.ReceivedChunks++
			log.Printf("接收文件块: %s, 当前块: %d, 已接收: %d/%d",
				msg.Name, msg.CurrentChunk, receivingFile.ReceivedChunks, receivingFile.TotalChunks)

			// 直接写入临时文件（按顺序）
			if msg.CurrentChunk == 0 {
				// 第一块直接写入文件开始位置
				log.Printf("开始写入第 %d 块到位置 0，块大小: %d", msg.CurrentChunk, len(data))
				_, err := receivingFile.TempFile.WriteAt(data, 0)
				if err != nil {
					log.Printf("写入文件块失败: %v", err)
					// 关闭文件并同步状态
					receivingFile.TempFile.Close()
					session.mu.Unlock()
					break
				}
				// 立即同步到磁盘
				receivingFile.TempFile.Sync()
				log.Printf("完成写入第 %d 块到位置 0，块大小: %d", msg.CurrentChunk, len(data))
			} else {
				// 其他块写入对应位置
				offset := int64(msg.CurrentChunk) * int64(ChunkSize)
				log.Printf("开始写入第 %d 块到位置 %d，块大小: %d", msg.CurrentChunk, offset, len(data))
				_, err := receivingFile.TempFile.WriteAt(data, offset)
				if err != nil {
					log.Printf("写入文件块失败: %v", err)
					// 关闭文件并同步状态
					receivingFile.TempFile.Close()
					session.mu.Unlock()
					break
				}
				// 立即同步到磁盘
				receivingFile.TempFile.Sync()
				log.Printf("完成写入第 %d 块到位置 %d，块大小: %d", msg.CurrentChunk, offset, len(data))
			}

			// 如果接收完所有块
			// 检查是否接收了所有预期的块，即使TotalChunks未正确设置
			allChunksReceived := false
			if receivingFile.TotalChunks > 0 {
				// 标准情况：TotalChunks已设置
				allChunksReceived = (receivingFile.ReceivedChunks == receivingFile.TotalChunks)
			} else {
				// 特殊情况：TotalChunks未设置，根据文件大小计算
				expectedChunks := int((receivingFile.Size + ChunkSize - 1) / ChunkSize)
				allChunksReceived = (receivingFile.ReceivedChunks == expectedChunks)
				log.Printf("TotalChunks未设置，根据文件大小计算预期块数: %d, 已接收: %d",
					expectedChunks, receivingFile.ReceivedChunks)
			}

			// 如果接收完所有块
			if allChunksReceived && receivingFile.ReceivedChunks > 0 {
				log.Printf("文件 %s 接收完成，准备关闭文件", msg.Name)
				// 确保所有数据都写入磁盘
				receivingFile.TempFile.Sync()
				log.Printf("文件 %s 数据同步完成，准备关闭文件句柄", msg.Name)
				// 关闭临时文件
				receivingFile.TempFile.Close()
				log.Printf("文件 %s 文件句柄已关闭", msg.Name)

				// 检查文件大小是否正确
				filePath := filepath.Join(TempDir, sessionID+"_"+msg.Name)
				if stat, err := os.Stat(filePath); err == nil {
					log.Printf("文件 %s 传输完成，预期大小: %d, 实际大小: %d",
						msg.Name, receivingFile.Size, stat.Size())
					if stat.Size() != receivingFile.Size {
						log.Printf("警告: 文件大小不匹配，期望: %d, 实际: %d", receivingFile.Size, stat.Size())
					}
				}

				// 更新会话文件信息到已接收文件列表
				fileInfo := &FileInfo{
					Name:         receivingFile.Name,
					Size:         receivingFile.Size,
					TempFilePath: filepath.Join(TempDir, sessionID+"_"+receivingFile.Name),
				}
				session.ReceivedFiles[receivingFile.Name] = fileInfo

				log.Printf("更新已接收文件列表: 文件名=%s, 大小=%d, 路径=%s",
					fileInfo.Name, fileInfo.Size, fileInfo.TempFilePath)

				// 构造完整文件消息并广播
				fullMsg := Message{
					Type:         "file",
					Name:         receivingFile.Name,
					Size:         receivingFile.Size,
					SessionID:    sessionID,
					Timestamp:    time.Now(),
					TempFilePath: filepath.Join(TempDir, sessionID+"_"+receivingFile.Name),
					Data:         "文件已保存在服务器上，可通过下载链接获取",
				}

				// 广播给所有客户端
				log.Printf("准备广播文件传输完成消息，会话ID: %s, 文件名: %s", sessionID, receivingFile.Name)
				broadcastMessage(fullMsg, session)
				log.Printf("文件 %s 传输完成并广播给所有客户端", receivingFile.Name)

				// 清理接收中的文件
				delete(session.ReceivingFiles, receivingFile.Name)
				log.Printf("已清理接收中的文件: %s", receivingFile.Name)
			}
		}

		session.mu.Unlock()
	}
}

// 广播消息给会话中的所有客户端
func broadcastMessage(message interface{}, session *Session) {
	var data []byte
	var err error

	// 检查message是否已经是[]byte
	switch msg := message.(type) {
	case []byte:
		data = msg
	default:
		// 否则进行序列化
		data, err = json.Marshal(message)
		if err != nil {
			log.Println("marshal failed:", err)
			return
		}
	}

	clientCount := len(session.Clients)
	if clientCount == 0 {
		log.Println("警告: 尝试向没有客户端的会话广播消息")
		return
	}

	successCount := 0
	for client := range session.Clients {
		select {
		case client.send <- data:
			successCount++
		default:
			log.Println("警告: 无法向客户端发送消息，关闭连接")
			close(client.send)
			delete(session.Clients, client)
		}
	}

	log.Printf("消息广播完成，成功发送给 %d/%d 个客户端", successCount, clientCount)
}

// 广播客户端数量给会话中的所有客户端
func broadcastClientsCount(session *Session) {
	clientsCount := len(session.Clients)

	// 创建客户端数量消息
	clientsMsg := Message{
		Type:    "clients",
		Clients: clientsCount,
	}

	// 将消息转换为JSON格式
	if data, err := json.Marshal(clientsMsg); err == nil {
		// 广播给所有客户端
		broadcastMessage(data, session)
	}
}

// 创建会话API
func createSession(c *gin.Context) {
	var req struct {
		Type      string `json:"type"`      // "text" 或 "file"
		SessionID string `json:"sessionID"` // 可选的自定义会话ID
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 使用自定义会话ID或生成新的UUID
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateUUID()
	}

	c.JSON(http.StatusOK, gin.H{
		"sessionID": sessionID,
		"url":       req.Type + "/" + sessionID,
	})
}

// 获取会话历史API
func getSessionHistory(c *gin.Context) {
	sessionID := c.Param("sessionID")

	session := store.GetOrCreateSession(sessionID)
	session.mu.RLock()
	defer session.mu.RUnlock()

	history := make(map[string]interface{})

	if session.TextContent != "" {
		history["textContent"] = session.TextContent
	}

	if session.FileInfo != nil {
		history["fileInfo"] = session.FileInfo
	}

	c.JSON(http.StatusOK, history)
}

// 开始断点续传上传
func startResumableUpload(c *gin.Context) {
	var req UploadStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查文件是否已经存在于会话中
	session := store.GetOrCreateSession(req.SessionID)
	session.mu.RLock()
	if existingFile, exists := session.ReceivedFiles[req.FileName]; exists {
		session.mu.RUnlock()
		log.Printf("文件 %s 已经存在于会话中，返回已完成状态", req.FileName)
		c.JSON(http.StatusOK, gin.H{
			"uploadID": req.SessionID, // 使用sessionID作为uploadID
			"config": map[string]interface{}{
				"fileName":     req.FileName,
				"fileSize":     existingFile.Size,
				"totalChunks":  1,
				"chunkSize":    existingFile.Size,
				"chunks":       map[string]interface{}{"0": map[string]interface{}{"completed": true}},
				"tempFilePath": existingFile.TempFilePath,
			},
			"progress":      100,
			"missingChunks": []int{},
			"completed":     true,
		})
		return
	}
	session.mu.RUnlock()

	// 生成上传ID
	uploadID := generateUUID()

	// 计算总分片数
	totalChunks := int((req.FileSize + ChunkSize - 1) / ChunkSize)

	// 添加详细的文件信息日志
	log.Printf("开始处理文件: %s", req.FileName)
	log.Printf("文件大小: %d 字节 (%.2f GB)", req.FileSize, float64(req.FileSize)/1024/1024/1024)
	log.Printf("分片大小: %d 字节", ChunkSize)
	log.Printf("计算分片数: %d", totalChunks)

	// 创建配置文件
	config := &ResumableFileConfig{
		FileName:     req.FileName,
		FileSize:     req.FileSize,
		FileHash:     req.FileHash,
		ChunkSize:    ChunkSize,
		TotalChunks:  totalChunks,
		Chunks:       make(map[string]*ChunkInfo),
		TempFilePath: filepath.Join(TempDir, req.SessionID+"_"+req.FileName),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// 初始化分片信息
	for i := 0; i < totalChunks; i++ {
		var chunkSize int64 = ChunkSize
		if i == totalChunks-1 {
			// 最后一个分片可能小于标准大小
			chunkSize = req.FileSize - int64(i)*ChunkSize
		}

		config.Chunks[strconv.Itoa(i)] = &ChunkInfo{
			ChunkIndex: i,
			Size:       chunkSize,
			Hash:       "",
			Completed:  false,
			Offset:     int64(i) * ChunkSize,
		}
	}

	// 保存配置文件（使用sessionID保持与源文件一致）
	configPath := filepath.Join(ConfigDir, req.SessionID+"_"+req.FileName+".json")
	if err := saveResumableConfig(configPath, config); err != nil {
		log.Printf("保存配置文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置文件失败"})
		return
	}

	// 创建临时文件并预分配空间
	tempFile, err := os.Create(config.TempFilePath)
	if err != nil {
		log.Printf("创建临时文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建临时文件失败"})
		return
	}

	// 预分配文件空间
	if err := tempFile.Truncate(req.FileSize); err != nil {
		log.Printf("预分配文件空间失败: %v", err)
		tempFile.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "预分配文件空间失败"})
		return
	}
	tempFile.Close()

	// 获取缺失的分片列表
	missingChunks := make([]int, 0, totalChunks)
	for i := 0; i < totalChunks; i++ {
		missingChunks = append(missingChunks, i)
	}

	response := UploadStartResponse{
		UploadID:      uploadID,
		ChunkSize:     ChunkSize,
		TotalChunks:   totalChunks,
		MissingChunks: missingChunks,
		ConfigPath:    configPath,
	}

	c.JSON(http.StatusOK, response)
}

// 上传分片
func uploadChunk(c *gin.Context) {
	// 获取表单数据
	sessionID := c.PostForm("sessionID")
	fileName := c.PostForm("fileName")
	chunkIndexStr := c.PostForm("chunkIndex")
	uploadID := c.PostForm("uploadID")

	if sessionID == "" || fileName == "" || chunkIndexStr == "" || uploadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分片索引无效"})
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("chunk")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "获取分片文件失败"})
		return
	}

	// 读取配置文件（使用sessionID）
	configPath := filepath.Join(ConfigDir, sessionID+"_"+fileName+".json")

	// 获取配置文件锁，确保并发安全
	configLockKey := configPath
	configLockValue, _ := fileLocks.LoadOrStore(configLockKey, &sync.RWMutex{})
	configLock := configLockValue.(*sync.RWMutex)

	configLock.Lock()
	defer configLock.Unlock()

	config, err := loadResumableConfig(configPath)
	if err != nil {
		log.Printf("加载配置文件失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "上传配置不存在"})
		return
	}

	// 检查分片是否已完成
	chunkKey := strconv.Itoa(chunkIndex)
	chunkInfo, exists := config.Chunks[chunkKey]
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分片索引超出范围"})
		return
	}

	if chunkInfo.Completed {
		// 分片已完成，返回成功
		response := ChunkUploadResponse{
			ChunkIndex: chunkIndex,
			Completed:  false,
			Progress:   calculateProgress(config),
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// 打开分片文件
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "打开分片文件失败"})
		return
	}
	defer src.Close()

	// 读取分片数据
	chunkData, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取分片数据失败"})
		return
	}

	// 验证分片大小
	if int64(len(chunkData)) != chunkInfo.Size {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分片大小不匹配"})
		return
	}

	// 计算分片哈希
	hash := md5.Sum(chunkData)
	chunkHash := hex.EncodeToString(hash[:])

	// 使用安全的分片写入函数
	if err := writeChunkSafely(config.TempFilePath, chunkData, chunkInfo.Offset); err != nil {
		log.Printf("写入分片失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入分片失败"})
		return
	}

	// 更新分片状态
	chunkInfo.Hash = chunkHash
	chunkInfo.Completed = true
	config.UpdatedAt = time.Now()

	// 保存配置文件
	if err := saveResumableConfig(configPath, config); err != nil {
		log.Printf("保存配置文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置文件失败"})
		return
	}

	// 检查是否所有分片都已完成
	allCompleted := true
	completedCount := 0
	for _, chunk := range config.Chunks {
		if chunk.Completed {
			completedCount++
		} else {
			allCompleted = false
		}
	}

	log.Printf("分片 %d 上传完成，总进度: %d/%d", chunkIndex, completedCount, config.TotalChunks)

	response := ChunkUploadResponse{
		ChunkIndex: chunkIndex,
		Completed:  allCompleted,
		Progress:   calculateProgress(config),
	}

	if allCompleted {
		// 所有分片完成，添加到会话的已接收文件列表
		session := store.GetOrCreateSession(sessionID)
		session.mu.Lock()
		session.ReceivedFiles[fileName] = &FileInfo{
			Name:         fileName,
			Size:         config.FileSize,
			TempFilePath: config.TempFilePath,
		}
		session.mu.Unlock()

		log.Printf("🎉 文件上传完成: %s (大小: %d 字节)", fileName, config.FileSize)

		// 上传完成后删除配置文件
		configPath := filepath.Join(ConfigDir, sessionID+"_"+fileName+".json")
		if err := os.Remove(configPath); err != nil {
			log.Printf("删除配置文件失败 %s: %v", configPath, err)
		} else {
			log.Printf("已删除配置文件: %s", configPath)
		}

		// 通过WebSocket通知接收端
		message := Message{
			Type:         "file",
			Content:      fmt.Sprintf("文件上传完成: %s", fileName),
			Name:         fileName,
			Size:         config.FileSize,
			SessionID:    sessionID,
			Timestamp:    time.Now(),
			TempFilePath: config.TempFilePath,
		}

		// 广播消息到会话中的所有客户端
		broadcastMessage(message, session)
	}

	c.JSON(http.StatusOK, response)
}

// 获取上传状态
func getUploadStatus(c *gin.Context) {
	sessionID := c.Param("sessionID")
	fileName := c.Param("fileName")

	if sessionID == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}

	// 读取配置文件（使用sessionID查找）
	configPath := filepath.Join(ConfigDir, sessionID+"_"+fileName+".json")
	config, err := loadResumableConfig(configPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上传配置不存在"})
		return
	}

	config.mu.RLock()
	defer config.mu.RUnlock()

	// 计算完成的分片数和缺失的分片
	completedChunks := 0
	missingChunks := make([]int, 0)

	for i := 0; i < config.TotalChunks; i++ {
		chunkKey := strconv.Itoa(i)
		if chunk, exists := config.Chunks[chunkKey]; exists && chunk.Completed {
			completedChunks++
		} else {
			missingChunks = append(missingChunks, i)
		}
	}

	allCompleted := completedChunks == config.TotalChunks

	response := UploadStatusResponse{
		UploadID:        sessionID, // 使用sessionID作为uploadID
		FileName:        config.FileName,
		FileSize:        config.FileSize,
		ChunkSize:       config.ChunkSize,
		TotalChunks:     config.TotalChunks,
		CompletedChunks: completedChunks,
		MissingChunks:   missingChunks,
		Progress:        calculateProgress(config),
		Completed:       allCompleted,
	}

	c.JSON(http.StatusOK, response)
}

// 完成上传
func completeUpload(c *gin.Context) {
	sessionID := c.Param("sessionID")
	fileName := c.Param("fileName")

	if sessionID == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}

	// 读取配置文件
	configPath := filepath.Join(ConfigDir, sessionID+"_"+fileName+".json")
	config, err := loadResumableConfig(configPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上传配置不存在"})
		return
	}

	// 检查文件是否已经在会话中存在（可能已经完成）
	session := store.GetOrCreateSession(sessionID)
	session.mu.RLock()
	if existingFile, exists := session.ReceivedFiles[fileName]; exists {
		session.mu.RUnlock()
		log.Printf("文件 %s 已经存在于会话中，直接返回成功", fileName)
		c.JSON(http.StatusOK, gin.H{
			"message":  "文件上传完成",
			"fileName": fileName,
			"fileSize": existingFile.Size,
		})
		return
	}
	session.mu.RUnlock()

	config.mu.Lock()
	defer config.mu.Unlock()

	// 验证所有分片都已完成
	incompleteChunks := 0
	for _, chunk := range config.Chunks {
		if !chunk.Completed {
			incompleteChunks++
		}
	}

	if incompleteChunks > 0 {
		log.Printf("文件 %s 还有 %d 个分片未完成上传", fileName, incompleteChunks)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":            fmt.Sprintf("还有 %d 个分片未完成上传", incompleteChunks),
			"incompleteChunks": incompleteChunks,
		})
		return
	}

	// 验证文件完整性（可选）
	if config.FileHash != "" {
		if err := verifyFileHash(config.TempFilePath, config.FileHash); err != nil {
			log.Printf("文件哈希验证失败: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "文件完整性验证失败"})
			return
		}
	}

	// 添加到会话的已接收文件列表
	session.mu.Lock()
	session.ReceivedFiles[fileName] = &FileInfo{
		Name:         fileName,
		Size:         config.FileSize,
		TempFilePath: config.TempFilePath,
	}
	session.mu.Unlock()

	log.Printf("文件上传完成并验证: %s", fileName)

	c.JSON(http.StatusOK, gin.H{
		"message":  "文件上传完成",
		"fileName": fileName,
		"fileSize": config.FileSize,
	})
}

// 保存断点续传配置文件
func saveResumableConfig(configPath string, config *ResumableFileConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// 加载断点续传配置文件
func loadResumableConfig(configPath string) (*ResumableFileConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config ResumableFileConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// 计算上传进度
func calculateProgress(config *ResumableFileConfig) float64 {
	if config.TotalChunks == 0 {
		return 0
	}

	completedChunks := 0
	for _, chunk := range config.Chunks {
		if chunk.Completed {
			completedChunks++
		}
	}

	return float64(completedChunks) / float64(config.TotalChunks) * 100
}

// 验证文件哈希
func verifyFileHash(filePath, expectedHash string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 根据哈希长度判断使用哪种算法
	var hasher interface {
		io.Writer
		Sum([]byte) []byte
	}
	if len(expectedHash) == 32 {
		// MD5 哈希长度为32
		hasher = md5.New()
	} else if len(expectedHash) == 64 {
		// SHA-256 哈希长度为64
		hasher = sha256.New()
	} else {
		return fmt.Errorf("不支持的哈希长度: %d", len(expectedHash))
	}

	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("文件哈希不匹配: 期望 %s, 实际 %s", expectedHash, actualHash)
	}

	return nil
}

// 安全的分片写入函数，支持并发
func writeChunkSafely(filePath string, data []byte, offset int64) error {
	// 获取文件锁
	lockKey := filePath
	lockValue, _ := fileLocks.LoadOrStore(lockKey, &sync.Mutex{})
	fileLock := lockValue.(*sync.Mutex)

	fileLock.Lock()
	defer fileLock.Unlock()

	// 打开文件进行写入
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	// 写入指定位置
	n, err := file.WriteAt(data, offset)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	if n != len(data) {
		return fmt.Errorf("写入数据不完整: 期望 %d 字节, 实际写入 %d 字节", len(data), n)
	}

	// 同步到磁盘
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步文件失败: %v", err)
	}

	return nil
}

// 验证分片完整性
func verifyChunkIntegrity(filePath string, offset int64, expectedSize int64, expectedHash string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	// 读取指定位置的数据
	data := make([]byte, expectedSize)
	n, err := file.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return fmt.Errorf("读取文件失败: %v", err)
	}

	if int64(n) != expectedSize {
		return fmt.Errorf("读取数据大小不匹配: 期望 %d 字节, 实际读取 %d 字节", expectedSize, n)
	}

	// 验证哈希
	if expectedHash != "" {
		hash := md5.Sum(data)
		actualHash := hex.EncodeToString(hash[:])
		if actualHash != expectedHash {
			return fmt.Errorf("分片哈希不匹配: 期望 %s, 实际 %s", expectedHash, actualHash)
		}
	}

	return nil
}

// 生成UUID
func generateUUID() string {
	newUUID := uuid.New()
	return newUUID.String()
}

// 清理断点续传配置文件和临时文件
func cleanupResumableConfigs(sessionID string) {
	// 读取temp目录
	files, err := os.ReadDir(TempDir)
	if err != nil {
		log.Printf("读取temp目录失败: %v", err)
		return
	}

	deletedCount := 0
	failedFiles := []string{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		filePath := filepath.Join(TempDir, fileName)

		// 检查是否是以会话ID开头的文件（包括源文件和配置文件）
		if len(fileName) > len(sessionID) && fileName[:len(sessionID)] == sessionID {
			// 删除文件
			if err := os.Remove(filePath); err != nil {
				log.Printf("删除会话文件失败 %s: %v", fileName, err)
				failedFiles = append(failedFiles, fileName)
			} else {
				log.Printf("已删除会话文件: %s", fileName)
				deletedCount++
			}
		}
	}

	log.Printf("清理会话相关文件完成，删除了 %d 个文件", deletedCount)

	// 如果有文件删除失败，启动延迟清理
	if len(failedFiles) > 0 {
		log.Printf("有 %d 个文件删除失败，将在5秒后重试", len(failedFiles))
		go delayedCleanup(sessionID, failedFiles)
	}
}

// 延迟清理失败的文件
func delayedCleanup(sessionID string, failedFiles []string) {
	time.Sleep(5 * time.Second)

	log.Printf("开始延迟清理会话 %s 的失败文件", sessionID)
	deletedCount := 0

	for _, fileName := range failedFiles {
		filePath := filepath.Join(TempDir, fileName)

		// 再次尝试删除
		if err := os.Remove(filePath); err != nil {
			log.Printf("延迟清理仍然失败 %s: %v", fileName, err)
		} else {
			log.Printf("延迟清理成功: %s", fileName)
			deletedCount++
		}
	}

	log.Printf("延迟清理完成，成功删除了 %d 个文件", deletedCount)
}
