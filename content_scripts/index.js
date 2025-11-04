
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

let enableSound = false;

let ParserName = null

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
    }, 1500);
}

// 根据当前网站URL选择合适的解析器
async function initializeParser() {
    try {


        const url = window.location.href;
        const extensionUrl = chrome.runtime.getURL('');

        if (url.includes('zhipin.com')) {
            ParserName = 'boos'

            const { BossParser } = await import(extensionUrl + 'content_scripts/sites/boss.js');
            currentParser = new BossParser();
            // showNotification('BOSS直聘初始化完成，请前往推荐牛人页面使用-GoodHR', 'status');
            // 检查是否在iframe中
            const isInIframe = window !== window.top;
            // 跳过 about:blank 和非主框架页面

            if (!isInIframe) {
                createDraggablePrompt(); // 只在主框架中创建询问框
            }


        } else if (url.includes('lagou.com')) {
            ParserName = 'lagou'
            const { LagouParser } = await import(extensionUrl + 'content_scripts/sites/lagou.js');
            currentParser = new LagouParser();
            // showNotification('拉勾网初始化完成-GoodHR', 'status');
            createDraggablePrompt();
        } else if (url.includes('lpt.liepin.com')) {
            ParserName = 'liepin'
            const { LiepinParser } = await import(extensionUrl + 'content_scripts/sites/liepin.js');
            currentParser = new LiepinParser();
            // showNotification('猎聘网初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        } else if (url.includes('h.liepin.com')) {
            ParserName = 'hliepin'
            const { HLiepinParser } = await import(extensionUrl + 'content_scripts/sites/hliepin.js');
            currentParser = new HLiepinParser();
            // showNotification('猎聘网初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        }else if (url.includes('employer.58.com')) {
            ParserName = 'employer58'
            const { Employer58Parser } = await import(extensionUrl + 'content_scripts/sites/employer58.js');
            currentParser = new Employer58Parser();
            // showNotification('58同城初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        }  else if (url.includes('zhaopin.com')) {

            ParserName = 'zhilian'
            const { ZhilianParser } = await import(extensionUrl + 'content_scripts/sites/zhilian.js');
            currentParser = new ZhilianParser();
            // showNotification('智联网初始化完成，请前往推荐人才页面使用-GoodHR', 'status');
            createDraggablePrompt();
        }

        if (currentParser) {
            await currentParser.loadSettings();
            console.log('解析器初始化完成');
        } else {
            // console.warn('未找到匹配的解析器，当前URL:', url);
            // throw new Error('未找到匹配的解析器');
        }
    } catch (error) {
        console.error('初始化解析器失败:', error);
        showNotification('⚠️ 初始化解析器失败: ' + error.message, 'status');
        currentParser = null; // 确保失败时设置为null
    }
}

// 添加随机延迟函数
function randomDelay(message2 = "无") {
    // 获取最新的设置值（注意：这里的值已经是秒为单位）
    let currentMin, currentMax;

    if (currentParser?.aiMode) {
        // AI模式：使用AI设置
        currentMin = currentParser?.aiSettings?.scrollDelayMin || 3;
        currentMax = currentParser?.aiSettings?.scrollDelayMax || 5;
    } else {
        // 免费模式：使用原有设置
        currentMin = currentParser?.filterSettings?.scrollDelayMin || 3;
        currentMax = currentParser?.filterSettings?.scrollDelayMax || 5;
    }

    // 使用最新的设置值
    const actualMin = currentMin;
    const actualMax = currentMax;

    // 生成随机延迟（秒）
    const delaySeconds = Math.floor(Math.random() * (actualMax - actualMin + 1) + actualMin);

    // 转换为毫秒
    const delayMs = delaySeconds * 1000;



    sendMessage({
        type: 'LOG_MESSAGE',
        data: {
            message: `随机停止 ${delaySeconds} 秒 ${message2}`,
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

    // console.log('开始自动滚动');



    if (isRunning) return;

    // 检查AI模式的配置要求
    if (currentParser?.aiMode) {
        // AI模式：检查必要的配置
        if (!currentParser?.aiSettings?.aiConfig?.token) {
            console.error('AI模式需要配置Token');
            sendMessage({
                type: 'LOG_MESSAGE',
                data: {
                    message: 'AI模式需要配置Token，请先配置AI设置',
                    type: 'error'
                }
            });
            showNotification('⚠️ AI模式需要配置Token', 'status');
            return;
        }

        if (!currentParser?.aiSettings?.jobDescription || !currentParser.aiSettings.jobDescription.trim()) {
            console.error('AI模式需要填写岗位说明');
            sendMessage({
                type: 'LOG_MESSAGE',
                data: {
                    message: 'AI模式需要填写岗位说明',
                    type: 'error'
                }
            });
            showNotification('⚠️ AI模式需要填写岗位说明', 'status');
            return;
        }
    }

    try {
        isRunning = true;
        lastProcessedPosition = 0;

        // 从 currentParser 获取设置，不要在这里乘以1000
        if (currentParser.aiMode) {
            // AI模式：使用AI设置
            matchLimit = currentParser?.aiSettings?.matchLimit || 200;
            scrollDelayMin = currentParser?.aiSettings?.scrollDelayMin || 3;
            scrollDelayMax = currentParser?.aiSettings?.scrollDelayMax || 5;
            // console.log('AI模式设置:', {
            //     matchLimit,
            //     scrollDelayMin,
            //     scrollDelayMax,
            //     aiSettings: currentParser.aiSettings
            // });
        } else {
            // 免费模式：使用原有设置
            matchLimit = currentParser?.filterSettings?.matchLimit || 200;
            scrollDelayMin = currentParser?.filterSettings?.scrollDelayMin || 3;
            scrollDelayMax = currentParser?.filterSettings?.scrollDelayMax || 5;
            // console.log('免费模式设置:', {
            //     matchLimit,
            //     scrollDelayMin,
            //     scrollDelayMax,
            //     filterSettings: currentParser.filterSettings
            // });
        }

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
            // items可能是数组也可能是字符串，需要循环处理
            // const selector = '.' + currentParser.selectors.items;
            // let elements = Array.from(doc.querySelectorAll(selector));
            let elements = [];


            if (typeof currentParser.selectors.items === 'array') {
                for (const item of currentParser.selectors.items) {
                    const selector = '.' + item;
                    const elements = Array.from(doc.querySelectorAll(selector));

                    if (elements.length > 0) {
                        elements.push(...elements);
                    }
                }
            } else {
                const selector = '.' + currentParser.selectors.items;
                elements = Array.from(doc.querySelectorAll(selector));

                if (elements.length > 0) {
                    elements.push(...elements);
                }
            }

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
                // 如果没找到未处理的元素，检查是否需要翻页
                if (ParserName === 'hliepin' && currentParser && typeof currentParser.shouldNavigateToNextPage === 'function') {
                    const shouldNavigate = currentParser.shouldNavigateToNextPage(elements);
                    
                    if (shouldNavigate) {
                        console.log('准备翻页');
                        await currentParser.clickNextPageButton();
                        // 翻页后等待页面加载
                        await new Promise(resolve => setTimeout(resolve, 3000));
                        // 重新加载解析器设置
                        await currentParser.loadSettings();
                        // 重置滚动位置
                        window.scrollTo(0, 0);
                        lastProcessedPosition = 0;
                        executeScroll();
                        return;
                    }
                }
                
                // 默认行为：向下滚动一段距离
                window.scrollBy({
                    top: 200,
                    behavior: 'smooth'
                });

                // 使用 randomDelay 获取最新的延迟设置
                // await randomDelay("无候选人卡片");
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


// 添加高亮原因标签函数
function addHighlightReason(element, reason, color) {


    // 移除旧的原因标签
    element.querySelector('.goodhr-highlight-reason')?.remove();

    const reasonEl = document.createElement('div');
    reasonEl.className = 'goodhr-highlight-reason';
    reasonEl.textContent = reason;
    reasonEl.style.cssText = `
                position: absolute;
                top: 0;
                left: 0;
                background-color: ${color};
                color: white;
                padding: 1px 12px;
                font-size: 12px;
                border-bottom-right-radius: 8px;
                z-index: 1;
            `;
    element.style.position = 'relative';
    element.appendChild(reasonEl);


    // 根据状态设置不同高亮
    if (reason.includes("已打招呼")) {
        // 已打招呼状态（绿色）
        const contactedStyles = {
            'background-color': '#e8f5e9',
            'border': '2px solid #4caf50',
            'box-shadow': '0 0 10px rgba(76, 175, 80, 0.3)'
        };
        Object.entries(contactedStyles).forEach(([property, value]) => {
            element.style.setProperty(property, value, 'important');
        });
    } else {
        // 未打招呼状态（灰色）
        const notContactedStyles = {
            'background-color': '#f5f5f5',
            'border': '2px solid #9e9e9e',
            'box-shadow': '0 0 10px rgba(158, 158, 158, 0.3)'
        };
        Object.entries(notContactedStyles).forEach(([property, value]) => {
            element.style.setProperty(property, value, 'important');
        });
    }
}


// 处理单个元素的函数
async function processElement(element, doc) {


    try {
        // 发送计数请求到服务器（不等待响应）
        const apiBase = window.GOODHR_CONFIG ? window.GOODHR_CONFIG.API_BASE : 'https://goodhr.58it.cn';
        fetch(`${apiBase}/counter.php`, {
            method: 'GET',
            headers: {
                'Content-Type': 'application/json'
            }
        }).catch(err => {
            // console.log('计数请求失败:', err);
        });

        // 首先检查是否已达到匹配限制
        if (matchCount >= matchLimit) {
            // console.log(`已达到匹配限制 ${matchLimit}，停止处理`);
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

        // 清除之前的高亮和原因标签
        targetElement.querySelector('.goodhr-highlight-reason')?.remove();

        // 应用处理中样式（橙色）
        // const processingStyles = {
        //     'background-color': '#fff3e0',
        //     'border': '2px solid #ffa726',
        //     'position': 'relative',
        //     'box-shadow': '0 0 15px rgba(255, 167, 38, 0.4)',
        //     'transition': 'all 0.3s ease'
        // };

        // Object.entries(processingStyles).forEach(([property, value]) => {
        //     targetElement.style.setProperty(property, value, 'important');
        // });

        const rect = element.getBoundingClientRect();
        lastProcessedPosition = rect.top + rect.height + (doc === document ?
            window.pageYOffset :
            doc.defaultView.pageYOffset);

        const candidates = await currentParser.extractCandidates([element]);

        const resolvedCandidates = candidates;
        if (resolvedCandidates.length > 0) {
            for (let candidate of candidates) {

                let AiMsg = ""
                let simpleCandidateInfo = null
                // 再次检查是否已达到匹配限制
                if (matchCount >= matchLimit) {
                    console.log(`处理候选人过程中达到匹配限制 ${matchLimit}，停止处理`);
                    isRunning = false;
                    stopAutoScroll();
                    return;
                }

                let shouldSkipDelay = false;

                // 第一个决策点：决定是否查看候选人详细信息
                let clickCandidate = false;
                simpleCandidateInfo = await currentParser.getSimpleCandidateInfo(candidate);

                if (currentParser.aiMode) {
                    // AI模式：基于简单信息决定是否查看详细信息
                    let { isok, msg } = await performAIClickDecision(simpleCandidateInfo);
                    AiMsg = msg
                    clickCandidate = isok;

                    if (clickCandidate) {
                    addHighlightReason(targetElement, '已打招呼(' + AiMsg + ')', '#4caf50');

                    }else{
                        addHighlightReason(targetElement, '未打招呼(' + AiMsg + ')', '#9e9e9e');
                    }
                } else {
                    // 免费模式：通过概率决定
                    clickCandidate = currentParser.shouldClickCandidate();
                }

                // 判断是否应该联系候选人
                let shouldContact = false;
                if (clickCandidate) {
                    // 检查是否有打开的候选人页面
                    //关闭候选人弹框

                    let clicked = await currentParser.clickCandidateDetail(element);

                    if (ParserName === 'employer58') {
                        clicked = false
                        shouldContact =true
                                                await randomDelay(`查看候选人详细信息: ${candidate.name}`);
                    }

                    if (clicked) {
                        shouldSkipDelay = true;
                        await randomDelay(`查看候选人详细信息: ${candidate.name}`);

                        let colleagueContactedInfo = null;
                        try {
                            //查询同事沟通过候选人的信息
                            colleagueContactedInfo = await currentParser.queryColleagueContactedInfo(candidate);
                            simpleCandidateInfo += colleagueContactedInfo;

                        } catch (error) {
                            console.error('查询同事沟通过候选人的信息失败:', error);
                        }


                        try {
                            //第二次组装信息
                            let data2 = await currentParser.extractCandidates2(candidate);
                            if(data2){
                                simpleCandidateInfo = data2

                            }
                        } catch (error) {
                            console.error('第二次组装信息失败:', error);
                        }

                        // console.log(candidate);


                        // AI模式：使用AI筛选
                        if (currentParser.aiMode) {

                            // 如果是Boss且第一次AI决策为true，直接打招呼
                            if (ParserName == "boos") {
                                if (clickCandidate) {
                                    shouldContact = true;
                                } else {
                                    shouldContact = false
                                    //不做任何处理，继续下一个候选人
                                }

                            } else {

                                let { isok, msg } = {} = await performAIClickDecision(simpleCandidateInfo);
                                shouldContact = isok;
                                AiMsg = msg
                            }


                        } else {
                            // 免费模式：使用关键词筛选
                            shouldContact = currentParser.filterCandidate(simpleCandidateInfo);
                            if(ParserName === 'employer58'){
                                shouldContact = true
                            }
                        }

                        // 确保详情页完全关闭
                        await currentParser.closeDetail();

                        // 等待一小段时间确保页面完全关闭
                        await new Promise(resolve => setTimeout(resolve, 500));
                    }
                } else {
                    // console.log('不查看候选人详细信息:', candidate.name);
                    await randomDelay("不查看候选人详细信息");

                    // 如果不查看详情，直接使用关键词筛选
                    if (!currentParser.aiMode) {
                        // console.log('使用关键词筛选候选人:', candidate.name);

                        shouldContact = currentParser.filterCandidate(simpleCandidateInfo);
                         if(ParserName === 'employer58'){
                                shouldContact = true
                            }
                    }
                }

                if (shouldContact) {
                    // 再次检查是否已达到匹配限制
                    if (matchCount >= matchLimit) {
                        console.log(`匹配成功但已达到限制 ${matchLimit}，停止处理`);
                        isRunning = false;
                        stopAutoScroll();
                        return;
                    }

                    addHighlightReason(targetElement, '已打招呼(' + AiMsg + ')', '#4caf50');

                    const clicked = await currentParser.clickMatchedItem(element);
                    if (clicked) {
                        matchCount++;
                        console.log(`打招呼成功，当前计数: ${matchCount}/${matchLimit}`);
                        //播放提示音

                        playNotificationSound()
                        
                        // 调用处理沟通功能
                        try {
                            if (currentParser.processCommunication && typeof currentParser.processCommunication === 'function') {
                                await currentParser.processCommunication(candidate);
                            }
                        } catch (error) {
                            console.error('处理沟通功能时出错:', error);
                        }
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
                    addHighlightReason(targetElement, '未打招呼(' + AiMsg + ')', '#9e9e9e');
                }
            }
        }



    } catch (error) {
        console.error('处理元素失败:', error);
        element.removeAttribute('style');
    }


}

function playNotificationSound() {
    if (enableSound) {
        const audio = new Audio(chrome.runtime.getURL('sounds/notification2.mp3'));
        audio.volume = 0.5; // 设置音量
        audio.play().catch(error => console.error('播放提示音失败:', error));
    }
}


// 处理来自popup的消息
chrome.runtime.onMessage.addListener(async (message, sender, sendResponse) => {
    try {
        switch (message.action) {
            case 'START_SCROLL':
                // console.log('收到开始滚动消息:', message);

                // 检查解析器是否已初始化
                if (!currentParser) {
                    console.error('解析器未初始化，无法启动滚动');
                    sendResponse({ status: 'error', message: '解析器未初始化' });
                    return;
                }

                // 更新点击频率设置
                if (message.data.clickFrequency !== undefined) {
                    currentParser.clickCandidateConfig.frequency = message.data.clickFrequency;
                    // console.log('更新点击频率为:', message.data.clickFrequency);
                }
                currentParser.aiMode = false;

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
            case 'SET_AI_MODE':
                break;
            case 'START_AI_SCROLL':

            // console.log('收到开始AI滚动消息:', message);
            

                // 检查解析器是否已初始化
                if (!currentParser) {
                    console.error('解析器未初始化，无法启动AI滚动');
                    sendResponse({ status: 'error', message: '解析器未初始化' });
                    return;
                }

                // 更新AI设置
                if (message.data.clickFrequency !== undefined) {
                    currentParser.clickCandidateConfig.frequency = message.data.clickFrequency;
                    // console.log('更新点击频率为:', message.data.clickFrequency);
                }

                // 设置AI筛选模式
                currentParser.aiMode = true;
                currentParser.aiSettings = {
                    positionName: message.data.positionName,
                    jobDescription: message.data.jobDescription,
                    aiConfig: message.data.aiConfig,
                    matchLimit: message.data.matchLimit,
                    scrollDelayMin: message.data.scrollDelayMin,
                    scrollDelayMax: message.data.scrollDelayMax
                };

                // 直接使用原有的滚动逻辑
                await startAutoScroll();
                sendResponse({ status: 'success' });
                                break;
            case 'SHOW_ADS':
                // 检查广告配置是否已加载
                if (adConfig) {
                    // 检查AI是否过期
                    chrome.storage.local.get(['ai_expire_time'], function(result) {
                        let isAIExpired = false;
                        if (result.ai_expire_time) {

                            const now = new Date();
                            let expireDate = null;
                            try {
                                 expireDate = new Date(result.ai_expire_time + 'T00:00:00');
                                                             isAIExpired = now > expireDate;

                            //         console.log('AI到期时间:', expireDate);
                            // console.log('当前时间:', now);
                            // console.log('是否过期:', isAIExpired);
                            } catch (error) {
                                console.error('解析AI到期时间失败:', error);
                                sendResponse({ status: 'error', message: '解析AI到期时间失败' });
                                isAIExpired = true;
                            }     
                           

                        }
                        
                        // 广告显示条件：AI未过期（或无到期时间）
                        // const shouldShowAd = vip_show|| ;
                        
                        // if (isAIExpired ) {
                            // 显示广告
                            displayAds(isAIExpired);
                            // 标记广告已显示
                            chrome.storage.local.set({adDisplayed: true});
                        // }
                        sendResponse({ status: 'success' });
                    });
                } else {
                    sendResponse({ status: 'error', message: '广告配置未加载' });
                }
                return true; // 异步发送响应

                enableSound = currentParser.aiSettings.enableSound


                // console.log('设置AI模式:', {
                //     aiMode: currentParser.aiMode,
                //     aiSettings: currentParser.aiSettings
                // });

                break;
            case 'STOP_SCROLL':
                stopAutoScroll();
                sendResponse({ status: 'stopped' });
                break;
            case 'REMOVE_ADS':
                // 移除所有广告元素
                removeAds();
                sendResponse({ status: 'success' });
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
                    sendResponse({ status: 'ok' });
                } else {
                    console.error('解析器未初始化');
                    sendResponse({ status: 'error', message: '解析器未初始化' });
                }
                break;
            case 'COMMUNICATION_PROCESS':
                // 处理沟通功能
                if (currentParser) {
                    // 将沟通处理数据传递给解析器
                    if (message.data.companyInfo) {
                        currentParser.companyInfo = message.data.companyInfo;
                    }
                    if (message.data.jobInfo) {
                        currentParser.jobInfo = message.data.jobInfo;
                    }
                    if (message.data.communicationConfig) {
                        currentParser.communicationConfig = message.data.communicationConfig;
                    }
                    if (message.data.runModeConfig) {
                        currentParser.runModeConfig = message.data.runModeConfig;
                    }
                    
                    sendResponse({ status: 'success' });
                } else {
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

// 移除所有广告元素
function removeAds() {
    try {
        // 移除页面中的所有广告元素
        const adElements = document.querySelectorAll('.ad-container, .draggable-ad-container');
        adElements.forEach(adElement => {
            adElement.remove();
        });
        
        // 清除广告显示状态
        chrome.storage.local.remove(['adDisplayed']);
        
        console.log('广告已移除');
    } catch (error) {
        console.error('移除广告失败:', error);
    }
}



// 获取候选人详细信息
async function getCandidateInfo(candidate) {
    try {
        // 构建候选人信息字符串
        let info = `姓名：${candidate.name}\n`;

        if (candidate.age) info += `年龄：${candidate.age}\n`;
        if (candidate.education) info += `学历：${candidate.education}\n`;
        if (candidate.university) info += `学校：${candidate.university}\n`;
        if (candidate.experience) info += `经验：${candidate.experience}\n`;
        if (candidate.salary) info += `期望薪资：${candidate.salary}\n`;
        if (candidate.location) info += `期望地点：${candidate.location}\n`;

        // 添加额外信息
        if (candidate.extraInfo && candidate.extraInfo.length > 0) {
            info += `其他信息：\n`;
            candidate.extraInfo.forEach(item => {
                info += `- ${item.type}: ${item.value}\n`;
            });
        }
        if (candidate.colleagueContactedInfo) {
            info += `同事沟通过候选人的信息：\n${candidate.colleagueContactedInfo}\n`;
        }

        if (candidate.activeText) {
            info += `在线状态：${candidate.activeText}\n`;
        }

        return info;
    } catch (error) {
        console.error('获取候选人信息失败:', error);
        return `姓名：${candidate.name}`;
    }
}



// 轨迹流动API配置
const GUJJI_API_CONFIG = window.GOODHR_CONFIG ? window.GOODHR_CONFIG.GUJJI_API : {
    baseUrl: 'https://api.siliconflow.cn/v1/chat/completions',
    maxTokens: 100,
    temperature: 0.1
};

// 直接发送AI请求到轨迹流动
async function sendDirectAIRequest(prompt, aiConfig) {
    try {
        const model = aiConfig.model;

        const response = await fetch(GUJJI_API_CONFIG.baseUrl, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${aiConfig.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                model: model,
                messages: [
                    {
                        role: 'user',
                        content: prompt
                    }
                ],
                max_tokens: GUJJI_API_CONFIG.maxTokens,
                temperature: GUJJI_API_CONFIG.temperature
            })
        });

        if (!response.ok) {
            throw new Error(`API请求失败，HTTP状态码: ${response.status}`);
        }

        const data = await response.json();
        const aiResponse = data.choices?.[0]?.message?.content;

        if (!aiResponse) {
            throw new Error('AI响应为空');
        }

        return {
            success: true,
            response: aiResponse.trim()
        };
    } catch (error) {
        return {
            success: false,
            error: error.message
        };
    }
}



// 检查AI是否过期
function checkAIExpiration() {
    // 从popup获取AI到期时间
    return new Promise((resolve) => {
        chrome.runtime.sendMessage({
            action: 'CHECK_AI_EXPIRATION'
        }, (response) => {
            if (chrome.runtime.lastError) {
                console.error('检查AI到期时间失败:', chrome.runtime.lastError);
                resolve(false); // 检查失败时允许继续
            } else {
                resolve(response && response.expired);
            }
        });
    });
}

// 第一个AI决策点：决定是否查看候选人详细信息
async function performAIClickDecision(simpleCandidateInfo) {
    try {

//如果是58 就不查看详细信息
                    if (ParserName === 'employer58') {
                        return { isok: true, msg: "58无法筛选" };
                    }

        // 检查AI是否过期
        const isExpired = await checkAIExpiration();
        if (isExpired) {
            alert('AI版本试用期已到期，请前往官网联系作者续费。\n\n官网地址：http://goodhr.58it.cn');
            return false;
        }

        // 显示AI决策动画
        showAIDecisionModal();

        const prompt = buildAIClickPrompt(simpleCandidateInfo);
        const result = await sendDirectAIRequest(prompt, currentParser.aiSettings.aiConfig);

        hideAIDecisionModal();

        if (result.success) {
            const shouldClick = parseAIResponse(result.response);
            return shouldClick;
        } else {
            console.error('AI点击决策失败:', result.error);
            return false; // 失败时默认不点击
        }
    } catch (error) {
        // 确保动画被隐藏
        hideAIDecisionModal();
        console.error('AI点击决策异常:', error);
        return false; // 异常时默认不点击
    }
}

// 构建第一个决策点的AI提示词
function buildAIClickPrompt(simpleCandidateInfo) {
    const customPrompt = currentParser.aiSettings?.aiConfig?.clickPrompt;
    if (customPrompt) {
        return customPrompt
            .replace('${候选人信息}', simpleCandidateInfo)
            .replace('${岗位信息}', currentParser.aiSettings?.jobDescription || '未设置岗位要求');
    }

    // 默认提示语
    return `你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。

重要提示：
1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回"内容与招聘无关 无法解答"。
2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。
3. 必须返回JSON格式，包含decision和reason两个字段。
4. decision字段只能是"是"或"否"。
5. reason字段是决策原因，15个字以内。

岗位要求：
${currentParser.aiSettings?.jobDescription || '未设置岗位要求'}

候选人基本信息：
${simpleCandidateInfo}

请判断是否值得查看这位候选人的详细信息。
返回JSON格式：{"decision":"是","reason":"符合基本要求"}`;
}


// 解析AI响应
function parseAIResponse(response) {
    try {
        // 尝试解析JSON格式
        const jsonMatch = response.match(/\{[^}]*"decision"[^}]*\}/);
        if (jsonMatch) {
            const aiResponse = JSON.parse(jsonMatch[0]);
            const decision = aiResponse.decision === '是';
            const reason = aiResponse.reason || '未提供原因';

            console.log(`AI决策结果: ${aiResponse.decision}, 原因: ${reason}`);

            // 发送日志消息到插件显示
            sendMessage({
                type: 'LOG_MESSAGE',
                data: {
                    message: `AI决策: ${aiResponse.decision} (${reason})`,
                    type: decision ? 'success' : 'info'
                }
            });

            return {
                isok: decision,
                msg: reason
            };
        }
    } catch (error) {
        console.warn('解析JSON失败:', error);
        // JSON解析失败时发送日志并默认返回否
        sendMessage({
            type: 'LOG_MESSAGE',
            data: {
                message: `解析失败 原文: ${response.trim()}`,
                type: 'error'
            }
        });
        return {
            isok: false,
            msg: '解析失败'
        };
    }

    // 兼容旧格式
    const cleanResponse = response.trim().toLowerCase();
    if (cleanResponse.includes('是') || cleanResponse.includes('yes')) {
        // console.log("决策结果 :是");
        sendMessage({
            type: 'LOG_MESSAGE',
            data: {
                message: `AI决策: 是 (旧格式兼容)`,
                type: 'success'
            }
        });
        return true;
    } else if (cleanResponse.includes('否') || cleanResponse.includes('no')) {
        // console.log("决策结果 :否");
        sendMessage({
            type: 'LOG_MESSAGE',
            data: {
                message: `AI决策: 否 (旧格式兼容)`,
                type: 'info'
            }
        });
        return false;
    } else {
        console.warn('AI响应格式异常:', response);
        // 格式异常时发送日志并默认返回否
        sendMessage({
            type: 'LOG_MESSAGE',
            data: {
                message: `格式异常 原文: ${response.trim()}`,
                type: 'error'
            }
        });
        return false;
    }
}



// 创建AI决策动画弹框
function createAIDecisionModal() {
    // 检查是否已存在
    if (document.getElementById('ai-decision-modal')) {
        return document.getElementById('ai-decision-modal');
    }

    const modal = document.createElement('div');
    modal.id = 'ai-decision-modal';
    modal.style.cssText = `
		position: fixed;
		top: 20px;
		right: 20px;
		width: 200px;
		height: 120px;
		background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
		border-radius: 15px;
		box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
		z-index: 10000;
		display: none;
		animation: none;
		overflow: hidden;
	`;

    modal.innerHTML = `
		<div style="
			position: relative;
			width: 100%;
			height: 100%;
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
			color: white;
			font-family: 'Arial', sans-serif;
		">
			<div style="
				width: 40px;
				height: 40px;
				border: 3px solid rgba(255, 255, 255, 0.3);
				border-top: 3px solid white;
				border-radius: 50%;
				animation: spin 1s linear infinite;
				margin-bottom: 10px;
			"></div>
			<div style="
				font-size: 14px;
				font-weight: bold;
				margin-bottom: 5px;
				text-align: center;
			">AI正在思考...</div>
			<div style="
				font-size: 12px;
				opacity: 0.8;
				text-align: center;
			">请稍候</div>
		</div>
	`;

    // 添加CSS动画
    const style = document.createElement('style');
    style.textContent = `
		@keyframes slideInRight {
			from {
				transform: translateX(100%);
				opacity: 0;
			}
			to {
				transform: translateX(0);
				opacity: 1;
			}
		}

		@keyframes slideOutRight {
			from {
				transform: translateX(0);
				opacity: 1;
			}
			to {
				transform: translateX(100%);
				opacity: 0;
			}
		}

		@keyframes spin {
			0% { transform: rotate(0deg); }
			100% { transform: rotate(360deg); }
		}

		@keyframes pulse {
			0%, 100% { transform: scale(1); }
			50% { transform: scale(1.05); }
		}
	`;

    document.head.appendChild(style);
    document.body.appendChild(modal);

    return modal;
}

// 显示AI决策动画
function showAIDecisionModal() {
    const modal = createAIDecisionModal();
    if (modal) {
        // 重置状态
        modal.style.display = 'block';
        modal.style.animation = 'none';
        modal.style.transform = 'translateX(100%)';
        modal.style.opacity = '0';

        // 强制重绘
        modal.offsetHeight;

        // 开始动画
        modal.style.animation = 'slideInRight 0.5s ease-out';
        modal.style.transform = '';
        modal.style.opacity = '';
    }
}

// 隐藏AI决策动画
function hideAIDecisionModal() {
    const modal = document.getElementById('ai-decision-modal');
    if (modal) {
        modal.style.animation = 'slideOutRight 0.5s ease-out';
        setTimeout(() => {
            modal.style.display = 'none';
            // 重置动画状态，为下次显示做准备
            modal.style.animation = 'none';
            modal.style.transform = '';
            modal.style.opacity = '';
        }, 500);
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
        // port.onDisconnect.addListener(() => {
        //     console.log('连接断开，尝试重新连接');
        //     port = null;
        //     setTimeout(initializeConnection, 1000);
        // });
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
                <span style='background-color: #e8f0fe; color: #1a73e8; padding: 2px 6px; border-radius: 10px; font-size: 12px;'>v${window.GOODHR_CONFIG ? window.GOODHR_CONFIG.VERSION : chrome.runtime.getManifest().version}</span>
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
    // 检查是否在iframe中
    const isInIframe = window !== window.top;
    
    // 只加载广告配置，不在这里显示广告
    loadAdConfig();

    initializeParser().then(() => {
        // createDraggablePrompt();
    });
} catch (error) {
    console.error('初始化失败:', error);
    showNotification('⚠️ 初始化失败', 'status');
}

// 监听窗口可见性变化，检测最小化状态
document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
        // 窗口被隐藏（可能是最小化、切换标签页等）
        // console.log('窗口状态变为隐藏');

        // 检查是否真的是最小化（而不是切换标签页）
        setTimeout(() => {
            if (document.hidden && document.visibilityState === 'hidden') {
                // 如果插件正在运行，显示警告
                if (isRunning) {
                    handleWindowMinimized();
                }
            }
        }, 100);
    } else {
        // 窗口变为可见
        // console.log('窗口状态变为可见');
    }
});

// 监听页面卸载事件，清除广告显示状态
window.addEventListener('beforeunload', function() {
    // 清除广告显示状态，以便下次访问页面时广告可以再次显示
    chrome.storage.local.remove(['adDisplayed']);
});

// 处理窗口最小化的函数
function handleWindowMinimized() {
    try {
        // 暂停当前操作
        if (isRunning) {
            console.warn('检测到窗口最小化，暂停插件运行');

            // 发送日志消息
            sendMessage({
                type: 'LOG_MESSAGE',
                data: {
                    message: '检测到窗口最小化，已暂停运行',
                    type: 'warning'
                }
            });

            // 弹出警告提示
            // alert('GoodHR提醒您：为了你的账号安全，请勿在最小化时运行。\n\n如果需要做别的，你可以新开一个浏览器窗口。\n\n插件已自动暂停，请恢复窗口后重新启动。');

            // 停止自动滚动
            // stopAutoScroll();
        }
    } catch (error) {
        console.error('处理窗口最小化时出错:', error);
    }
}

// 初始化连接
initializeConnection();
// 在文件顶部添加广告配置变量定义
let adConfig = null;

// 在文件中添加广告相关函数
// 加载广告配置
async function loadAdConfig() {
    try {
        // 从服务器加载广告配置，与popup中的实现保持一致
        const API_BASE = window.GOODHR_CONFIG ? window.GOODHR_CONFIG.API_BASE : 'https://goodhr.58it.cn';
        const response = await fetch(`${API_BASE}/ads.json?t=${Date.now()}`);
        if (response.ok) {
            adConfig = await response.json();
            if (adConfig.success) {
            } else {
                console.warn('广告配置加载失败');
                adConfig = null;
            }
        } else {
            console.warn('无法获取广告配置');
            adConfig = null;
        }
    } catch (error) {
        console.error('加载广告配置失败:', error);
        adConfig = null;
    }
}

// 创建广告元素
function createAdElement(adData, position) {
    if (!adData) {
        return null;
    }

    const adElement = document.createElement('div');
    adElement.className = `ad-container ad-${position}`;
    adElement.setAttribute('data-ad-id', adData.id);

    // 应用样式
    if (adData.style) {
        Object.assign(adElement.style, {
            background: adData.style.background || '#f8f9fa',
            color: adData.style.color || '#333333',
            border: adData.style.border || '1px solid #e9ecef',
            padding: '8px',
            margin: '8px 0',
            borderRadius: '6px',
            fontSize: '12px',
            cursor: adData.link ? 'pointer' : 'default',
            position: 'relative'
        });
    }

    // 创建内容
    let content = `<div class="ad-title" style="font-weight: 600; margin-bottom: 4px;">${adData.title}</div>`;
    content += `<div class="ad-content">${adData.content}</div>`;

    if (adData.image) {
        content = `<img src="${adData.image}" style="max-width: 100%; height: auto; margin-bottom: 4px;">` + content;
    }

    // 添加关闭按钮
    if (adConfig && adConfig.config && adConfig.config.show_close_button) {
        content += `<button class="ad-close-btn" style="position: absolute; top: 4px; right: 4px; background: rgba(0,0,0,0.2); border: none; color: white; width: 20px; height: 20px; border-radius: 50%; font-size: 12px; cursor: pointer; line-height: 1;">&times;</button>`;
    }

    adElement.innerHTML = content;

    // 添加点击事件
    if (adData.link) {
        adElement.addEventListener('click', (e) => {
            if (e.target.classList.contains('ad-close-btn')) {
                return; // 不处理关闭按钮的点击
            }
            if (adConfig && adConfig.config && adConfig.config.click_tracking) {
                console.log('广告点击:', adData.id);
            }
            window.open(adData.link, '_blank');
        });
    }

    // 添加关闭按钮事件
    const closeBtn = adElement.querySelector('.ad-close-btn');
    if (closeBtn) {
        closeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            adElement.remove();
        });
    }

    return adElement;
}

// 创建可拖拽的广告元素
function createDraggableAdElement(adData) {
    if (!adData) {
        return null;
    }

    const adElement = document.createElement('div');
    adElement.className = 'draggable-ad-container';
    adElement.setAttribute('data-ad-id', adData.id);
    
    // 设置广告的初始位置和尺寸
    Object.assign(adElement.style, {
        position: 'fixed',
        left: adData.x ? adData.x + 'px' : '100px',
        top: adData.y ? adData.y + 'px' : '100px',
        width: adData.width ? adData.width + 'px' : '300px',
        height: adData.height ? adData.height + 'px' : '200px',
        background: adData.style.background || '#f8f9fa',
        color: adData.style.color || '#333333',
        border: adData.style.border || '1px solid #e9ecef',
        borderRadius: '6px',
        fontSize: '12px',
        cursor: 'move',
        zIndex: '10000',
        boxShadow: '0 4px 8px rgba(0,0,0,0.1)',
        overflow: 'hidden'
    });

    // 创建内容
    let content = `<div class="ad-header" style="padding: 0px 8px; background: rgba(0,0,0,0.05); cursor: move; user-select: none; display: flex; justify-content: space-between; align-items: center;">`;
    content += `<div class="ad-title" style="font-weight: 600;">${adData.title}</div>`;
    if (adConfig && adConfig.config && adConfig.config.show_close_button) {
        content += `<button class="ad-close-btn" style="background: rgba(0,0,0,0.2); border: none; color: white; width: 20px; height: 20px; border-radius: 50%; font-size: 12px; cursor: pointer; line-height: 1;">&times;</button>`;
    }
    content += `</div>`;
    content += `<div class="ad-content" style="padding: 0px 8px;  overflow-y: auto;cursor: pointer; ">${adData.content}</div>`;
    
    console.log(adData);
    
    // 添加底部文字（如果有）
    if (adData.bottom_content) {
        content += `<div class="ad-bottom-content" style="line-height: 1.4;padding: 0px 2px; font-size: 10px; color: #666; border-top: 1px solid #eee; background: rgba(0,0,0,0.02);">${adData.bottom_content||'免费版运行时无法关闭该广告(可拖拽)、如需关闭 请刷新浏览器或停止运行或升级AI版'}</div>`;
    }

    if (adData.image) {
        content = `<img src="${adData.image}" style="max-width: 100%; height: auto; margin-bottom: 4px;">` + content;
    }

    adElement.innerHTML = content;

    // 添加拖拽功能
    let isDragging = false;
    let currentX;
    let currentY;
    let initialX;
    let initialY;
    let xOffset = 0;
    let yOffset = 0;
    let dragStartTime = 0;

    const dragHeader = adElement.querySelector('.ad-header');
    
    if (dragHeader) {
        dragHeader.addEventListener('mousedown', dragStart);
        document.addEventListener('mouseup', dragEnd);
        document.addEventListener('mousemove', drag);
    }

    function dragStart(e) {
        // 只有在标题栏上点击才开始拖拽
        if (e.target !== dragHeader && !dragHeader.contains(e.target)) {
            return;
        }
        
        dragStartTime = Date.now();
        initialX = e.clientX - xOffset;
        initialY = e.clientY - yOffset;
        isDragging = true;
        e.preventDefault(); // 防止默认行为
    }

    function dragEnd(e) {
        // 计算拖拽持续时间
        const dragDuration = Date.now() - dragStartTime;
        initialX = currentX;
        initialY = currentY;
        isDragging = false;
        
        // 如果拖拽时间很短（小于200ms）且移动距离很小，则认为是点击而不是拖拽
        if (dragDuration < 200) {
            const moveDistance = Math.sqrt(Math.pow(xOffset, 2) + Math.pow(yOffset, 2));
            if (moveDistance < 5) {
                isDragging = false; // 重置拖拽状态
            }
        }
    }

    function drag(e) {
        if (isDragging) {
            e.preventDefault();
            currentX = e.clientX - initialX;
            currentY = e.clientY - initialY;

            xOffset = currentX;
            yOffset = currentY;

            setTranslate(currentX, currentY, adElement);
        }
    }

    function setTranslate(xPos, yPos, el) {
        el.style.transform = `translate3d(${xPos}px, ${yPos}px, 0)`;
    }

    // 添加点击事件
    if (adData.link) {
        adElement.addEventListener('click', (e) => {
            // 阻止拖拽后的点击事件
            if (isDragging) {
                e.preventDefault();
                e.stopPropagation();
                return;
            }
            
            // 不处理关闭按钮和标题栏的点击
            if (e.target.classList.contains('ad-close-btn') || 
                e.target.classList.contains('ad-header') || 
                (e.target.parentElement && e.target.parentElement.classList.contains('ad-header'))) {
                e.preventDefault();
                e.stopPropagation();
                return;
            }
            
            if (adConfig && adConfig.config && adConfig.config.click_tracking) {
                console.log('广告点击:', adData.id);
            }
            window.open(adData.link, '_blank');
        });
    }

    // 添加关闭按钮事件
    const closeBtn = adElement.querySelector('.ad-close-btn');
    if (closeBtn) {
        closeBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            adElement.remove();
        });
    }

    return adElement;
}

// 显示广告
function displayAds(isAIExpired) {
    // 检查是否在iframe中，如果在iframe中则不显示广告
    const isInIframe = window !== window.top;

    
    if (isInIframe) {
        console.log('在iframe中，跳过广告显示');
        return;
    }
    
    if (!adConfig || !adConfig.success || !adConfig.ads) {
        return;
    }

    // 处理页面广告 (pageAds)
    if (adConfig.ads.pageAds && Array.isArray(adConfig.ads.pageAds)) {
        adConfig.ads.pageAds.forEach(adData => {
            // 根据概率决定是否显示广告
            console.log('isAIExpired:', isAIExpired, 'vip_show:', adData.vip_show);
            
            if (isAIExpired ||(!isAIExpired && adData.vip_show)) {
                const adElement = createDraggableAdElement(adData);
                if (adElement) {
                    document.body.appendChild(adElement);
                }
            }
        });
    }
    
    // 保留原有的广告显示逻辑（用于非页面广告）
    // 查找页面上的合适位置插入广告
    // 这里我们假设在页面中查找具有特定类名或属性的元素来插入广告
    const targetElements = document.querySelectorAll('[class*="list"], [class*="container"], [class*="content"]');
    
    if (targetElements.length > 0) {
        // 插入top广告到第一个目标元素前
        // if (adConfig.ads.top && shouldShowAd(adConfig.ads.top.show_probability)) {
        //     const topAd = createAdElement(adConfig.ads.top, 'top');
        //     if (topAd) {
        //         targetElements[0].parentNode.insertBefore(topAd, targetElements[0]);
        //     }
        // }

        // // 插入middle广告到中间的目标元素后
        // if (adConfig.ads.middle && targetElements.length > 1 && shouldShowAd(adConfig.ads.middle.show_probability)) {
        //     const middleAd = createAdElement(adConfig.ads.middle, 'middle');
        //     if (middleAd) {
        //         targetElements[Math.floor(targetElements.length/2)].appendChild(middleAd);
        //     }
        // }

        // // 插入bottom广告到最后一个目标元素后
        // if (adConfig.ads.bottom && shouldShowAd(adConfig.ads.bottom.show_probability)) {
        //     const bottomAd = createAdElement(adConfig.ads.bottom, 'bottom');
        //     if (bottomAd) {
        //         targetElements[targetElements.length-1].appendChild(bottomAd);
        //     }
        // }
    }
}

// 判断是否显示广告（基于概率）
function shouldShowAd(probability) {
    if (probability === undefined) return true;
    return Math.random() * 100 < probability;
}