// 信号量类，用于控制并发
class Semaphore {
    constructor(maxConcurrency) {
        this.maxConcurrency = maxConcurrency;
        this.currentConcurrency = 0;
        this.queue = [];
    }

    async acquire() {
        return new Promise((resolve) => {
            if (this.currentConcurrency < this.maxConcurrency) {
                this.currentConcurrency++;
                resolve(() => this.release());
            } else {
                this.queue.push(() => {
                    this.currentConcurrency++;
                    resolve(() => this.release());
                });
            }
        });
    }

    release() {
        this.currentConcurrency--;
        if (this.queue.length > 0) {
            const next = this.queue.shift();
            next();
        }
    }
}

// 优化的断点续传文件上传管理器
class ResumableUploadManager {
    constructor(sessionID, webSocket) {
        this.sessionID = sessionID;
        this.webSocket = webSocket;
        this.uploads = new Map(); // 存储上传状态
        this.chunkSize = 1024 * 1024 * 5; // 5MB
        this.maxRetries = 5; // 增加重试次数
        this.retryDelay = 1000; // 1秒
        this.maxConcurrency = 3; // 最大并发数
        this.uploadQueue = []; // 上传队列
        this.activeUploads = 0; // 当前活跃上传数

        // 进度持久化
        this.storageKey = `resumable_uploads_${sessionID}`;
        this.loadPersistedUploads();
    }

    // 开始文件上传
    async startUpload(file) {
        const fileId = this.generateFileId(file);

        // 检查是否已有上传记录
        let uploadState = this.uploads.get(fileId);
        let isNewUpload = !uploadState;

        if (isNewUpload) {
            // 添加详细的文件信息日志
            console.log(`开始处理文件: ${file.name}`);
            console.log(`文件大小: ${file.size} 字节 (${(file.size / 1024 / 1024 / 1024).toFixed(2)} GB)`);
            console.log(`分片大小: ${this.chunkSize} 字节`);
            console.log(`预计分片数: ${Math.ceil(file.size / this.chunkSize)}`);

            // 计算文件哈希
            const fileHash = await this.calculateFileHash(file);

            // 创建新的上传状态
            uploadState = {
                file: file,
                fileId: fileId,
                fileName: file.name,
                fileSize: file.size,
                fileHash: fileHash,
                totalChunks: Math.ceil(file.size / this.chunkSize),
                completedChunks: new Set(),
                failedChunks: new Set(),
                uploading: false,
                progress: 0,
                startTime: Date.now(),
                uploadID: null,
                missingChunks: [],
                retryCount: 0,
                speed: 0,
                eta: 0,
                completed: false
            };
            this.uploads.set(fileId, uploadState);
        } else {
            // 如果是重复上传，重置状态并重新检查服务端
            console.log(`检测到重复上传文件: ${file.name}，重新检查服务端状态`);
            uploadState.uploadID = null;
            uploadState.completed = false;
            uploadState.uploading = false;
            uploadState.progress = 0;
            uploadState.completedChunks.clear();
            uploadState.failedChunks.clear();
        }

        // 显示上传进度UI
        this.createProgressUI(uploadState);

        // 持久化上传状态
        this.persistUploadState();

        // 请求服务器开始上传
        await this.requestStartUpload(uploadState);
    }

    // 生成文件唯一ID
    generateFileId(file) {
        return `${file.name}_${file.size}_${file.lastModified}`.replace(/[^a-zA-Z0-9_]/g, '_');
    }

    // 计算文件哈希（简化版本）
    async calculateFileHash(file) {
        // 对于大文件（>1GB），跳过哈希计算以避免内存问题
        const maxHashSize = 1024 * 1024 * 1024; // 1GB
        if (file.size > maxHashSize) {
            console.log(`文件 ${file.name} 大小超过 1GB，跳过哈希计算`);
            return '';
        }

        // 检查是否支持Web Crypto API
        if (!window.crypto || !window.crypto.subtle) {
            console.log(`浏览器不支持Web Crypto API，跳过哈希计算`);
            return '';
        }

        return new Promise((resolve, reject) => {
            const reader = new FileReader();
            reader.onload = async (e) => {
                try {
                    const arrayBuffer = e.target.result;
                    // 使用SHA-256计算哈希
                    const hashBuffer = await crypto.subtle.digest('SHA-256', arrayBuffer);
                    const hashArray = Array.from(new Uint8Array(hashBuffer));
                    const hashHex = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
                    console.log(`文件哈希计算完成: ${file.name} -> ${hashHex.substring(0, 16)}...`);
                    resolve(hashHex);
                } catch (error) {
                    console.warn('无法计算文件哈希:', error);
                    resolve(''); // 返回空字符串，服务端会跳过哈希验证
                }
            };
            reader.onerror = () => {
                console.warn('读取文件失败，跳过哈希计算');
                resolve(''); // 返回空字符串而不是reject
            };

            // 对于小文件，直接跳过哈希计算以避免兼容性问题
            if (file.size < 1024) {
                console.log(`文件 ${file.name} 太小，跳过哈希计算`);
                resolve('');
                return;
            }

            reader.readAsArrayBuffer(file);
        });
    }

    // 加载持久化的上传状态
    loadPersistedUploads() {
        try {
            const stored = localStorage.getItem(this.storageKey);
            if (stored) {
                const uploads = JSON.parse(stored);
                for (const [fileId, state] of Object.entries(uploads)) {
                    // 恢复Set对象
                    state.completedChunks = new Set(state.completedChunks);
                    state.failedChunks = new Set(state.failedChunks);
                    this.uploads.set(fileId, state);
                }
            }
        } catch (error) {
            console.warn('加载持久化上传状态失败:', error);
        }
    }

    // 持久化上传状态
    persistUploadState() {
        try {
            const uploads = {};
            for (const [fileId, state] of this.uploads.entries()) {
                // 转换Set为数组以便序列化
                uploads[fileId] = {
                    ...state,
                    completedChunks: Array.from(state.completedChunks),
                    failedChunks: Array.from(state.failedChunks),
                    file: null // 不持久化文件对象
                };
            }
            localStorage.setItem(this.storageKey, JSON.stringify(uploads));
        } catch (error) {
            console.warn('持久化上传状态失败:', error);
        }
    }

    // 请求服务器开始上传
    async requestStartUpload(uploadState) {
        try {
            const response = await fetch('/api/upload/start', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    sessionID: this.sessionID,
                    fileName: uploadState.fileName,
                    fileSize: uploadState.fileSize,
                    fileHash: uploadState.fileHash
                })
            });

            const result = await response.json();
            console.log(`服务端响应:`, result);

            if (response.ok) {
                // 检查是否已经完成
                if (result.completed) {
                    console.log(`文件已经完成上传: ${uploadState.file.name}`);
                    uploadState.completed = true;

                    // 显示重复文件提示
                    this.showError(uploadState, `文件 "${uploadState.file.name}" 已经上传过了`);

                    // 更新UI状态为错误
                    const statusText = document.getElementById(`status-${uploadState.fileId}`);
                    if (statusText) {
                        statusText.textContent = '文件已存在';
                        statusText.style.color = 'red';
                    }

                    // 隐藏进度条
                    const progressBar = document.getElementById(`progress-${uploadState.fileId}`);
                    if (progressBar) {
                        progressBar.style.display = 'none';
                    }

                    // 从上传列表中移除
                    this.uploads.delete(uploadState.fileId);
                    this.persistUploadState();

                    return;
                }

                uploadState.uploadID = result.uploadID;
                uploadState.missingChunks = result.missingChunks || [];
                uploadState.totalChunks = result.totalChunks;

                console.log(`开始上传文件: ${uploadState.fileName}, 上传ID: ${uploadState.uploadID}, 缺失分片: ${uploadState.missingChunks.length}`);

                // 开始上传缺失的分片
                await this.uploadMissingChunks(uploadState);
            } else {
                throw new Error(result.error || '开始上传失败');
            }
        } catch (error) {
            console.error('请求开始上传失败:', error);
            this.showError(uploadState, error.message);
        }
    }

    // 通过WebSocket请求开始上传
    requestStartUploadViaWebSocket(uploadState) {
        const message = {
            type: 'file_start',
            name: uploadState.file.name,
            size: uploadState.file.size,
            sessionID: this.sessionID,
            timestamp: new Date()
        };

        this.webSocket.send(JSON.stringify(message));

        // 设置响应监听器
        this.setupWebSocketResponseListener(uploadState);
    }

    // 通过HTTP API请求开始上传
    async requestStartUploadViaAPI(uploadState) {
        const response = await fetch('/api/upload/start', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                sessionID: this.sessionID,
                fileName: uploadState.file.name,
                fileSize: uploadState.file.size,
                fileHash: '' // 可以添加文件哈希计算
            })
        });

        const result = await response.json();
        console.log(`服务端响应:`, result);

        if (response.ok) {
            this.handleStartUploadResponse(uploadState, result);
        } else {
            throw new Error(result.error || '开始上传失败');
        }
    }

    // 处理开始上传响应
    handleStartUploadResponse(uploadState, result) {
        uploadState.config = result.config;
        uploadState.progress = result.progress || 0;

        // 更新已完成的分片
        uploadState.completedChunks.clear();
        if (result.config && result.config.chunks) {
            for (const [index, chunk] of Object.entries(result.config.chunks)) {
                if (chunk.completed) {
                    uploadState.completedChunks.add(parseInt(index));
                }
            }
        }

        console.log(`文件上传开始: ${uploadState.file.name}, 进度: ${uploadState.progress}%`);
        this.updateProgressUI(uploadState);

        // 检查是否已经完成
        if (result.completed) {
            console.log(`文件已经完成上传: ${uploadState.file.name}`);
            uploadState.completed = true;

            // 显示重复文件提示
            this.showError(uploadState, `文件 "${uploadState.file.name}" 已经上传过了`);

            // 更新UI状态为错误
            const statusText = document.getElementById(`status-${uploadState.fileId}`);
            if (statusText) {
                statusText.textContent = '文件已存在';
                statusText.style.color = 'red';
            }

            // 隐藏进度条
            const progressBar = document.getElementById(`progress-${uploadState.fileId}`);
            if (progressBar) {
                progressBar.style.display = 'none';
            }

            // 从上传列表中移除
            this.uploads.delete(uploadState.fileId);
            this.persistUploadState();

            return;
        }

        // 开始上传缺失的分片
        this.uploadMissingChunks(uploadState, result.missingChunks || []);
    }

    // 设置WebSocket响应监听器
    setupWebSocketResponseListener(uploadState) {
        const originalOnMessage = this.webSocket.onmessage;

        this.webSocket.onmessage = (event) => {
            try {
                const message = JSON.parse(event.data);

                if (message.type === 'file_start_response' && message.name === uploadState.file.name) {
                    this.handleStartUploadResponse(uploadState, message.data);
                    // 恢复原始消息处理器
                    this.webSocket.onmessage = originalOnMessage;
                } else if (originalOnMessage) {
                    // 传递给原始处理器
                    originalOnMessage(event);
                }
            } catch (error) {
                console.error('解析WebSocket消息失败:', error);
                if (originalOnMessage) {
                    originalOnMessage(event);
                }
            }
        };
    }

    // 上传缺失的分片
    async uploadMissingChunks(uploadState) {
        if (uploadState.uploading) {
            return; // 已在上传中
        }

        uploadState.uploading = true;
        uploadState.startTime = Date.now();

        // 使用队列管理并发上传
        const chunks = [...uploadState.missingChunks];
        const uploadPromises = [];

        // 限制并发数
        const semaphore = new Semaphore(this.maxConcurrency);

        for (const chunkIndex of chunks) {
            if (!uploadState.uploading) break;

            const uploadPromise = semaphore.acquire().then(async (release) => {
                try {
                    await this.uploadChunk(uploadState, chunkIndex);
                } finally {
                    release();
                }
            });

            uploadPromises.push(uploadPromise);
        }

        try {
            await Promise.all(uploadPromises);

            // 检查是否所有分片都已完成
            console.log(`上传完成检查: ${uploadState.completedChunks.size}/${uploadState.totalChunks}, 已完成标志: ${uploadState.completed}`);

            if (uploadState.completedChunks.size === uploadState.totalChunks && !uploadState.completed) {
                // 所有分片完成
                uploadState.completed = true;
                console.log(`🎉 所有分片上传完成: ${uploadState.fileName}`);

                // 检查服务端是否已经处理了完成逻辑
                if (uploadState.serverCompleted) {
                    console.log(`服务端已处理完成逻辑，直接调用前端完成处理`);
                    this.onUploadComplete(uploadState);
                } else if (uploadState.totalChunks > 1) {
                    console.log(`调用完成API（多分片文件）`);
                    await this.completeUpload(uploadState);
                } else {
                    console.log(`单分片文件，无需调用完成API`);
                    this.onUploadComplete(uploadState);
                }
            } else if (uploadState.completedChunks.size < uploadState.totalChunks) {
                console.log(`还有 ${uploadState.totalChunks - uploadState.completedChunks.size} 个分片未完成`);
            }
        } catch (error) {
            console.error('分片上传失败:', error);
            this.showError(uploadState, error.message);
        } finally {
            uploadState.uploading = false;
            this.persistUploadState();
        }
    }

    // 上传单个分片
    async uploadChunk(uploadState, chunkIndex) {
        // 检查分片是否已完成
        if (uploadState.completedChunks.has(chunkIndex)) {
            return;
        }

        const start = chunkIndex * this.chunkSize;
        const end = Math.min(start + this.chunkSize, uploadState.file.size);
        const chunk = uploadState.file.slice(start, end);

        let retries = 0;
        const maxRetries = this.maxRetries;

        while (retries < maxRetries) {
            try {
                const startTime = Date.now();

                // 使用HTTP API上传分片
                await this.uploadChunkViaAPI(uploadState, chunkIndex, chunk);

                // 计算上传速度
                const endTime = Date.now();
                const duration = (endTime - startTime) / 1000; // 秒
                const speed = chunk.size / duration; // 字节/秒
                uploadState.speed = speed;

                uploadState.completedChunks.add(chunkIndex);
                uploadState.failedChunks.delete(chunkIndex);

                console.log(`分片 ${chunkIndex} 上传成功，速度: ${this.formatSpeed(speed)}，进度: ${uploadState.completedChunks.size}/${uploadState.totalChunks}`);
                this.updateProgressUI(uploadState);
                this.persistUploadState();

                return;
            } catch (error) {
                retries++;
                console.error(`分片 ${chunkIndex} 上传失败 (重试 ${retries}/${maxRetries}):`, error);

                if (retries < maxRetries) {
                    // 智能重试延迟：指数退避
                    const delay = Math.min(this.retryDelay * Math.pow(2, retries - 1), 10000);
                    await this.delay(delay);
                } else {
                    uploadState.failedChunks.add(chunkIndex);
                    throw error;
                }
            }
        }
    }

    // 通过WebSocket上传分片
    async uploadChunkViaWebSocket(uploadState, chunkIndex, chunk) {
        return new Promise((resolve, reject) => {
            const reader = new FileReader();

            reader.onload = (e) => {
                const message = {
                    type: 'file_chunk',
                    name: uploadState.file.name,
                    size: uploadState.file.size,
                    data: Array.from(new Uint8Array(e.target.result)),
                    sessionID: this.sessionID,
                    timestamp: new Date(),
                    totalChunks: uploadState.totalChunks,
                    currentChunk: chunkIndex,
                    isLastChunk: chunkIndex === uploadState.totalChunks - 1
                };

                try {
                    this.webSocket.send(JSON.stringify(message));
                    resolve();
                } catch (error) {
                    reject(error);
                }
            };

            reader.onerror = () => {
                reject(new Error('读取分片数据失败'));
            };

            reader.readAsArrayBuffer(chunk);
        });
    }

    // 通过HTTP API上传分片
    async uploadChunkViaAPI(uploadState, chunkIndex, chunk) {
        const formData = new FormData();
        formData.append('sessionID', this.sessionID);
        formData.append('fileName', uploadState.fileName);
        formData.append('chunkIndex', chunkIndex.toString());
        formData.append('uploadID', uploadState.uploadID);
        formData.append('chunk', chunk);

        const response = await fetch('/api/upload/chunk', {
            method: 'POST',
            body: formData
        });

        const result = await response.json();

        if (!response.ok) {
            throw new Error(result.error || '分片上传失败');
        }

        uploadState.progress = result.progress || 0;

        // 检查是否完成
        if (result.completed) {
            // 服务端已经处理了完成逻辑，标记为已完成
            console.log(`服务端报告上传完成: ${uploadState.fileName}`);
            uploadState.completed = true;
            uploadState.serverCompleted = true; // 标记服务端已完成
        }
    }

    // 完成上传
    async completeUpload(uploadState) {
        try {
            const response = await fetch(`/api/upload/complete/${this.sessionID}/${encodeURIComponent(uploadState.fileName)}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                }
            });

            const result = await response.json();

            if (response.ok) {
                this.onUploadComplete(uploadState);
                console.log(`文件上传完成: ${uploadState.fileName}`);
            } else {
                throw new Error(result.error || '完成上传失败');
            }
        } catch (error) {
            console.error('完成上传失败:', error);
            this.showError(uploadState, error.message);
        }
    }

    // 格式化速度显示
    formatSpeed(bytesPerSecond) {
        if (bytesPerSecond < 1024) {
            return `${bytesPerSecond.toFixed(0)} B/s`;
        } else if (bytesPerSecond < 1024 * 1024) {
            return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`;
        } else {
            return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`;
        }
    }

    // 计算预计剩余时间
    calculateETA(uploadState) {
        if (uploadState.speed === 0) return 0;

        const remainingBytes = uploadState.fileSize * (1 - uploadState.progress / 100);
        return remainingBytes / uploadState.speed; // 秒
    }

    // 格式化时间显示
    formatTime(seconds) {
        if (seconds < 60) {
            return `${Math.round(seconds)}秒`;
        } else if (seconds < 3600) {
            return `${Math.round(seconds / 60)}分钟`;
        } else {
            return `${Math.round(seconds / 3600)}小时`;
        }
    }

    // 延迟函数
    delay(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }

    // 创建进度UI
    createProgressUI(uploadState) {
        const progressContainer = document.getElementById('sent-files-list');
        if (!progressContainer) return;

        const progressElement = document.createElement('div');
        progressElement.className = 'file-item';
        progressElement.id = `upload-${uploadState.fileId}`;
        progressElement.innerHTML = `
            <p><strong>${uploadState.fileName}</strong></p>
            <p>大小: ${this.formatFileSize(uploadState.fileSize)} | 状态: <span id="status-${uploadState.fileId}">准备中...</span></p>
            <p>进度: <span id="progress-${uploadState.fileId}">0%</span> | 速度: <span id="speed-${uploadState.fileId}">0 B/s</span> | 剩余: <span id="eta-${uploadState.fileId}">计算中...</span></p>
            <div class="progress-display">
                <div class="progress-bar" id="progress-bar-${uploadState.fileId}" style="width: 0%"></div>
            </div>
        `;

        progressContainer.prepend(progressElement);
    }

    // 更新进度UI
    updateProgressUI(uploadState) {
        // 计算当前进度
        const currentProgress = (uploadState.completedChunks.size / uploadState.totalChunks) * 100;
        uploadState.progress = currentProgress;

        const progressBar = document.getElementById(`progress-bar-${uploadState.fileId}`);
        const progressText = document.getElementById(`progress-${uploadState.fileId}`);
        const statusText = document.getElementById(`status-${uploadState.fileId}`);
        const speedText = document.getElementById(`speed-${uploadState.fileId}`);
        const etaText = document.getElementById(`eta-${uploadState.fileId}`);

        if (progressBar) {
            progressBar.style.width = `${currentProgress}%`;
        }

        if (progressText) {
            progressText.textContent = `${currentProgress.toFixed(1)}%`;
        }

        if (statusText) {
            if (uploadState.uploading) {
                statusText.textContent = `上传中... (${uploadState.completedChunks.size}/${uploadState.totalChunks} 分片)`;
            } else if (currentProgress >= 100) {
                statusText.textContent = '已完成';
            } else {
                statusText.textContent = '已暂停';
            }
        }

        // 更新速度显示
        if (speedText) {
            if (uploadState.speed > 0) {
                speedText.textContent = this.formatSpeed(uploadState.speed);
            } else {
                speedText.textContent = '0 B/s';
            }
        }

        // 更新预计剩余时间
        if (etaText) {
            if (uploadState.uploading && uploadState.speed > 0) {
                const eta = this.calculateETA(uploadState);
                etaText.textContent = this.formatTime(eta);
            } else {
                etaText.textContent = '计算中...';
            }
        }
    }



    // 上传完成处理
    onUploadComplete(uploadState) {
        // 防止重复处理
        if (uploadState.completed && uploadState.progress === 100) {
            return;
        }

        uploadState.uploading = false;
        uploadState.progress = 100;
        uploadState.completed = true;
        this.updateProgressUI(uploadState);

        console.log(`文件上传完成: ${uploadState.fileName}`);

        // 添加到已发送文件列表（调用主页面的函数）
        console.log(`检查addToSentFiles函数: ${typeof addToSentFiles}`);
        if (typeof addToSentFiles === 'function') {
            console.log(`调用addToSentFiles，文件名: ${uploadState.file.name}`);
            addToSentFiles(uploadState.file);
            console.log(`addToSentFiles调用完成`);
        } else {
            console.error(`addToSentFiles函数不存在或不是函数`);
        }

        // 清理持久化状态
        this.uploads.delete(uploadState.fileId);
        this.persistUploadState();

        // 移除上传进度UI元素
        const uploadItem = document.getElementById(`upload-${uploadState.fileId}`);
        if (uploadItem) {
            console.log(`移除上传进度UI: ${uploadState.fileName}`);
            uploadItem.remove();
        } else {
            console.warn(`未找到上传进度UI元素: upload-${uploadState.fileId}`);
        }
    }

    // 显示错误
    showError(uploadState, message) {
        const statusText = document.getElementById(`status-${uploadState.fileId}`);
        if (statusText) {
            statusText.textContent = `错误: ${message}`;
            statusText.style.color = 'red';
        }

        // 显示弹框提示
        alert(message);

        console.error(`上传错误: ${message}`);
    }

    // 格式化文件大小
    formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    // 延迟函数
    delay(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }
}

// 全局实例
let resumableManager = null;
