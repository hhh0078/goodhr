// BOSS直聘简历下载器配置
const BOSS_SELECTORS = {
    // 候选人列表容器
    CANDIDATE_LIST: '[role="group"]',
    // 单个候选人
    CANDIDATE_ITEM: '[role="listitem"]',
    // 候选人可点击区域
    CANDIDATE_CLICKABLE: '.geek-item-wrap',
    // 查看简历按钮
    RESUME_BUTTON: '.btn.resume-btn-file',
    // 下载按钮
    DOWNLOAD_BUTTON: 'popover icon-content popover-bottom',
    // 候选人姓名
    CANDIDATE_NAME: '.geek-name',
    // 候选人技能描述
    CANDIDATE_SKILLS: '.source-job',
    // 关闭弹窗
    dialogWrap: '.boss-popup__close'
};

// 移除 export，改为全局变量
window.BossResumeDownloader = class BossResumeDownloader {
    constructor() {
        this.isRunning = false;
        // 生成1-3秒的随机延时
        this.getRandomDelay = () => Math.floor(Math.random() * 1000) + 1000; // 1000-3000ms
        this.downloadCount = 0;
        this.currentCandidate = null;
    }

    async start() {
        if (this.isRunning) return;
        this.isRunning = true;
        
        try {
            await this.processNextCandidate();
        } catch (error) {
            console.error('下载过程出错:', error);
            this.stop();
        }
    }

    stop() {
        this.isRunning = false;
        chrome.runtime.sendMessage({ type: 'DOWNLOAD_STOPPED' });
    }

    async processNextCandidate() {
        if (!this.isRunning) return;

        console.log('查找候选人列表');
        const candidates = document.querySelectorAll(
            `${BOSS_SELECTORS.CANDIDATE_LIST} ${BOSS_SELECTORS.CANDIDATE_ITEM}:not(.processed)`
        );
        console.log('找到候选人数量:', candidates.length);
        
        if (!candidates.length) {
            this.stop();
            chrome.runtime.sendMessage({ 
                type: 'DOWNLOAD_COMPLETE',
                data: { totalCount: this.downloadCount }
            });
            return;
        }

        const candidate = candidates[0];
        try {
            // 保存当前候选人信息到全局变量
            window.currentCandidateInfo = {
                name: candidate.querySelector(BOSS_SELECTORS.CANDIDATE_NAME)?.textContent?.trim(),
                skills: candidate.querySelector(BOSS_SELECTORS.CANDIDATE_SKILLS)?.textContent?.trim(),
                element: candidate
            };
            console.log('已保存候选人信息到全局变量:', window.currentCandidateInfo);

            console.log('处理候选人:', candidate);
            
            // 高亮当前候选人
            candidate.style.backgroundColor = '#fff3e0';
            candidate.style.transition = 'background-color 0.3s';
            
            // 找到并点击候选人的可点击区域
            const clickableArea = candidate.querySelector(BOSS_SELECTORS.CANDIDATE_CLICKABLE);
            if (!clickableArea) {
                throw new Error('未找到可点击区域');
            }
            
            console.log('找到可点击区域:', clickableArea);
            clickableArea.click();
            console.log('已点击候选人');
            
            // 等待简历按钮出现
            console.log('等待简历按钮出现');
            await this.sleep(3000);

            // 查找并点击"查看简历"按钮
            let resumeBtn = null;
            let retryCount = 0;
            const maxRetries = 10;

            while (!resumeBtn && retryCount < maxRetries) {
                resumeBtn = document.querySelector(BOSS_SELECTORS.RESUME_BUTTON);
                if (!resumeBtn) {
                    console.log(`第 ${retryCount + 1} 次尝试查找简历按钮`);
                    await this.sleep(500);
                    retryCount++;
                }
            }

            console.log('简历按钮:', resumeBtn);
            if (resumeBtn) {
                resumeBtn.click();
                console.log('已点击简历按钮');
                
                // 等待下载简历按钮出现
                console.log('等待下载简历按钮出现');
                await this.sleep(1000);

                // 查找并点击"下载简历"按钮
                let downloadBtns = [];
                let downloadCount = 0;
                const downloadRetries = 20;
                let lastLength = 0;  // 记录上一次找到的按钮数量

                while (downloadCount < downloadRetries) {
                    downloadBtns = document.getElementsByClassName(BOSS_SELECTORS.DOWNLOAD_BUTTON);
                    const currentLength = downloadBtns?.length || 0;
                    
                    // 如果按钮数量发生变化，重置等待时间
                    if (currentLength !== lastLength) {
                        console.log('下载按钮数量变化:', currentLength);
                        lastLength = currentLength;
                        downloadCount = 0;  // 重置计数
                        if (currentLength === 3) {  // 如果找到3个按钮，等待一下确保都加载完成
                            await this.sleep(500);
                            break;
                        }
                    }

                    if (downloadCount === 10 && downloadBtns?.length === 1) {
                        console.log('10次尝试后仍只有1个下载按钮，可能不会有更多按钮了');
                        break;
                    }

                    console.log(`第 ${downloadCount + 1} 次尝试查找下载简历按钮，当前数量:`, downloadBtns?.length);
                    await this.sleep(500);
                    downloadCount++;
                }

                if (downloadBtns && downloadBtns.length > 2) {
                    // 始终点击最后一个按钮
                    const downloadBtn = downloadBtns[downloadBtns.length - 1];
                    console.log('下载按钮:', downloadBtn);
                    
                    console.log('找到下载按钮组:', {
                        totalButtons: downloadBtns.length,
                        selectedButton: downloadBtn
                    });
                    
                    try {
                        // 尝试点击下载按钮
                        console.log('尝试点击下载按钮...');
                        
                        // 创建并触发点击事件
                        const clickEvent = new MouseEvent('click', {
                            view: window,
                            bubbles: true,
                            cancelable: true
                        });
                        downloadBtn.dispatchEvent(clickEvent);
                        
                        // 查找并点击所有可能的点击元素
                        const possibleElements = downloadBtn.querySelectorAll('a, button, [role="button"], span, div, svg, use');
                        console.log('找到的所有可能点击元素:', possibleElements);
                        
                        // 等待一下确保元素都加载完成
                        await this.sleep(500);
                        
                        // 点击所有找到的元素
                        for (const element of possibleElements) {
                            try {
                                console.log('尝试点击元素:', element);
                                element.click();
                            } catch (err) {
                                console.log('点击元素失败:', err);
                            }
                        }
                        
                        // 增加下载计数
                        this.downloadCount++;
                        
                        // 等待一下确保下载开始
                        await this.sleep(1000);
                    } catch (error) {
                        console.error('点击下载按钮时出错:', error);
                        throw new Error('点击下载按钮失败');
                    }
                } else {
                    console.error('未找到下载按钮');
                    throw new Error('未找到下载按钮');
                }

                // 关闭弹窗
                const closeBtn = await this.waitForElement(BOSS_SELECTORS.dialogWrap);
                if (closeBtn) {
                    closeBtn.click();
                    console.log('已关闭弹窗');
                }

                // 发送下载成功消息
                chrome.runtime.sendMessage({
                    type: 'RESUME_DOWNLOADED',
                    data: {
                        name: "1",
                        skills: "1",
                        count: 1
                    }
                });
            } else {
                console.error('未找到简历按钮');
                throw new Error('未找到简历按钮');
            }

            // 标记已处理
            candidate.classList.add('processed');
            candidate.style.backgroundColor = '#e8f5e9';

            // 处理下一个
            const randomDelay = this.getRandomDelay();
            console.log(`等待随机时间: ${randomDelay}ms`);
            await this.sleep(randomDelay);
            await this.processNextCandidate();

        } catch (error) {
            this.currentCandidate = null;
            console.error('处理候选人时出错:', error);
            candidate.style.backgroundColor = '#ffebee';
            this.stop();
        }
    }

    // 等待元素出现
    async waitForElement(selector, timeout = 5000) {
        const start = Date.now();
        while (Date.now() - start < timeout) {
            const element = document.querySelector(selector);
            if (element) return element;
            await this.sleep(100);
        }
        return null;
    }

    // 延迟函数
    sleep(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }

    // 等待下载完成
    waitForDownload() {
        return new Promise((resolve, reject) => {
            const timeout = setTimeout(() => {
                reject(new Error('下载超时'));
            }, 30000);  // 增加超时时间到30秒

            const handler = (message) => {
                if (message.type === 'RESUME_DOWNLOAD_COMPLETED') {
                    clearTimeout(timeout);
                    chrome.runtime.onMessage.removeListener(handler);
                    resolve();
                }
            };

            chrome.runtime.onMessage.addListener(handler);
        });
    }
};

// 添加消息监听器
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message.type === 'GET_CANDIDATE_INFO' && window.resumeDownloader?.downloader) {
        sendResponse(window.resumeDownloader.downloader.currentCandidate);
    }
    return true;
}); 