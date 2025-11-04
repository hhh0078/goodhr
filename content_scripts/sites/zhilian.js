import { BaseParser } from './common.js';

class ZhilianParser extends BaseParser {
    constructor() {
        super();
        // 定义智联网特定的选择器
        this.selectors = {
            container: '[role="group"]',  // 容器列表
            items: 'recommend-item__inner recommend-resume-item__inner',   // 每个候选人卡片 (移除了点号)
            name: '.talent-basic-info__name--inner', // 姓名
            age: '.talent-basic-info__basic',   // 年龄
            education: '.resume-item__content.resume-card-exp', // 学历
            university: 'fasdfasdfasdf', // 学校
            description: '.resume-item__content', // 描述
            clickTarget: 'small-screen-btn is-mr-16',  // 打招呼按钮
            extraSelectors: [
                { selector: '.talent-basic-info__extra--content', type: '薪资' },
                { selector: 'fasdfasdf', type: '地点' },
                { selector: 'fasdfasdf', type: '经验' },
                { selector: 'sdfasdf', type: '标签' }
            ],
            xiangqing: ['new-resume-detail--inner', 'km-scrollbar__view', 'km-scrollbar__wrap']
        };

        this.urlInfo = {
            url: 'https://www.liepin.com/zhaopin/?key=python',
            site: '推荐人才'
        };

        // 智联特定的详情页选择器
        this.detailSelectors = {
            detailLink: 'resume-item__content resume-card-exp',  // 点击整个卡片打开详情
            closeButton: 'km-icon sati sati-times-circle-s', // 关闭按钮
            viewButton: '.km-button--primary'   // 查看简历按钮
        };


    }

    // 重写查找元素的方法
    findElements() {
        return document.querySelectorAll(this.selectors.items);
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

    /**
     * 构建第一次候选人信息
     * @param {*} element
     * @returns
     */
    getSimpleCandidateInfo(data) {
        console.log("智联的信息");
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


    /**
     * 查找同事沟通记录
     * @param {*} data
     * @returns
     */
    queryColleagueContactedInfo(data) {


        return ""
    }

   async extractCandidates2(data) {
        // 使用通用查找函数查找元素，尝试多种方法（同步模式）
        const result = this.findElementsByMultipleMethods(".new-shortcut-resume--wrapper", {
            includeHidden: false,  // 不包含隐藏元素
            methods: ['all']  // 尝试所有方法
        });        
        // 直接同步处理结果
        // console.log("元素查找结果:", result);
        
        if (result.elements && result.elements.length > 0) {
            // console.log("成功找到元素，使用方法:", result.method);
           return result.elements[0].textContent;
        } else {
            // console.log("未能找到目标元素:", result.message);
            return null;
        }
    }


    // 重写提取候选人信息的方法
    extractCandidates(elements = null) {

        const candidates = [];

        try {
            const items = elements || this.findElements();


            Array.from(items).forEach((item, index) => {
                try {
                    const nameElement = item.querySelector(this.selectors.name);
                    const ageElement = item.querySelector(this.selectors.age);
                    const educationElement = item.querySelector(this.selectors.education);
                    const universityElement = item.querySelector(this.selectors.university);
                    const descriptionElement = item.querySelector(this.selectors.description);

                    // 提取额外信息
                    const extraInfo = this.selectors.extraSelectors.map(selector => {
                        const element = item.querySelector(selector.selector);
                        return element ? {
                            type: selector.type,
                            value: element.textContent?.trim() || ''
                        } : null;
                    }).filter(info => info !== null);

                    const candidate = {
                        name: nameElement?.textContent?.trim() || '',
                        age: this.extractAge(ageElement?.textContent),
                        education: educationElement?.textContent?.trim() || '',
                        university: universityElement?.textContent?.trim() || '',
                        description: descriptionElement?.textContent?.trim() || '',
                        extraInfo: extraInfo
                    };

                    if (candidate.name) {
                        candidates.push(candidate);
                    }
                } catch (error) {
                    console.error(`处理第 ${index + 1} 个候选人时出错:`, error);
                }
            });
        } catch (error) {
            console.error('提取候选人信息失败:', error);
            throw error;
        }

        return candidates;
    }


    // 重写点击打招呼的方法
    async clickMatchedItem(element) {
        try {

            // 包含部分类名

            const greetButton = element.querySelector(`[class^="${this.selectors.clickTarget}"]`);
          

            if (greetButton) {
                greetButton.click();
                return true;
            }
            return false;
        } catch (error) {
            console.error('点击打招呼按钮失败:', error);
            return false;
        }
    }

    // 重写点击查看详情的方法
    async clickCandidateDetail(element) {
        // element.click();
        try {
            const detailLink = element.getElementsByClassName(this.detailSelectors.detailLink)[0];
            if (detailLink) {
                detailLink.click();
                return true;
            }
            return false;
        } catch (error) {
            console.error('点击候选人详情失败:', error);
            return false;
        }

    }

    // 重写关闭详情的方法
    async closeDetail() {
        try {
            const closeButton = document.getElementsByClassName(this.detailSelectors.closeButton)[0];
            if (closeButton) {
                closeButton.click();
                return true;
            }
            return false;
        } catch (error) {
            console.error('关闭详情失败:', error);
            return false;
        }
    }

    // 重写年龄提取方法
    extractAge(text) {
        if (!text) return 0;
        const matches = text.match(/(\d+)/);  // 智联可能只显示数字
        return matches ? parseInt(matches[1]) : 0;
    }
}

export { ZhilianParser };