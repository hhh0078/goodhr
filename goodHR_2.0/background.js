// 初始化 Service Worker
self.addEventListener('install', (event) => {
    console.log('Service Worker 正在安装');
    event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
    console.log('Service Worker 已激活');
    event.waitUntil(self.clients.claim());
});

// 错误处理
self.addEventListener('error', (event) => {
    console.error('Service Worker 错误:', event.message);
});

self.addEventListener('unhandledrejection', (event) => {
    console.error('Service Worker 未处理的 Promise 拒绝:', event.reason);
});

// 存储收集到的候选人信息
let collectedCandidates = [];

// 添加网站状态管理
let runningSite = null;

// 用于存储已处理的请求
const processedRequests = new Set();

// 用于存储已处理的请求URL，避免重复处理
const processedUrls = new Set();

// 保持 Service Worker 活跃
let keepAliveInterval = null;

function startKeepAlive() {
    if (keepAliveInterval) {
        clearInterval(keepAliveInterval);
    }
    keepAliveInterval = setInterval(() => {
        console.log('Service Worker 保活 ping:', new Date().toISOString());
    }, 20000);
}

// 获取打赏排行榜数据
async function fetchRankingData() {
    try {
        const response = await fetch('https://goodhr.58it.cn/dashang.json');
        const data = await response.json();
        return data;
    } catch (error) {
        console.error('获取排行榜数据失败:', error);
        return [];
    }
}

// 监听来自content script的消息
chrome.runtime.onMessage.addListener(function (message, sender, sendResponse) {
    console.log('Background收到消息:', message);
    startKeepAlive(); // 每次收到消息时重置保活定时器

    // 对于异步操作，需要先返回true
    if (message.type === 'CANDIDATES_COLLECTED' || message.type === 'RESUME_DATA' || message.type === 'GET_RANKING') {
        return true;  // 表明我们会异步发送响应
    }

    try {
        switch (message.action) {
            case 'OPEN_PLUGIN':
                console.log('打开插件的请求');
                // 这里可以添加打开插件的具体逻辑，例如打开一个新的标签页
                chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
                    chrome.tabs.update(tabs[0].id, { url: 'popup/index.html' });
                });
                sendResponse({ status: 'success' });
                break;

            case 'GET_RANKING':
                fetchRankingData()
                    .then(data => {
                        sendResponse({ status: 'success', data: data });
                    })
                    .catch(error => {
                        console.error('获取排行榜数据失败:', error);
                        sendResponse({ status: 'error', error: error.message });
                    });
                return true;  // 表明我们会异步发送响应

            case 'MATCH_SUCCESS':
                console.log('处理匹配成功消息:', message.data);
                // 立即发送响应
                sendResponse({ status: 'success', received: true });
                break;

            case 'CHECK_RUNNING_SITE':
                const canStart = !runningSite || runningSite === message.site;
                sendResponse({ canStart });
                break;

            case 'SITE_STARTED':
                runningSite = message.site;
                sendResponse({ status: 'success' });
                break;

            case 'SITE_STOPPED':
                if (runningSite === message.site) {
                    runningSite = null;
                }
                sendResponse({ status: 'success' });
                break;

            case 'SITE_INITIALIZED':
                if (!runningSite) {
                    runningSite = message.site;
                }
                sendResponse({ status: 'success' });
                break;

            default:
                sendResponse({ status: 'unknown_message_type' });
                break;
        }
    } catch (error) {
        console.error('处理消息时出错:', error);
        sendResponse({ status: 'error', error: error.message });
    }

    return true;
});

// 监听连接以保持service worker活跃
chrome.runtime.onConnect.addListener((port) => {
    console.log('建立新连接:', port.name);
    startKeepAlive(); // 建立连接时启动保活

    port.onMessage.addListener((message) => {
        console.log('收到端口消息:', message);
    });

    port.onDisconnect.addListener(() => {
        console.log('连接断开:', port.name);
    });
});

// 初始化
startKeepAlive();

// 根据设置的筛选条件过滤候选人
async function filterCandidates(candidates) {
    // 获取保存的筛选条件
    const settings = await chrome.storage.local.get([
        'ageRange',
        'education',
        'gender',
        'keywords'
    ]);

    return candidates.filter(candidate => {
        // 年龄筛选
        if (settings.ageRange) {
            const { min, max } = settings.ageRange;
            if (min && candidate.age < min) return false;
            if (max && candidate.age > max) return false;
        }

        // 学历筛选
        if (settings.education &&
            !settings.education.includes('不限') &&
            !settings.education.includes(candidate.education)) {
            return false;
        }

        // 关键词筛选
        if (settings.keywords && settings.keywords.length > 0) {
            const text = `${candidate.name} ${candidate.university}`;
            if (!settings.keywords.some(keyword =>
                text.toLowerCase().includes(keyword.toLowerCase())
            )) {
                return false;
            }
        }

        return true;
    });
}

// 清理文件名，移除不合法字符
function sanitizeFilename(filename) {
    // 替换 Windows 和通用的非法字符
    return filename
        .replace(/[<>:"/\\|?*]/g, '_')  // 替换 Windows 非法字符
        .replace(/\s+/g, '_')           // 替换空格
        .replace(/\./g, '_')            // 替换点号
        .trim();                        // 移除首尾空格
}


