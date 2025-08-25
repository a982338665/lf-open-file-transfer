// 全局变量和函数
let sentFiles = [];

// 全局函数：添加到已发送文件列表
function addToSentFiles(file) {
    console.log(`addToSentFiles被调用，文件: ${file.name}, 当前数量: ${sentFiles.length}`);

    // 添加到数组开头
    sentFiles.unshift({
        name: file.name,
        size: file.size
    });

    console.log(`文件已添加，新数量: ${sentFiles.length}`);

    // 更新已发送文件列表
    updateSentFilesList();
}

// 全局函数：更新已发送文件列表
function updateSentFilesList() {
    console.log(`updateSentFilesList被调用，文件数量: ${sentFiles.length}`);

    const sentFilesList = document.getElementById('sent-files-list');
    if (!sentFilesList) {
        console.error(`找不到sent-files-list元素`);
        return;
    }

    sentFilesList.innerHTML = '';

    // 更新标题中的数量
    const sentFilesTitle = document.getElementById('sent-files-title');
    if (sentFilesTitle) {
        sentFilesTitle.textContent = `已发送文件 (${sentFiles.length})`;
        console.log(`标题已更新为: 已发送文件 (${sentFiles.length})`);
    } else {
        console.error(`找不到sent-files-title元素`);
    }

    sentFiles.forEach(file => {
        const fileItem = document.createElement('div');
        fileItem.className = 'file-item';
        fileItem.innerHTML = `
            <p><strong>${file.name}</strong></p>
            <p>大小: ${formatFileSize(file.size)}</p>
        `;
        sentFilesList.appendChild(fileItem);
    });
}

// 全局函数：格式化文件大小
function formatFileSize(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

document.addEventListener('DOMContentLoaded', function () {
    // 设置功能
    initializeSettings();

    // 标签切换功能
    const tabButtons = document.querySelectorAll('.tab-button');
    const tabContents = document.querySelectorAll('.tab-content');

    tabButtons.forEach(button => {
        button.addEventListener('click', () => {
            const tabName = button.getAttribute('data-tab');

            // 更新激活的标签按钮
            tabButtons.forEach(btn => btn.classList.remove('active'));
            button.classList.add('active');

            // 显示对应的内容区域
            tabContents.forEach(content => {
                content.classList.remove('active');
                if (content.id === tabName + '-tab') {
                    content.classList.add('active');
                }
            });
        });
    });

    let textWebSocket = null;
    let fileWebSocket = null;
    let textSessionID = null;
    let fileSessionID = null;
    let resumableManager = null;

    // 文字输入处理
    const textInput = document.getElementById('text-input');
    const textUrlInput = document.getElementById('text-url');
    const copyTextUrlButton = document.getElementById('copy-text-url');
    const textOnlineCount = document.getElementById('text-online-count'); // 添加在线人数元素

    // 生成文字传输链接
    if (textInput) {
        // 创建文字传输会话
        createSession('text').then(data => {
            textSessionID = data.sessionID;
            textUrlInput.value = `${window.location.origin}/text/${textSessionID}`;

            // 连接WebSocket
            connectTextWebSocket(textSessionID);
        });
    }

    // 连接文字WebSocket
    function connectTextWebSocket(sessionID) {
        textWebSocket = new WebSocket(`ws://${window.location.host}/ws/${sessionID}`);

        textWebSocket.onopen = function (event) {
            console.log("文字传输WebSocket连接已建立");
        };

        textWebSocket.onmessage = function (event) {
            try {
                const message = JSON.parse(event.data);
                switch (message.type) {
                    case 'text':
                        textInput.value = message.content;
                        break;
                    case 'system':
                        console.log("系统消息:", message.content);
                        break;
                    case 'clients':
                        // 更新文字传输在线人数
                        if (textOnlineCount) {
                            textOnlineCount.textContent = message.clients;
                        }
                        break;
                }
            } catch (e) {
                console.error("解析消息失败:", e);
            }
        };

        textWebSocket.onclose = function (event) {
            console.log("文字传输WebSocket连接已关闭");
        };

        textWebSocket.onerror = function (error) {
            console.error("文字传输WebSocket错误:", error);
        };

        // 监听文字输入变化
        let timeout;
        textInput.addEventListener('input', function () {
            // 防抖处理，避免过于频繁发送
            clearTimeout(timeout);
            timeout = setTimeout(() => {
                if (textWebSocket && textWebSocket.readyState === WebSocket.OPEN) {
                    const message = {
                        type: 'text',
                        content: this.value,
                        sessionID: sessionID,
                        timestamp: new Date()
                    };
                    textWebSocket.send(JSON.stringify(message));
                }
            }, 300);
        });
    }

    // 文件上传处理
    const fileInput = document.getElementById('file-input');
    const dropArea = document.getElementById('drop-area');
    const sentFilesList = document.getElementById('sent-files-list');
    const fileUrlInput = document.getElementById('file-url');
    const copyFileUrlButton = document.getElementById('copy-file-url');
    const fileOnlineCount = document.getElementById('file-online-count'); // 添加在线人数元素

    // 存储已发送的文件（已在全局定义）

    // 拖拽事件处理
    if (dropArea) {
        ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
            dropArea.addEventListener(eventName, preventDefaults, false);
        });

        function preventDefaults(e) {
            e.preventDefault();
            e.stopPropagation();
        }

        ['dragenter', 'dragover'].forEach(eventName => {
            dropArea.addEventListener(eventName, highlight, false);
        });

        ['dragleave', 'drop'].forEach(eventName => {
            dropArea.addEventListener(eventName, unhighlight, false);
        });

        function highlight() {
            dropArea.classList.add('dragover');
        }

        function unhighlight() {
            dropArea.classList.remove('dragover');
        }

        // 处理拖拽放置的文件
        dropArea.addEventListener('drop', handleDrop, false);

        function handleDrop(e) {
            const dt = e.dataTransfer;
            const files = dt.files;
            handleFiles(files);
        }

        // 处理选择的文件
        fileInput.addEventListener('change', function () {
            handleFiles(this.files);
        });
    }

    // 生成文件传输链接
    if (fileInput) {
        // 创建文件传输会话
        createSession('file').then(data => {
            fileSessionID = data.sessionID;
            fileUrlInput.value = `${window.location.origin}/file/${fileSessionID}`;

            // 连接WebSocket
            connectFileWebSocket(fileSessionID);
        });
    }

    // 处理文件
    function handleFiles(files) {
        if (files.length > 0) {
            // 检查断点续传管理器是否已初始化
            if (!resumableManager) {
                console.error('断点续传管理器未初始化，请等待WebSocket连接建立');
                return;
            }

            // 使用断点续传管理器上传所有文件
            for (let i = 0; i < files.length; i++) {
                const file = files[i];
                console.log(`开始上传文件: ${file.name}, 大小: ${file.size} 字节`);

                // 使用断点续传管理器上传文件
                resumableManager.startUpload(file).catch(error => {
                    console.error(`文件上传失败: ${file.name}`, error);
                });
            }
        }
    }

    // 分块发送文件（优化大文件处理）
    function sendFileInChunks(file) {
        const chunkSize = 1024 * 1024 * 5; // 将块大小增加到1MB，优化大文件传输效率
        const totalChunks = Math.ceil(file.size / chunkSize);

        // 记录传输开始时间
        const startTime = new Date().getTime();

        // 为文件名创建安全的ID（替换特殊字符）
        const safeFileName = file.name.replace(/[^a-zA-Z0-9]/g, '_');

        // 显示进度信息
        const progressInfo = document.createElement('div');
        progressInfo.className = 'file-item';
        progressInfo.innerHTML = `
            <p><strong>${file.name}</strong></p>
            <p>大小: ${formatFileSize(file.size)}&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;正在传输... <span id="progress-${safeFileName}">0%</span></p>
            <p>耗时: <span id="duration-${safeFileName}">0s</span>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;速度: <span id="speed-${safeFileName}">0 MB/s</span></p>
            <div class="progress-display">
                <div class="progress-bar" id="progress-bar-${safeFileName}"></div>
            </div>
        `;
        sentFilesList.prepend(progressInfo);

        // 开始更新传输时间显示
        const durationElement = document.getElementById(`duration-${safeFileName}`);
        const speedElement = document.getElementById(`speed-${safeFileName}`);
        let totalSent = 0; // 已发送的字节数
        let finalSpeed = null; // 最终传输速度
        const durationInterval = setInterval(() => {
            const currentTime = new Date().getTime();
            const elapsed = currentTime - startTime;
            durationElement.textContent = formatDuration(elapsed);

            // 计算并显示上传速度
            if (elapsed > 0) {
                const speed = totalSent / (elapsed / 1000); // bytes per second
                const speedMB = (speed / (1024 * 1024)).toFixed(2); // MB per second
                speedElement.textContent = `${speedMB} MB/s`;
            }
        }, 1000);

        let currentChunk = 0;
        let chunkSending = false; // 添加标志位防止重复发送
        let sentChunks = 0; // 记录已发送的块数
        let transferCompleted = false; // 标记传输是否在服务器端完成

        // 递归发送文件块
        function sendNextChunk() {
            if (currentChunk >= totalChunks || chunkSending) {
                return;
            }

            chunkSending = true; // 设置发送中标志

            const start = currentChunk * chunkSize;
            const end = Math.min(start + chunkSize, file.size);

            const chunk = file.slice(start, end);
            const reader = new FileReader();

            reader.onload = function (e) {
                const message = {
                    type: 'file_chunk',
                    name: file.name,
                    size: file.size,
                    data: Array.from(new Uint8Array(e.target.result)),
                    sessionID: fileSessionID,
                    timestamp: new Date(),
                    totalChunks: totalChunks,
                    currentChunk: currentChunk,
                    isLastChunk: currentChunk === totalChunks - 1
                };

                console.log(`准备发送文件块: ${file.name}, 块: ${currentChunk}/${totalChunks - 1}, 大小: ${e.target.result.byteLength} bytes`);

                // 添加重试机制
                let retryCount = 0;
                const maxRetries = 3;

                const sendChunk = () => {
                    if (fileWebSocket && fileWebSocket.readyState === WebSocket.OPEN) {
                        try {
                            fileWebSocket.send(JSON.stringify(message));
                            sentChunks++;
                            currentChunk++;
                            totalSent += e.target.result.byteLength; // 更新已发送字节数
                            updateProgress();
                            chunkSending = false; // 发送完成，重置标志
                            console.log(`文件块发送成功: ${file.name}, 块: ${message.currentChunk}, 已发送: ${sentChunks}/${totalChunks}`);

                            // 如果还有更多块需要发送
                            if (currentChunk < totalChunks) {
                                setTimeout(sendNextChunk, 10); // 短暂延迟避免阻塞
                            } else {
                                // 所有块已发送，但等待服务器确认完成
                                updateProgress('发送完成，等待服务器确认');
                                console.log(`文件 ${file.name} 所有块已发送，等待服务器确认完成`);
                                // 计算最终速度
                                const currentTime = new Date().getTime();
                                const elapsed = currentTime - startTime;
                                if (elapsed > 0) {
                                    const speed = totalSent / (elapsed / 1000); // bytes per second
                                    finalSpeed = (speed / (1024 * 1024)).toFixed(2); // MB per second
                                }

                                // 添加更长时间的超时检测，用于调试
                                setTimeout(() => {
                                    console.log(`文件 ${file.name} 等待服务器确认超时，当前状态:`, {
                                        sentChunks,
                                        totalChunks,
                                        transferCompleted
                                    });
                                }, 30000); // 30秒后输出调试信息
                            }
                        } catch (error) {
                            console.error("发送文件块失败:", error);
                            if (retryCount < maxRetries) {
                                retryCount++;
                                setTimeout(sendChunk, 1000 * retryCount); // 重试间隔递增
                            } else {
                                console.error('文件传输失败');
                                // 显示错误信息
                                updateProgress('传输失败');
                                chunkSending = false;
                            }
                        }
                    } else if (retryCount < maxRetries) {
                        retryCount++;
                        setTimeout(sendChunk, 1000 * retryCount); // 重试间隔递增
                    } else {
                        console.error('WebSocket连接不可用，传输已中断');
                        // 显示错误信息
                        updateProgress('连接中断');
                        chunkSending = false;
                    }
                };

                sendChunk();
            };

            reader.onerror = function (error) {
                console.error("读取文件块失败:", error);
                chunkSending = false;
                // 显示错误信息
                updateProgress('读取失败');
                // 尝试发送下一个块
                if (currentChunk < totalChunks) {
                    setTimeout(sendNextChunk, 100);
                }
            };

            reader.readAsArrayBuffer(chunk);
        }

        // 更新进度
        function updateProgress(status) {
            // 如果服务器已确认传输完成，则显示传输完成
            if (transferCompleted) {
                const progressElement = document.getElementById(`progress-${safeFileName}`);
                const progressBar = document.getElementById(`progress-bar-${safeFileName}`);

                if (progressElement) {
                    progressElement.textContent = '传输完成';
                }

                if (progressBar) {
                    progressBar.style.width = '100%';
                }
                return;
            }

            // 如果有特定状态（如错误），显示该状态
            if (status) {
                const progressElement = document.getElementById(`progress-${safeFileName}`);
                if (progressElement) {
                    progressElement.textContent = status;
                }
                return;
            }

            // 否则基于已发送的块数计算进度，但不超过99%
            const progress = Math.min(Math.round((sentChunks / totalChunks) * 100), 99);
            const progressElement = document.getElementById(`progress-${safeFileName}`);
            const progressBar = document.getElementById(`progress-bar-${safeFileName}`);

            if (progressElement) {
                progressElement.textContent = `${progress}%`;
            }

            if (progressBar) {
                progressBar.style.width = `${progress}%`;
            }
        }

        // 监听服务器确认传输完成的消息
        function onTransferCompleted() {
            // 停止更新时间显示
            clearInterval(durationInterval);

            // 显示最终传输时间
            const currentTime = new Date().getTime();
            const elapsed = currentTime - startTime;
            durationElement.textContent = formatDuration(elapsed);

            // 显示最终速度
            if (elapsed > 0) {
                const speed = file.size / (elapsed / 1000); // bytes per second
                const speedMB = (speed / (1024 * 1024)).toFixed(2); // MB per second
                speedElement.textContent = `${speedMB} MB/s`;
            }

            transferCompleted = true;
            updateProgress();
            console.log(`文件 ${file.name} 传输完成，共发送 ${sentChunks} 个块`);
        }

        // 将完成回调暴露给外部，以便在收到服务器确认时调用
        // 使用安全的文件名作为键
        sendFileInChunks[`onComplete_${safeFileName}`] = onTransferCompleted;

        // 开始发送第一个块
        sendNextChunk();
    }



    // 连接文件WebSocket
    function connectFileWebSocket(sessionID) {
        fileWebSocket = new WebSocket(`ws://${window.location.host}/ws/${sessionID}`);

        fileWebSocket.onopen = function (event) {
            console.log("文件传输WebSocket连接已建立");

            // 初始化断点续传管理器
            resumableManager = new ResumableUploadManager(sessionID, fileWebSocket);

            // 暴露到全局作用域，方便调试和控制
            window.resumableManager = resumableManager;

            console.log("断点续传管理器已初始化");
        };

        fileWebSocket.onmessage = function (event) {
            try {
                const message = JSON.parse(event.data);
                console.log("收到WebSocket消息:", message);
                switch (message.type) {
                    case 'file':
                        console.log("文件已发送完成:", message);

                        // 添加到已发送文件列表
                        if (message.name && message.size) {
                            console.log("从WebSocket消息添加文件到已发送列表");
                            addToSentFiles({
                                name: message.name,
                                size: message.size
                            });
                        }

                        // 显示文件传输完成的提示
                        const fileItems = document.querySelectorAll('.file-item');
                        console.log("找到文件项数量:", fileItems.length);
                        fileItems.forEach((item, index) => {
                            const nameElement = item.querySelector('strong');
                            console.log(`检查文件项 ${index}:`, {
                                elementText: nameElement ? nameElement.textContent : '无',
                                messageName: message.name,
                                isMatch: nameElement && nameElement.textContent === message.name
                            });

                            // 为消息中的文件名创建安全的ID（替换特殊字符）
                            const safeFileName = message.name.replace(/[^a-zA-Z0-9]/g, '_');

                            if (nameElement && nameElement.textContent === message.name) {
                                console.log("匹配到文件项，更新状态");
                                // 尝试使用转义后的文件名查找元素
                                const progressElement = item.querySelector(`#progress-${safeFileName}`);
                                if (progressElement) {
                                    progressElement.textContent = '传输完成';
                                } else {
                                    // 如果找不到特定的进度元素，尝试查找通用的进度元素
                                    const genericProgressElement = item.querySelector('[id^="progress-"]');
                                    if (genericProgressElement) {
                                        genericProgressElement.textContent = '传输完成';
                                    }
                                }

                                // 调用对应的完成回调函数
                                // 尝试多种可能的回调函数名
                                const possibleFunctionNames = [
                                    `onComplete_${safeFileName}`,
                                    `onComplete_${message.name}`
                                ];

                                let onCompleteFunc = null;
                                for (const funcName of possibleFunctionNames) {
                                    if (typeof sendFileInChunks[funcName] === 'function') {
                                        onCompleteFunc = sendFileInChunks[funcName];
                                        console.log("找到完成回调函数:", funcName);
                                        break;
                                    }
                                }

                                if (onCompleteFunc) {
                                    console.log("调用完成回调函数");
                                    onCompleteFunc();
                                } else {
                                    console.log("未找到完成回调函数，尝试过的名称:", possibleFunctionNames);
                                }

                                // 确保进度条显示100%
                                const progressBar = item.querySelector(`#progress-bar-${safeFileName}`);
                                if (progressBar) {
                                    progressBar.style.width = '100%';
                                }
                            }
                        });

                        // 如果没有找到对应的文件项，可能是页面刷新后重新连接
                        if (fileItems.length === 0) {
                            console.log("未找到文件项，可能是页面刷新后重新连接");
                            // 为消息中的文件名创建安全的ID（替换特殊字符）
                            const safeFileName = message.name.replace(/[^a-zA-Z0-9]/g, '_');

                            // 检查是否需要更新全局传输完成状态
                            const possibleFunctionNames = [
                                `onComplete_${safeFileName}`,
                                `onComplete_${message.name}`
                            ];

                            let onCompleteFunc = null;
                            for (const funcName of possibleFunctionNames) {
                                if (typeof sendFileInChunks[funcName] === 'function') {
                                    onCompleteFunc = sendFileInChunks[funcName];
                                    console.log("找到完成回调函数（无文件项情况）:", funcName);
                                    break;
                                }
                            }

                            if (onCompleteFunc) {
                                console.log("调用完成回调函数（无文件项情况）");
                                onCompleteFunc();
                            } else {
                                console.log("未找到完成回调函数（无文件项情况），尝试过的名称:", possibleFunctionNames);
                            }
                        }
                        break;
                    case 'clients':
                        // 更新文件传输在线人数
                        if (fileOnlineCount) {
                            fileOnlineCount.textContent = message.clients;
                        }
                        // 更新平台总在线会话数
                        const fileTotalSessions = document.getElementById('file-total-sessions');
                        if (fileTotalSessions && message.totalSessions !== undefined) {
                            fileTotalSessions.textContent = message.totalSessions;
                        }
                        break;
                }
            } catch (e) {
                console.error("解析消息失败:", e);
            }
        };

        fileWebSocket.onclose = function (event) {
            console.log("文件传输WebSocket连接已关闭");
        };

        fileWebSocket.onerror = function (error) {
            console.error("文件传输WebSocket错误:", error);
        };
    }

    // 复制链接功能
    if (copyTextUrlButton) {
        copyTextUrlButton.addEventListener('click', function () {
            textUrlInput.select();
            document.execCommand('copy');
            const originalText = this.textContent;
            this.textContent = '已复制!';
            setTimeout(() => {
                this.textContent = originalText;
            }, 2000);
        });
    }

    if (copyFileUrlButton) {
        copyFileUrlButton.addEventListener('click', function () {
            fileUrlInput.select();
            document.execCommand('copy');
            const originalText = this.textContent;
            this.textContent = '已复制!';
            setTimeout(() => {
                this.textContent = originalText;
            }, 2000);
        });
    }
});

// 创建会话
async function createSession(type) {
    const sessionID = generateSessionID();
    const response = await fetch('/api/session', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({type, sessionID})
    });
    return await response.json();
}

// 生成UUID函数
function generateUUID() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
        const r = Math.random() * 16 | 0;
        const v = c == 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

// 格式化文件大小（已在全局定义）

// 格式化持续时间
function formatDuration(milliseconds) {
    const totalSeconds = Math.floor(milliseconds / 1000);

    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;

    let result = '';
    if (hours > 0) {
        result += hours + 'h';
    }
    if (minutes > 0) {
        result += minutes + 'm';
    }
    result += seconds + 's';

    return result;
}

// 设置功能相关
let urlMode = 'number'; // 默认使用数字累加模式
let currentNumber = 1; // 当前数字计数器

// 初始化设置功能
function initializeSettings() {
    // 从localStorage加载设置
    const savedMode = localStorage.getItem('urlMode');
    const savedNumber = localStorage.getItem('currentNumber');

    if (savedMode) {
        urlMode = savedMode;
    }
    if (savedNumber) {
        currentNumber = parseInt(savedNumber);
    }

    // 设置按钮事件
    const settingsBtn = document.getElementById('settings-btn');
    const settingsModal = document.getElementById('settings-modal');
    const closeBtn = settingsModal.querySelector('.close');
    const saveBtn = document.getElementById('save-settings');
    const cancelBtn = document.getElementById('cancel-settings');

    // 打开设置弹窗
    settingsBtn.addEventListener('click', () => {
        // 设置当前选中的模式
        const radioButtons = document.querySelectorAll('input[name="url-mode"]');
        radioButtons.forEach(radio => {
            radio.checked = radio.value === urlMode;
        });

        settingsModal.style.display = 'block';
    });

    // 关闭弹窗
    const closeModal = () => {
        settingsModal.style.display = 'none';
    };

    closeBtn.addEventListener('click', closeModal);
    cancelBtn.addEventListener('click', closeModal);

    // 点击弹窗外部关闭
    window.addEventListener('click', (event) => {
        if (event.target === settingsModal) {
            closeModal();
        }
    });

    // 保存设置
    saveBtn.addEventListener('click', () => {
        const selectedMode = document.querySelector('input[name="url-mode"]:checked').value;
        urlMode = selectedMode;

        // 保存到localStorage
        localStorage.setItem('urlMode', urlMode);
        localStorage.setItem('currentNumber', currentNumber.toString());

        closeModal();

        // 显示保存成功提示
        alert('设置已保存！新的链接将使用新的模式。');
    });
}

// 生成会话ID（根据设置的模式）
function generateSessionID() {
    if (urlMode === 'number') {
        const sessionID = currentNumber.toString();
        currentNumber++;
        // 保存更新后的数字
        localStorage.setItem('currentNumber', currentNumber.toString());
        return sessionID;
    } else {
        // UUID模式，使用原来的随机生成方式
        return Math.random().toString(36).substr(2, 9) + Date.now().toString(36);
    }
}
