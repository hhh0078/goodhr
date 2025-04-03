// 配置选择器


class ResumeDownloader {
    constructor() {
        this.downloader = null;
    }

    // 初始化下载器
    initDownloader(hostname) {
        console.log('初始化下载器，hostname:', hostname);
        if (hostname.includes('zhipin.com')) {
            this.downloader = new window.BossResumeDownloader();
            console.log('BOSS直聘下载器已初始化');
        }
        // 后续可以添加其他网站的下载器
    }

    async start() {
        console.log('开始下载，downloader:', this.downloader);
        if (!this.downloader) {
            throw new Error('未初始化下载器');
        }
        await this.downloader.start();
    }

    stop() {
        if (this.downloader) {
            this.downloader.stop();
        }
    }
}

// 监听来自popup的消息
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    console.log('收到消息:', message);
    
    if (!window.resumeDownloader) {
        console.log('创建下载器实例');
        window.resumeDownloader = new ResumeDownloader();
        window.resumeDownloader.initDownloader(window.location.hostname);
    }

    if (message.action === 'START_DOWNLOAD') {
        console.log('开始下载流程');
        window.resumeDownloader.start()
            .then(() => {
                console.log('下载启动成功');
                sendResponse({ status: 'started' });
            })
            .catch(error => {
                console.error('下载启动失败:', error);
                sendResponse({ status: 'error', message: error.message });
            });
    } else if (message.action === 'STOP_DOWNLOAD') {
        window.resumeDownloader.stop();
        sendResponse({ status: 'stopped' });
    }
    return true;
}); 