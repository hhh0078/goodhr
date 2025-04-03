let currentParser = null;
let scrollInterval = null;
let lastProcessedPosition = 0;
let isRunning = false;
let currentDelay = 3000;
let matchLimit = 0;
let scrollDelayMin = 3000;
let scrollDelayMax = 6000;
let port = null;
let matchCount = 0;
let currentPrompt = null;

// 显示提示信息
function showNotification(message, type = 'status') {
    if (!isExtensionValid()) {
        console.warn('扩展上下文已失效，无法发送通知');
        return;
    }

    const notification = document.createElement('div');

    // 基础样式
    let baseStyle = `
        position: fixed;
        padding: 12px 20px;
        background: rgba(51, 51, 51, 0.9);
        color: white;
        border-radius: 6px;
        z-index: 9999;
        font-size: 14px;
        box-shadow: 0 2px 12px rgba(0,0,0,0.2);
        pointer-events: none;
    `;

    // 根据类型设置不同的位置样式
    if (type === 'status') {
        baseStyle += `
            left: 50%;
            top: 20px;
            transform: translateX(-50%);
        `;
    } else {
        baseStyle += `
            left: 50%;
            top: 50%;
            transform: translate(-50%, -50%);
        `;
    }

    notification.style.cssText = baseStyle;
    notification.textContent = message;
    document.body.appendChild(notification);

    setTimeout(() => {
        notification.remove();
    }, 5000);
}

// 根据当前网站URL选择合适的解析器
async function initializeParser() {
    try {
        const url = window.location.href;
        console.log('当前URL:', url);





        const extensionUrl = chrome.runtime.getURL('');

        if (url.includes('zhipin.com')) {
            const { BossParser } = await import(extensionUrl + 'content_scripts/sites/boss.js');
            currentParser = new BossParser();
            showNotification('BOSS直聘初始化完成，请前往推荐牛人页面使用-GoodHR', 'status');
            // 检查是否在iframe中
            const isInIframe = window !== window.top;
            console.log('是否在iframe中:', isInIframe);
            // 跳过 about:blank 和非主框架页面

            if (!isInIframe) {
                console.log('在主框架中创建询问框');
                createDraggablePrompt(); // 只在主框架中创建询问框
            }


        } else if (url.includes('lagou.com')) {
            const { LagouParser } = await import(extensionUrl + 'content_scripts/sites/lagou.js');
            currentParser = new LagouParser();
            showNotification('拉勾网初始化完成-GoodHR', 'status');
            createDraggablePrompt();
        } else if (url.includes('liepin.com')) {
            const { LiepinParser } = await import(extensionUrl + 'content_scripts/sites/liepin.js');
            currentParser = new LiepinParser();
            showNotification('猎聘网初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        } else if (url.includes('zhaopin.com')) {
            const { ZhilianParser } = await import(extensionUrl + 'content_scripts/sites/zhilian.js');
            currentParser = new ZhilianParser();
            showNotification('智联网初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        }

        if (currentParser) {
            await currentParser.loadSettings();
        } else {
            console.warn('未找到匹配的解析器，当前URL:', url);
            throw new Error('未找到匹配的解析器');
        }
    } catch (error) {
        console.error('初始化解析器失败:', error);
        showNotification('⚠️ 初始化解析器失败: ' + error.message, 'status');
    }
}

// 添加随机延迟函数
function randomDelay(min = 3, max = 5) {
    // 获取最新的设置值（注意：这里的值已经是秒为单位）
    const currentMin = currentParser?.filterSettings?.scrollDelayMin || 3;
    const currentMax = currentParser?.filterSettings?.scrollDelayMax || 5;

    // 使用最新的设置值
    const actualMin = currentMin;
    const actualMax = currentMax;

    // 生成随机延迟（秒）
    const delaySeconds = Math.floor(Math.random() * (actualMax - actualMin + 1) + actualMin);

    // 转换为毫秒
    const delayMs = delaySeconds * 1000;

    console.log('当前延迟设置:', {
        min: actualMin,
        max: actualMax,
        selectedDelay: delaySeconds
    });

    sendMessage({
        type: 'LOG_MESSAGE',
        data: {
            message: `随机停止 ${delaySeconds} 秒`,
            type: 'info'
        }
    });
    return new Promise(resolve => setTimeout(resolve, delayMs));
}

// 添加一个函数来获取所有可用的文档对象
function getAllDocuments() {
    const documents = [document];

    const frames = document.getElementsByTagName('iframe');
    for (const frame of frames) {
        try {
            if (frame.contentDocument) {
                documents.push(frame.contentDocument);
            }
        } catch (error) {
            console.warn('无法访问 iframe:', error);
        }
    }

    return documents;
}

// 修改自动滚动功能
async function startAutoScroll() {

    // if(!window.location.href.includes(currentParser.urlInfo.url)){
    //     console.log("当前页面URL:",window.location.href);
    //     console.log("页面URL:",currentParser.urlInfo.url);
    //     console.log("当前页面是否包含目标URL:",!window.location.href.includes(currentParser.urlInfo.url));

    //     console.log("请在"+currentParser.urlInfo.site+"页面使用该插件");

    //     sendMessage({
    //         type: 'LOG_MESSAGE',
    //         data: {
    //             message:"请在"+currentParser.urlInfo.site+"页面使用该插件",
    //             type: 'ERROR'
    //         }
    //     });
    //     showNotification("⚠️ 请在"+currentParser.urlInfo.site+"页面使用该插件", 'status');
    //     isRunning = false;
    //         stopAutoScroll();
    //         return;
    //     return;
    // }

    if (isRunning) return;

    try {
        isRunning = true;
        lastProcessedPosition = 0;

        // 从 currentParser 获取设置，不要在这里乘以1000
        matchLimit = currentParser?.filterSettings?.matchLimit || 200;
        scrollDelayMin = currentParser?.filterSettings?.scrollDelayMin || 3;
        scrollDelayMax = currentParser?.filterSettings?.scrollDelayMax || 5;

        window.scrollTo(0, 0);

        sendMessage({
            type: 'LOG_MESSAGE',
            data: {
                message: `开始滚动`,
                type: 'info'
            }
        });

        executeScroll();
        showNotification('开始自动滚动', 'status');
    } catch (error) {
        isRunning = false;
        console.error('启动失败:', error);
        showNotification('⚠️ ' + error.message, 'status');
        throw error;
    }
}

// 将滚动逻辑提取为单独的函数
async function executeScroll() {
    if (!isRunning || !currentParser) {
        if (scrollInterval) {
            clearInterval(scrollInterval);
            scrollInterval = null;
        }
        isRunning = false;
        return;
    }

    try {
        await currentParser.loadSettings();
        const documents = getAllDocuments();

        for (const doc of documents) {
            const selector = '.' + currentParser.selectors.items;
            let elements = Array.from(doc.querySelectorAll(selector));

            if (elements.length === 0) {
                const looseSelector = `[class*="${currentParser.selectors.items}"]`;
                const looseElements = Array.from(doc.querySelectorAll(looseSelector));
                if (looseElements.length > 0) {
                    elements.push(...looseElements);
                }
            }

            const unprocessedElements = elements.filter(el => {
                const rect = el.getBoundingClientRect();
                const absoluteTop = rect.top + (doc === document ?
                    window.pageYOffset :
                    doc.defaultView.pageYOffset);
                return absoluteTop > lastProcessedPosition;
            });

            if (unprocessedElements.length > 0) {
                const element = unprocessedElements[0];

                try {
                    // 滚动到元素位置
                    const rect = element.getBoundingClientRect();
                    const scrollTo = rect.top + window.pageYOffset - 100;

                    window.scrollTo({
                        top: scrollTo,
                        behavior: 'smooth'
                    });

                    // 创建临时高亮样式
                    const tempStyleEl = document.createElement('style');
                    const tempClass = 'temp-highlight-' + Math.random().toString(36).substr(2, 9);
                    element.classList.add(tempClass);

                    tempStyleEl.textContent = `
                        .${tempClass} {
                            background-color: rgba(255, 247, 224, 0.8) !important;
                            transition: all 0.3s ease !important;
                            outline: 2px dashed #ffa726 !important;
                            position: relative !important;
                            box-shadow: 0 0 20px rgba(255, 167, 38, 0.6) !important;
                        }
                    `;
                    document.head.appendChild(tempStyleEl);

                    // 处理元素
                    await processElement(element, doc);

                    // 清理临时样式
                    element.classList.remove(tempClass);
                    tempStyleEl.remove();

                    // 等待一个短暂的延迟后继续处理下一个元素
                    await new Promise(resolve => setTimeout(resolve, 500));
                    executeScroll();
                    return;

                } catch (error) {
                    console.error('处理元素失败:', error);
                    // 出错时也继续处理下一个
                    executeScroll();
                    return;
                }
            } else {
                // 如果没找到未处理的元素，向下滚动一段距离
                window.scrollBy({
                    top: 200,
                    behavior: 'smooth'
                });

                // 使用 randomDelay 获取最新的延迟设置
                await randomDelay();
                executeScroll();
                return;
            }
        }
    } catch (error) {
        console.error('滚动处理失败:', error);
        showNotification('⚠️ 滚动处理出错', 'status');
        stopAutoScroll();
    }
}

// 处理单个元素的函数
async function processElement(element, doc) {
    try {
        // 首先检查是否已达到匹配限制
        if (matchCount >= matchLimit) {
            console.log(`已达到匹配限制 ${matchLimit}，停止处理`);
            isRunning = false;
            stopAutoScroll();
            return;
        }

        await currentParser.loadSettings();

        let targetElement = element.closest('.' + currentParser.selectors.items);
        if (!targetElement) {
            targetElement = element.closest(`[class*="${currentParser.selectors.items}"]`);
        }

        if (!targetElement) return;

        // 先清除之前的样式
        targetElement.removeAttribute('style');

        // 直接应用样式到目标元素
        const styles = {
            'background-color': '#fff3e0',
            'border': '2px solid #ffa726',
            'position': 'relative',
            'box-shadow': '0 0 15px rgba(255, 167, 38, 0.4)',
            'transition': 'all 0.3s ease'
        };

        Object.entries(styles).forEach(([property, value]) => {
            targetElement.style.setProperty(property, value, 'important');
        });

        const rect = element.getBoundingClientRect();
        lastProcessedPosition = rect.top + rect.height + (doc === document ?
            window.pageYOffset :
            doc.defaultView.pageYOffset);

        const candidates = currentParser.extractCandidates([element]);
        if (candidates.length > 0) {
            for (const candidate of candidates) {
                // 再次检查是否已达到匹配限制
                if (matchCount >= matchLimit) {
                    console.log(`处理候选人过程中达到匹配限制 ${matchLimit}，停止处理`);
                    isRunning = false;
                    stopAutoScroll();
                    return;
                }

                let shouldSkipDelay = false;

                // 首先进行随机点击判断
                let clickCandidate = currentParser.shouldClickCandidate();
                if (clickCandidate) {
                    // 检查是否有打开的候选人页面
                    const maxWaitTime = 10000; // 最大等待时间10秒
                    const startTime = Date.now();

                    while (document.querySelector('.boss-popup__close') ||
                        document.querySelector('.km-icon.sati.sati-times-circle-s')) {
                        console.log('等待上一个候选人页面关闭...');
                        await new Promise(resolve => setTimeout(resolve, 500));

                        // 如果等待时间超过10秒，强制关闭
                        if (Date.now() - startTime > maxWaitTime) {
                            console.log('等待超时，尝试强制关闭');
                            await currentParser.closeDetail();
                            break;
                        }
                    }

                    const clicked = await currentParser.clickCandidateDetail(element);
                    if (clicked) {
                        shouldSkipDelay = true;
                        console.log('准备查看候选人:', candidate.name);

                        sendMessage({
                            type: 'LOG_MESSAGE',
                            data: {
                                message: `查看候选人: ${candidate.name}`,
                                type: 'info'
                            }
                        });

                        await randomDelay();

                        // 确保详情页完全关闭
                        await currentParser.closeDetail();

                        // 等待一小段时间确保页面完全关闭
                        await new Promise(resolve => setTimeout(resolve, 500));
                    }
                } else {
                    console.log('不打开候选人页面:', candidate.name);
                    await randomDelay();
                }

                if (currentParser.filterCandidate(candidate)) {
                    // 再次检查是否已达到匹配限制
                    if (matchCount >= matchLimit) {
                        console.log(`匹配成功但已达到限制 ${matchLimit}，停止处理`);
                        isRunning = false;
                        stopAutoScroll();
                        return;
                    }

                    console.log('信息匹配:', candidate.name);
                    const matchStyles = {
                        'background-color': '#e8f5e9',
                        'border': '2px solid rgb(115, 172, 117)',
                        'box-shadow': '0 0 10px rgba(102, 180, 104, 0.3)'
                    };

                    Object.entries(matchStyles).forEach(([property, value]) => {
                        targetElement.style.setProperty(property, value, 'important');
                    });
                    console.log('开始打招呼:', candidate.name);

                    const clicked = currentParser.clickMatchedItem(element);
                    if (clicked) {
                        matchCount++;
                        console.log(`打招呼成功，当前计数: ${matchCount}/${matchLimit}`);
                    }

                    await sendMessage({
                        type: 'MATCH_SUCCESS',
                        data: {
                            name: candidate.name,
                            age: candidate.age,
                            education: candidate.education,
                            university: candidate.university,
                            extraInfo: candidate.extraInfo,
                            matchTime: new Date().toLocaleTimeString('zh-CN', {
                                hour12: false,
                                hour: '2-digit',
                                minute: '2-digit',
                                second: '2-digit'
                            }),
                            clicked: clicked
                        }
                    });

                    // 最后再次检查是否需要停止
                    if (matchCount >= matchLimit) {
                        console.log(`已达到匹配限制 ${matchLimit}，即将停止`);
                        isRunning = false;
                        stopAutoScroll();
                        return;
                    }
                } else {
                    console.log('信息不匹配:', candidate.name);
                    targetElement.removeAttribute('style');
                }
            }
        }

        // 5秒后移除样式
        setTimeout(() => {
            targetElement.removeAttribute('style');
        }, 5000);

    } catch (error) {
        console.error('处理元素失败:', error);
        element.removeAttribute('style');
    }
}

// 处理来自popup的消息
chrome.runtime.onMessage.addListener(async (message, sender, sendResponse) => {
    try {
        switch (message.action) {
            case 'START_SCROLL':
                console.log('收到开始滚动消息:', message);
                // 更新点击频率设置
                if (currentParser && message.data.clickFrequency !== undefined) {
                    currentParser.clickCandidateConfig.frequency = message.data.clickFrequency;
                    console.log('更新点击频率为:', message.data.clickFrequency);
                }
                // 更新其他设置
                if (message.data.keywords) {
                    currentParser.filterSettings = {
                        ...currentParser.filterSettings,
                        keywords: message.data.keywords,
                        excludeKeywords: message.data.excludeKeywords,
                        isAndMode: message.data.isAndMode,
                        matchLimit: message.data.matchLimit,
                        scrollDelayMin: message.data.scrollDelayMin,
                        scrollDelayMax: message.data.scrollDelayMax
                    };
                }
                await startAutoScroll();
                sendResponse({ status: 'success' });
                break;
            case 'STOP_SCROLL':
                stopAutoScroll();
                sendResponse({ status: 'stopped' });
                break;
            case 'UPDATE_KEYWORDS':
                if (currentParser) {
                    currentParser.setFilterSettings(message.data);
                    sendResponse({ status: 'updated' });
                } else {
                    sendResponse({ status: 'error', message: '解析器未初始化' });
                }
                break;
            case 'SETTINGS_UPDATED':
                if (currentParser) {
                    // 更新解析器的设置
                    currentParser.setFilterSettings({
                        ...message.data,
                        scrollDelayMin: message.data.scrollDelayMin || 3,
                        scrollDelayMax: message.data.scrollDelayMax || 5
                    });
                    console.log('已更新设置:', message.data);
                    sendResponse({ status: 'ok' });
                } else {
                    console.error('解析器未初始化');
                    sendResponse({ status: 'error', message: '解析器未初始化' });
                }
                break;
            default:
                console.error('未知的消息类型:', message.action);
                sendResponse({ status: 'error', message: '未知的消息类型' });
        }
    } catch (error) {
        console.error('处理消息时出错:', error);
        isRunning = false;
        sendResponse({ status: 'error', message: error.message });
    }
    return true;  // 表示会异步发送响应
});

// 停止滚动时重置位置
function stopAutoScroll() {
    if (!isRunning) return;

    try {
        isRunning = false;
        if (scrollInterval) {
            clearInterval(scrollInterval);
            scrollInterval = null;
        }
        lastProcessedPosition = 0;

        if (currentParser) {
            document.querySelectorAll(`[class^="${currentParser.selectors.items}"], [class*=" ${currentParser.selectors.items}"]`)
                .forEach(el => {
                    el.style.cssText = '';
                });
        }

        if (isExtensionValid()) {
            showNotification('已停止自动滚动', 'status');
        } else {
            console.warn('扩展已重新加载，自动滚动已停止');
        }
    } catch (error) {
        console.error('停止失败:', error);
    }
}

// 添加一个检查扩展状态的函数
function isExtensionValid() {
    return chrome.runtime && chrome.runtime.id;
}

// 初始化连接
function initializeConnection() {
    try {
        port = chrome.runtime.connect({ name: 'content-script-connection' });
        port.onDisconnect.addListener(() => {
            console.log('连接断开，尝试重新连接');
            port = null;
            setTimeout(initializeConnection, 1000);
        });
        return true;
    } catch (error) {
        console.error('建立连接失败:', error);
        return false;
    }
}

// 封装消息发送函数
async function sendMessage(message) {
    const MAX_RETRIES = 3;
    let retryCount = 0;

    while (retryCount < MAX_RETRIES) {
        try {
            if (!isExtensionValid()) {
                console.warn('扩展上下文已失效，请刷新页面');
                throw new Error('扩展上下文已失效');
            }

            // 确保连接存在
            if (!port && !initializeConnection()) {
                throw new Error('无法建立连接');
            }

            console.log("准备发送消息到插件:", message.type);

            return await new Promise((resolve, reject) => {
                chrome.runtime.sendMessage(message, function (response) {
                    const lastError = chrome.runtime.lastError;
                    if (lastError) {
                        // 如果是连接问题，尝试重新连接
                        if (lastError.message.includes('Receiving end does not exist')) {
                            console.log('连接断开，尝试重新连接');
                            port = null;
                            initializeConnection();
                            reject(lastError);
                            return;
                        }
                        console.error('发送消息失败:', lastError);
                        reject(lastError);
                        return;
                    }
                    console.log("消息发送成功，收到响应:", response);
                    resolve(response);
                });
            });
        } catch (error) {
            retryCount++;
            console.error(`发送消息失败 (尝试 ${retryCount}/${MAX_RETRIES}):`, error);

            if (retryCount === MAX_RETRIES) {
                throw error;
            }

            // 等待后重试
            await new Promise(resolve => setTimeout(resolve, 1000 * retryCount));
        }
    }
}

function createDraggablePrompt() {
    return
    // 如果已经存在询问框，先移除它
    if (currentPrompt) {
        currentPrompt.remove();
    }

    const prompt = document.createElement('div');
    currentPrompt = prompt; // 设置全局变量
    prompt.className = 'goodhr-prompt';
    prompt.style.position = 'fixed';
    prompt.style.top = '20px';
    prompt.style.right = '20px';
    prompt.style.padding = '10px';
    prompt.style.backgroundColor = '#f9f9f9';
    prompt.style.border = '1px solid #ddd';
    prompt.style.borderRadius = '10px';
    prompt.style.boxShadow = '0 6px 20px rgba(0,0,0,0.5)';
    prompt.style.zIndex = '9999';
    prompt.style.transition = 'opacity 0.3s ease, transform 0.3s ease';
    prompt.style.opacity = '0';
    prompt.style.transform = 'translateY(-10px)';
    prompt.innerHTML = `
        <div style='cursor: move; display: flex; justify-content: space-between; align-items: center;'>
            <div style='display: flex; align-items: center;'>
                <strong style='color: #1a73e8; margin-right: 5px; font-size: 16px;'>GoodHR 插件</strong>
                <span style='background-color: #e8f0fe; color: #1a73e8; padding: 2px 6px; border-radius: 10px; font-size: 12px;'>v${chrome.runtime.getManifest().version}</span>
            </div>
            <span style='font-size: 12px; color: #999;'>拖动</span>
        </div>
        <div style='margin-top: 15px; text-align: center;'>
            <div style='margin-bottom: 15px; font-size: 14px;'>是否打开 GoodHR 插件？</div>
            <div>
                <button id='open-plugin' style='
                    padding: 5px 20px;
                    background-color: #1a73e8;
                    color: white;
                    border: none;
                    border-radius: 5px;
                    cursor: pointer;
                    transition: background-color 0.2s, transform 0.2s;
                    font-weight: 500;
                '>是</button>
                <button id='close-prompt' style='
                    padding: 5px 20px;
                    background-color: #ff4444;
                    color: white;
                    border: none;
                    border-radius: 5px;
                    cursor: pointer;
                    margin-left: 10px;
                    transition: background-color 0.2s, transform 0.2s;
                    font-weight: 500;
                '>取消 (10s)</button>
            </div>
        </div>
    `;

    document.body.appendChild(prompt);

    // 添加按钮悬停效果
    const buttons = prompt.querySelectorAll('button');
    buttons.forEach(button => {
        button.addEventListener('mouseover', () => {
            button.style.opacity = '0.9';
            button.style.transform = 'translateY(-1px)';
        });
        button.addEventListener('mouseout', () => {
            button.style.opacity = '1';
            button.style.transform = 'translateY(0)';
        });
    });

    // 弹出动画
    setTimeout(() => {
        prompt.style.opacity = '1';
        prompt.style.transform = 'translateY(0)';
    }, 10);

    // 倒计时功能
    let countdown = 10;
    const closeButton = prompt.querySelector('#close-prompt');
    const countdownInterval = setInterval(() => {
        countdown--;
        if (countdown >= 0) {
            closeButton.textContent = `取消 (${countdown}s)`;
        }
        if (countdown === 0) {
            clearInterval(countdownInterval);
            prompt.style.opacity = '0';
            prompt.style.transform = 'translateY(-10px)';
            setTimeout(() => {
                prompt.remove();
                currentPrompt = null; // 清除全局变量
            }, 300);
        }
    }, 1000);

    // 拖拽功能
    let isDragging = false;
    let offsetX, offsetY;

    const dragHandle = prompt.querySelector('div[style*="cursor: move"]');
    dragHandle.addEventListener('mousedown', (e) => {
        isDragging = true;
        offsetX = e.clientX - prompt.getBoundingClientRect().left;
        offsetY = e.clientY - prompt.getBoundingClientRect().top;

        // 添加拖动时的视觉反馈
        prompt.style.boxShadow = '0 8px 28px rgba(0,0,0,0.28)';
    });

    document.addEventListener('mousemove', (e) => {
        if (isDragging) {
            prompt.style.left = e.clientX - offsetX + 'px';
            prompt.style.top = e.clientY - offsetY + 'px';
            prompt.style.right = 'auto'; // 清除right属性以防止定位冲突
        }
    });

    document.addEventListener('mouseup', () => {
        if (isDragging) {
            isDragging = false;
            // 恢复原来的阴影
            prompt.style.boxShadow = '0 4px 20px rgba(0,0,0,0.2)';
        }
    });

    // 事件监听器
    const openButton = prompt.querySelector('#open-plugin');

    openButton.addEventListener('click', () => {
        clearInterval(countdownInterval); // 清除倒计时
        // 添加点击动画
        prompt.style.transform = 'scale(0.95)';
        setTimeout(() => {
            // 打开插件的逻辑
            chrome.runtime.sendMessage({ action: 'OPEN_PLUGIN' }, (response) => {
                if (chrome.runtime.lastError) {
                    console.error('发送消息失败:', chrome.runtime.lastError);
                } else {
                    console.log('插件已打开');
                }
            });
            // 淡出动画
            prompt.style.opacity = '0';
            prompt.style.transform = 'translateY(-10px)';
            setTimeout(() => {
                prompt.remove();
                currentPrompt = null; // 清除全局变量
            }, 300);
        }, 150);
    });

    closeButton.addEventListener('click', () => {
        clearInterval(countdownInterval); // 清除倒计时
        // 添加关闭动画
        prompt.style.opacity = '0';
        prompt.style.transform = 'translateY(-10px)';
        setTimeout(() => {
            prompt.remove();
            currentPrompt = null; // 清除全局变量
        }, 300);
    });
}

// 初始化
try {
    initializeParser().then(() => {
        // createDraggablePrompt();
    });
} catch (error) {
    console.error('初始化失败:', error);
    showNotification('⚠️ 初始化失败', 'status');
}

// 初始化连接
initializeConnection();