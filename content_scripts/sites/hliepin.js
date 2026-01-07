import { BaseParser } from './common.js';

class HLiepinParser extends BaseParser {
    constructor() {
        super();
        this.selectors = {
            container: 'recommandResumes--',
            items: 'no-hover-tr',
            name: 'new-resume-personal-name',
            age: 'personal-detail-age',
            education: 'J1lRR',
            university: 'J1lRR',
            description: 'new-resume-personal-expect',
            clickTarget: 'ant-btn ant-btn-default ant-btn-lg lp-ant-btn-light',
            confirmClickTarget: 'ant-btn ant-btn-link ant-btn-lg btn-cancel directly-open-chat-btn',
            extraSelectors: 'J1lRR',
            onlineStatus: 'new-resume-offline',
            // 分页相关选择器
            nextPageButton: 'ant-pagination-next',
            paginationContainer: 'ant-pagination',
            disabledClass: 'ant-pagination-disabled',
            continueButton:['ant-btn ant-btn-default ant-btn-lg lp-ant-btn-light'],
            phoneButton:['ant-btn ant-btn-primary __im_basic__basic-input-action','im-ui-action-button action-item'],
        };

        this.urlInfo = {
            url: 'https://www.liepin.com/zhaopin/?key=python',
            site: '推荐人才'
        };

        // 添加猎聘特定的详情页选择器
        this.detailSelectors = {
            detailLink: 'tlog-common-resume-card', // 点击姓名打开详情
            closeButton: 'closeBtn--', // 关闭按钮
            viewButton: '.ant-lpt-btn.ant-lpt-btn-primary' // 查看按钮
        };
    }


    /**
     * 索要绑定手机号
     * @param {*} phone 是否索要手机号
     * @param {*} wechat 是否索要微信号
     * @param {*} resume 是否索要简历附件
     * @param {*} candidate 候选人信息
     * @param {*} element 包含所有元素的父元素
     * @returns 
     */
    async collectPhoneWechatResume(phone, wechat, resume, candidate,element) {
        try {

            // for(let i=0;i<this.selectors.continueButton.length;i++){
            //     //先点击继续沟通
            //     const continueButton = this.getElementByClassPrefix(element, this.selectors.continueButton[i]);
                
            //     if (continueButton) {
            //         continueButton.click();
            //     } else {
            //         console.error('未找到继续沟通按钮');
            //         return null;
            //     }
            // }
            await new Promise(resolve => setTimeout(resolve, 2000));
            let buttonList = []
            for(let i=0;i<this.selectors.phoneButton.length;i++){
            //先点击继续沟通
            const continueButton = await this.CommonGetElementsByClassName(element, this.selectors.phoneButton[i]);
            console.log(continueButton);
                if(continueButton!=null&&continueButton.length>0){
                    buttonList=(continueButton);
                }
            }

            //  buttonList = document.getElementsByClassName(this.selectors.phoneButton);
            console.log(buttonList);
            if(phone){
            console.log('索要手机号');
             const phoneButton = buttonList[0];
            if (phoneButton) {
                phoneButton.click();
                //点击确定按钮
                await new Promise(resolve => setTimeout(resolve, 800));
                
                const confirmButton = await this.CommonQuerySelector(document, `[class^="ant-im-modal-confirm-btns"]`);
                if(confirmButton){
                    const confirmButton1 = this.CommonQuerySelectorAll(confirmButton,`[class^="ant-im-btn ant-im-btn-primary"]`);
                        for(let j=0;j<confirmButton1.length;j++){
                            confirmButton1[j].click();
                        }
                    }
                }

            }
            if(wechat){
                console.log('索要微信号');
                const wechatButton = buttonList[1];
                if (wechatButton) {
                    wechatButton.click();
                    //点击确定按钮
                await new Promise(resolve => setTimeout(resolve, 800));
                
                const confirmButton = await this.CommonQuerySelector(document, `[class^="ant-im-modal-confirm-btns"]`);
                if(confirmButton){
                    const confirmButton1 = this.CommonQuerySelectorAll(confirmButton,`[class^="ant-im-btn ant-im-btn-primary"]`);
                       for(let j=0;j<confirmButton1.length;j++){
                        confirmButton1[j].click();
                       }
                    }
                }
            }
            if(resume){
                console.log('索要简历附件');
                const resumeButton = buttonList[2];
                if (resumeButton) {
                    resumeButton.click();
                    //点击确定按钮
                await new Promise(resolve => setTimeout(resolve, 800));
               
                const confirmButton = await this.CommonQuerySelector(document, `[class^="ant-im-modal-confirm-btns"]`);
                if(confirmButton){
                    const confirmButton1 = this.CommonQuerySelectorAll(confirmButton,`[class^="ant-im-btn ant-im-btn-primary"]`);
                       for(let j=0;j<confirmButton1.length;j++){
                        confirmButton1[j].click();
                       }
                    }
                }
            }
            //等待2秒
            await new Promise(resolve => setTimeout(resolve, 1000));
            console.log('等待2秒');
            
            // await randomDelay("确认索要信息");
            //确认索要信息 可能会有多个弹框，需要循环点击确认
        //     const confirmButtons = document.querySelectorAll(`[class^="${this.selectors.confirmSuoIndoButton}"]`);
        // if (!confirmButtons) {
        //     confirmButtons = document.querySelectorAll(`[class*="${this.selectors.confirmSuoIndoButton}"]`);
        // }
        //     console.log('确认按钮数量:', confirmButtons.length);
        //     if (confirmButtons.length > 0) {
        //         confirmButtons.forEach(button => button.click());
        //     }


            //关闭按钮
            // const confirmButton = document.querySelector(`[style^="color: rgb(51, 51, 51); cursor: pointer;"]`);
            // if (confirmButton) {
            //     confirmButton.click();
            // }
        } catch (error) {
            console.error('索要信息失败:', error);
            return null;
        }
    }

    /**
     * 检查是否有消息提示，有就开始处理
     * @param {*} element 
     * @returns 
     */
    async checkMessageTip(element){
        return false;
    }

    // 添加sendMessage函数用于与index.js通信
    sendMessage(message) {
        if (typeof chrome !== 'undefined' && chrome.runtime && chrome.runtime.sendMessage) {
            chrome.runtime.sendMessage(message);
        }
    }
    // 检查是否到达页面底部
    isAtBottomOfPage() {
        const scrollTop = window.pageYOffset || document.documentElement.scrollTop;
        const windowHeight = window.innerHeight;
        const documentHeight = document.documentElement.scrollHeight;
        
        // 距离底部200px内认为到达底部
        const isNearBottom = scrollTop + windowHeight >= documentHeight - 200;
        
        // 检查是否有加载更多或无更多数据的提示
        const hasNoMoreData = document.querySelector('[class*="no-more"], [class*="no-data"], [class*="end"]');
        
        // 检查是否有加载中的指示器
        const hasLoadingIndicator = document.querySelector('[class*="loading"], [class*="spinner"]');
        
        console.log(`页面滚动位置检查: scrollTop=${scrollTop}, windowHeight=${windowHeight}, documentHeight=${documentHeight}`);
        console.log(`是否接近底部: ${isNearBottom}, 是否无更多数据: ${!!hasNoMoreData}, 是否正在加载: ${!!hasLoadingIndicator}`);
        
        return isNearBottom && !hasLoadingIndicator;
    }

    // 检查是否有下一页按钮且可用
    hasNextPageButton() {
        const nextPageButton = document.querySelector(`[class*="${this.selectors.nextPageButton}"]`);
        if (!nextPageButton) {
            return false;
        }
        
        // 检查按钮是否被禁用
        return !nextPageButton.classList.contains(this.selectors.disabledClass);
    }

    // 点击下一页按钮
    async clickNextPageButton() {
        try {
            const nextPageButton = document.querySelector(`[class*="${this.selectors.nextPageButton}"]`);
            if (nextPageButton && !nextPageButton.classList.contains(this.selectors.disabledClass)) {
                console.log('找到下一页按钮，准备点击');
                nextPageButton.click();
                
                // 等待页面开始加载
                await new Promise(resolve => setTimeout(resolve, 1000));
                
                // 等待页面加载完成，检查是否有候选人元素出现
                let pageLoaded = false;
                let attempts = 0;
                const maxAttempts = 10; // 最多尝试10次，每次间隔1秒
                
                while (!pageLoaded && attempts < maxAttempts) {
                    // 检查是否有候选人元素
                    const items = document.querySelectorAll(`[class*="${this.selectors.items}"]`);
                    if (items.length > 0) {
                        console.log(`页面加载完成，找到 ${items.length} 个候选人元素`);
                        pageLoaded = true;
                    } else {
                        console.log(`等待页面加载... 尝试 ${attempts + 1}/${maxAttempts}`);
                        await new Promise(resolve => setTimeout(resolve, 1000));
                        attempts++;
                    }
                }
                
                if (!pageLoaded) {
                    console.warn('页面加载超时，但继续执行');
                }
                
                // 重置滚动位置
                window.scrollTo(0, 0);
                
                // 通过发送消息给index.js来重置lastProcessedPosition
                this.sendMessage({
                    action: 'RESET_POSITION',
                    data: {}
                });
                
                console.log('已点击下一页按钮，等待新页面加载');
                return true;
            }else{
                console.log("下一页按钮不可用");
            }
            return false;
        } catch (error) {
            console.error('点击下一页按钮失败:', error);
            return false;
        }
    }

    // 检查是否需要翻页
    shouldNavigateToNextPage(elements) {
        // 如果没有找到任何元素，或者已经到达页面底部，则可能需要翻页
        // if (elements.length === 0 || this.isAtBottomOfPage()) {

        //     if(this.hasNextPageButton()){
        //         this.clickNextPageButton()
        //     }
        //     return this.hasNextPageButton();
        // }
        console.log("检测是否需要分页");
        
        return true;
    }

    /**
     * 查找同事沟通记录
     * @param {*} data
     * @returns
     */
    queryColleagueContactedInfo(data) {


        return ""
    }
    getElementByClassPrefix(parent, prefix) {
        

        let element = parent.querySelector(`[class^="${prefix}"]`);
        if (!element) {
            element = parent.querySelector(`[class*="${prefix}"]`);
        }
        return element;
    }

    /**
     * 构建第一次候选人信息
     * @param {*} element
     * @returns
     */
    getSimpleCandidateInfo(data) {

        //等待1秒，确保元素加载完成
        // 等待1秒，确保元素加载完成
        new Promise(resolve => setTimeout(resolve, 1000));
      
        
        let data2 = ""
        data2 += "姓名：" + data.name + "\n"
        data2 += "年龄：" + data.age + "\n"
        data2 += "学历：" + data.education + "\n"
        data2 += "学校：" + data.university + "\n"
        data2 += "描述：" + data.description + "\n"

        data.extraInfo.forEach(item => {

            data2 += "额外信息：" + item.type + " " + item.value + "\n"

        });


        return data2;
    }
    async extractCandidates2(data) {
        return new Promise(async (resolve, reject) => {
            // 存储所有响应
            const responses = [];
            let pollCount = 0;
            const maxPolls = 20; // 最多轮询20次，每次间隔500ms，总共10秒
            
            console.log('开始轮询localStorage中的候选人信息...');
            
            // 轮询函数
            const pollLocalStorage = () => {
                try {
                    const storedData = localStorage.getItem('goodhr-candidate-detail');
                    if (storedData) {
                        const data = JSON.parse(storedData);
                        // 检查数据类型和时效性
                        if (data.source === 'goodhr-plugin' && 
                            data.type === 'candidate-detail-response') {
                            responses.push(data);

                            
                            // 读取后立即删除缓存
                            localStorage.removeItem('goodhr-candidate-detail');
                            // console.log('已删除localStorage中的候选人信息缓存');
                            
                            // 处理获取到的响应
                            const matchedResponse = this.findMatchingResponse(responses, data);
                            resolve(matchedResponse);
                            return;
                        }
                    }
                    
                    pollCount++;
                    if (pollCount >= maxPolls) {
                        reject(new Error('获取候选人详细信息超时，未收到任何响应'));
                        return;
                    }
                    
                    // 继续轮询
                    setTimeout(pollLocalStorage, 500);
                } catch (e) {
                    console.error('从localStorage获取数据失败:', e);
                    pollCount++;
                    if (pollCount >= maxPolls) {
                        reject(new Error('获取候选人详细信息时出错'));
                        return;
                    }
                    setTimeout(pollLocalStorage, 500);
                }
            };
            
            // 开始轮询
            pollLocalStorage();
        });
    }
    
    // 通过名字匹配找到正确的响应
    findMatchingResponse(responses, originalData) {
        // console.log('候选人信息:', responses[0].data.textContent);
        
        return responses[0].data.textContent;
    }


    // 通用元素查找函数 - 增强版本
    findElementsByMultipleMethods(selector, options = {}) {
        const {
            timeout = 0,  // 默认不等待
            maxRetries = 0,  // 默认不重试
            retryInterval = 500,  // 重试间隔
            includeHidden = false,  // 是否包含隐藏元素
            methods = ['all']  // 要使用的查找方法
        } = options;
        
        // 如果设置了超时，则使用等待模式
        if (timeout > 0) {
            return this.waitForElements(selector, { timeout, interval: retryInterval });
        }
        
        // 方法1: querySelectorAll
        if (methods.includes('all') || methods.includes('querySelectorAll')) {
            try {
                const elements = document.querySelectorAll(selector);
                let filteredElements = Array.from(elements);
                
                // 过滤隐藏元素（如果需要）
                if (!includeHidden) {
                    filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                }
                
                if (filteredElements.length > 0) {
                    // console.log(`方法1 (querySelectorAll) 找到 ${filteredElements.length} 个元素`);
                    return { elements: filteredElements, method: 'querySelectorAll' };
                }
            } catch (e) {
                console.log(`方法1 (querySelectorAll) 查询失败:`, e);
            }
        }
        
        // 方法2: getElementsByClassName (如果是类选择器)
        if ((methods.includes('all') || methods.includes('getElementsByClassName')) && selector.startsWith('.')) {
            try {
                const className = selector.substring(1);
                const elements = document.getElementsByClassName(className);
                let filteredElements = Array.from(elements);
                
                // 过滤隐藏元素（如果需要）
                if (!includeHidden) {
                    filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                }
                
                if (filteredElements.length > 0) {
                    console.log(`方法2 (getElementsByClassName) 找到 ${filteredElements.length} 个元素`);
                    return { elements: filteredElements, method: 'getElementsByClassName' };
                }
            } catch (e) {
                console.log(`方法2 (getElementsByClassName) 查询失败:`, e);
            }
        }
        
        // 方法3: getElementsByTagName (如果是标签选择器)
        if ((methods.includes('all') || methods.includes('getElementsByTagName')) && /^[a-zA-Z]/.test(selector)) {
            try {
                const elements = document.getElementsByTagName(selector);
                let filteredElements = Array.from(elements);
                
                // 过滤隐藏元素（如果需要）
                if (!includeHidden) {
                    filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                }
                
                if (filteredElements.length > 0) {
                    console.log(`方法3 (getElementsByTagName) 找到 ${filteredElements.length} 个元素`);
                    return { elements: filteredElements, method: 'getElementsByTagName' };
                }
            } catch (e) {
                console.log(`方法3 (getElementsByTagName) 查询失败:`, e);
            }
        }
        
        // 方法4: XPath查找
        if (methods.includes('all') || methods.includes('xpath')) {
            try {
                // 将CSS选择器转换为简单XPath
                let xpath = '';
                if (selector.startsWith('.')) {
                    // 类选择器
                    const className = selector.substring(1);
                    xpath = `//*[contains(@class, '${className}')]`;
                } else if (selector.startsWith('#')) {
                    // ID选择器
                    const id = selector.substring(1);
                    xpath = `//*[@id='${id}']`;
                } else if (/^[a-zA-Z]/.test(selector)) {
                    // 标签选择器
                    xpath = `//${selector}`;
                }
                
                if (xpath) {
                    const result = document.evaluate(xpath, document, null, XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null);
                    const elements = [];
                    for (let i = 0; i < result.snapshotLength; i++) {
                        elements.push(result.snapshotItem(i));
                    }
                    
                    let filteredElements = elements;
                    
                    // 过滤隐藏元素（如果需要）
                    if (!includeHidden) {
                        filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                    }
                    
                    if (filteredElements.length > 0) {
                        console.log(`方法4 (XPath) 找到 ${filteredElements.length} 个元素`);
                        return { elements: filteredElements, method: 'xpath' };
                    }
                }
            } catch (e) {
                console.log(`方法4 (XPath) 查询失败:`, e);
            }
        }
        
        // 方法5: 通过文本内容查找
        if (methods.includes('all') || methods.includes('textSearch')) {
            try {
                // 提取选择器中的文本关键词（如果有的话）
                const keywords = selector.match(/[^\s.#\[\]]+/g) || [];
                if (keywords.length > 0) {
                    const allElements = Array.from(document.body.getElementsByTagName('*'));
                    const elements = allElements.filter(el => {
                        const text = el.textContent || el.innerText || '';
                        return keywords.some(keyword => text.includes(keyword));
                    });
                    
                    let filteredElements = elements;
                    
                    // 过滤隐藏元素（如果需要）
                    if (!includeHidden) {
                        filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                    }
                    
                    if (filteredElements.length > 0) {
                        console.log(`方法5 (文本搜索) 找到 ${filteredElements.length} 个元素`);
                        return { elements: filteredElements, method: 'textSearch' };
                    }
                }
            } catch (e) {
                console.log(`方法5 (文本搜索) 查询失败:`, e);
            }
        }
        
        // 方法6: DOM遍历查找
        if (methods.includes('all') || methods.includes('domTraversal')) {
            try {
                const elements = [];
                const walk = document.createTreeWalker(
                    document.body,
                    NodeFilter.SHOW_ELEMENT,
                    {
                        acceptNode: function(node) {
                            // 检查节点是否匹配选择器
                            try {
                                if (node.matches && node.matches(selector)) {
                                    return NodeFilter.FILTER_ACCEPT;
                                }
                            } catch (e) {
                                // 忽略不支持的选择器
                            }
                            return NodeFilter.FILTER_SKIP;
                        }
                    }
                );
                
                let node;
                while (node = walk.nextNode()) {
                    elements.push(node);
                }
                
                let filteredElements = elements;
                
                // 过滤隐藏元素（如果需要）
                if (!includeHidden) {
                    filteredElements = filteredElements.filter(el => this.isElementVisible(el));
                }
                
                if (filteredElements.length > 0) {
                    console.log(`方法6 (DOM遍历) 找到 ${filteredElements.length} 个元素`);
                    return { elements: filteredElements, method: 'domTraversal' };
                }
            } catch (e) {
                console.log(`方法6 (DOM遍历) 查询失败:`, e);
            }
        }
        
        console.log(`未能找到目标元素`);
        return { elements: [], method: 'none', message: '未找到元素' };
    }

        // 辅助函数：检查元素是否可见
    isElementVisible(element) {
        if (!element) return false;
        
        // 检查元素本身是否可见
        const style = window.getComputedStyle(element);
        if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
            return false;
        }
        
        // 检查元素是否在视口内
        const rect = element.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) {
            return false;
        }
        
        return true;
    }

    // 辅助函数：等待元素出现
    waitForElements(selector, options = {}) {
        const { timeout = 3000, interval = 100 } = options;
        return new Promise((resolve) => {
            const startTime = Date.now();
            
            const check = () => {
                const elements = document.querySelectorAll(selector);
                const visibleElements = Array.from(elements).filter(el => this.isElementVisible(el));
                
                if (visibleElements.length > 0) {
                    resolve({ elements: visibleElements, method: 'waitForElements' });
                } else if (Date.now() - startTime > timeout) {
                    resolve({ elements: [], method: 'waitForElements', message: '超时未找到元素' });
                } else {
                    setTimeout(check, interval);
                }
            };
            
            check();
        });
    }

    extractCandidates(elements = null) {


        const candidates = [];
        let items;

        try {
            if (elements) {
                items = elements;
            } else {
                items = document.querySelectorAll(`[class^="${this.selectors.items}"], [class*=" ${this.selectors.items}"]`);

                if (items.length === 0) {
                    console.error('未找到任何候选人元素!');
                    console.log('当前使用的选择器前缀:', this.selectors.items);
                    throw new Error('未找到任何候选人元素，请检查选择器是否正确');
                }
            }


            Array.from(items).forEach((item, index) => {
                // this.highlightElement(item, 'processing');

                try {

                    const nameElement = this.getElementByClassPrefix(item, this.selectors.name);                    
                    const ageElement = this.getElementByClassPrefix(item, this.selectors.age);
                    const educationElement = this.getElementByClassPrefix(item, this.selectors.education);
                    const universityElement = this.getElementByClassPrefix(item, this.selectors.university);
                    const descriptionElement = this.getElementByClassPrefix(item, this.selectors.description);
                    const extraInfo = this.getElementByClassPrefix(item, this.selectors.extraSelectors);
                    //在线状态
                    const onlineStatusElement = this.getElementByClassPrefix(item, this.selectors.onlineStatus);

                    const candidate = {
                        name: nameElement?.textContent?.trim() + '年龄:' + ageElement?.textContent,
                        age: parseInt(ageElement?.textContent) || 0,
                        education: educationElement?.textContent?.trim() || '',
                        university: universityElement?.textContent?.trim() || '',
                        description: descriptionElement?.textContent?.trim() || item.textContent?.trim() || '',
                        extraInfo: [
                            {type: '在线状态', value: onlineStatusElement?.textContent?.trim() || ''},
                            {type: '求职期望', value: descriptionElement?.textContent?.trim() || ''},
                        ] || [],
                    };


                    if (candidate.name) {
                        candidates.push(candidate);
                        // this.highlightElement(item, 'matched');
                    } else {
                        console.log('未能提取到有效的候选人信息');
                    }
                } catch (error) {
                    console.error(`处理第 ${index + 1} 个候选人时出错:`, error);
                    this.clearHighlight(item);
                }
            });

        } catch (error) {
            console.error('提取候选人信息失败:', error);
            throw error;
        }

        

        return candidates;
    }

    // 实现点击打招呼按钮
    async clickMatchedItem(element) {
        try {
            const clickElement = this.getElementByClassPrefix(element, this.selectors.clickTarget);
            if (clickElement) {
                clickElement.click();
                // return true;
            } else {
                console.error('未找到可点击的元素');
                return false;
            }
//等待500毫秒
             await new Promise(resolve => setTimeout(resolve, 1000));

             // 点击确认按钮
             const confirmClickElement = this.getElementByClassPrefix(document, this.selectors.confirmClickTarget);
             console.log('确认点击元素:', confirmClickElement);
             if (confirmClickElement) {
                confirmClickElement.click();
                return true;
             } else {
                console.error('未找到确认点击的元素');
                return false;
             }

        } catch (error) {
            console.error('点击元素时出错:', error);
            return false;
        }
    }

    // 实现点击候选人详情方法
    async clickCandidateDetail(element) {
        try {
            // 在点击候选人之前，添加时间戳到localStorage
            const clickTimestamp = Date.now();
            localStorage.setItem('goodhr-candidate-click-timestamp', clickTimestamp.toString());
            console.log('已添加候选人点击时间戳:', clickTimestamp);
            
            // 首先尝试点击姓名链接
            const detailLink = this.getElementByClassPrefix(element, this.detailSelectors.detailLink);

            if (detailLink) {
                detailLink.click();
                return true;
            }else{
             console.log('未找到详情链接');
            return false;

            }

        } catch (error) {
            console.error('点击候选人详情失败:', error);
            return false;
        }
    }

    // 实现关闭详情方法
    async closeDetail() {
        try {
            // 等待关闭按钮出现
            await new Promise(resolve => setTimeout(resolve, 500));

            // 查找关闭按钮

            const closeButton = document.querySelector(`[class*="${this.detailSelectors.closeButton}"]`);

            if (closeButton) {
                console.log('找到关闭按钮');
                closeButton.click();
                return true;
            }

            console.log('未找到关闭按钮');
            return false;
        } catch (error) {
            console.error('关闭详情失败:', error);
            return false;
        }
    }



      // 处理沟通功能
    async processCommunication(candidate) {
        try {
            // 检查是否启用了沟通处理功能
            if (!this.communicationConfig || !this.communicationConfig.communicationEnabled) {
                console.log('沟通处理功能未启用');
                return false;
            }

            // 获取候选人详情
            const candidateInfo = this.getSimpleCandidateInfo(candidate);
            
            // 构建沟通内容
            let communicationContent = "";
            
            // 添加公司信息
            if (this.companyInfo) {
                communicationContent += `公司名称：${this.companyInfo.name || ''}\n`;
                communicationContent += `公司地址：${this.companyInfo.address || ''}\n`;
                communicationContent += `公司规模：${this.companyInfo.size || ''}\n`;
                communicationContent += `公司类型：${this.companyInfo.type || ''}\n`;
                communicationContent += `所属行业：${this.companyInfo.industry || ''}\n`;
                communicationContent += `公司介绍：${this.companyInfo.description || ''}\n`;
                communicationContent += `公司官网：${this.companyInfo.website || ''}\n`;
                communicationContent += `联系电话：${this.companyInfo.phone || ''}\n`;
                communicationContent += `联系邮箱：${this.companyInfo.email || ''}\n\n`;
            }
            
            // 添加岗位信息
            if (this.jobInfo) {
                communicationContent += `岗位名称：${this.jobInfo.title || ''}\n`;
                communicationContent += `岗位职责：${this.jobInfo.duty || ''}\n`;
                communicationContent += `岗位要求：${this.jobInfo.requirement || ''}\n`;
                communicationContent += `经验要求：${this.jobInfo.experience || ''}\n`;
                communicationContent += `学历要求：${this.jobInfo.education || ''}\n`;
                communicationContent += `薪资范围：${this.jobInfo.salary || ''}\n`;
                communicationContent += `工作地点：${this.jobInfo.location || ''}\n`;
                communicationContent += `工作性质：${this.jobInfo.type || ''}\n`;
                communicationContent += `岗位类别：${this.jobInfo.category || ''}\n`;
                communicationContent += `岗位描述：${this.jobInfo.description || ''}\n\n`;
            }
            
            // 添加候选人信息
            communicationContent += `候选人信息：\n${candidateInfo}\n\n`;
            
            // 根据配置决定是否自动发送招呼
            if (this.communicationConfig.autoSendGreeting) {
                // 这里应该实现自动发送招呼的逻辑
                console.log('自动发送招呼功能已启用');
                // TODO: 实现自动发送招呼的具体逻辑
            }
            
            // 根据配置决定是否自动处理简历
            if (this.communicationConfig.autoProcessResume) {
                // 这里应该实现自动处理简历的逻辑
                console.log('自动处理简历功能已启用');
                // TODO: 实现自动处理简历的具体逻辑
            }
            
            // 收集联系方式（如果启用）
            if (this.communicationConfig.collectContactMethods) {
                // 这里应该实现收集联系方式的逻辑
                console.log('收集联系方式功能已启用');
                // TODO: 实现收集联系方式的具体逻辑
            }
            
            console.log('沟通处理完成:', communicationContent);
            return true;
        } catch (error) {
            console.error('处理沟通功能时出错:', error);
            return false;
        }
    }
}

  


export { HLiepinParser };