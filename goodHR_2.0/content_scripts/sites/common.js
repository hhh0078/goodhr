// 基础解析器类
class BaseParser {
    constructor() {
        this.settings = null;
        this.filterSettings = null;
        // 添加高亮样式
        this.highlightStyles = {
            processing: `
                background-color: #fff3e0 !important;
                transition: background-color 0.3s ease;
                outline: 2px solid #ffa726 !important;
            `,
            matched: `
                background-color: #e8f5e9 !important;
                transition: background-color 0.3s ease;
                outline: 2px solid #4caf50 !important;
                box-shadow: 0 0 10px rgba(76, 175, 80, 0.3) !important;
            `
        };
        this.clickCandidateConfig = {
            enabled: true,
            frequency: 3,  // 默认每浏览10个点击3个
            viewDuration: [3, 5]  // 查看时间将从页面设置获取
        };
        // 添加浏览器工具栏高度配置，可以通过设置调整
        this.browserUIHeight = 110; // 默认值，包含工具栏和标签栏
    }

    async loadSettings() {
        return new Promise((resolve, reject) => {
            chrome.storage.local.get(['keywords', 'isAndMode'], (result) => {
                if (chrome.runtime.lastError) {
                    reject(chrome.runtime.lastError);
                    return;
                }
                this.settings = result;
                resolve(result);
            });
        });
    }

    setFilterSettings(settings) {
        this.filterSettings = settings;
    }

    // 基础的筛选方法
    filterCandidate(candidate) {
        if (!this.filterSettings) {
            //console.log('没有筛选设置，返回所有候选人');
            return true;  // 如果没有设置，默认匹配所有
        }

        // 合并所有需要匹配的文本
        const allText = [
            candidate.name,
            candidate.age?.toString(),
            candidate.education,
            candidate.university,
            candidate.description,
            ...(candidate.extraInfo?.map(info => `${info.type}:${info.value}`) || [])
        ].filter(Boolean).join(' ').toLowerCase();

        //console.log('检查文本:', allText);

        // 检查排除关键词
        if (this.filterSettings.excludeKeywords &&
            this.filterSettings.excludeKeywords.some(keyword =>
                allText.includes(keyword.toLowerCase())
            )) {
            //console.log('匹配到排除关键词');
            return false;
        }

        // 如果没有关键词，匹配所有
        if (!this.filterSettings.keywords || !this.filterSettings.keywords.length) {
            //console.log('没有设置关键词，匹配所有');
            return true;
        }

        if (this.filterSettings.isAndMode) {
            // 与模式：所有关键词都必须匹配
            return this.filterSettings.keywords.every(keyword => {
                if (!keyword) return true;
                return allText.includes(keyword.toLowerCase());
            });
        } else {

            // 或模式：匹配任一关键词即可
            return this.filterSettings.keywords.some(keyword => {
                if (!keyword) return false;
                return allText.includes(keyword.toLowerCase());
            });
        }
    }

    // 添加高亮方法
    highlightElement(element, type = 'processing') {
        if (element && this.highlightStyles[type]) {
            element.style.cssText = this.highlightStyles[type];
        }
    }

    // 清除高亮
    clearHighlight(element) {
        if (element) {
            element.style.cssText = '';
        }
    }

    // 添加提取额外信息的方法
    extractExtraInfo(element, extraSelectors) {
        const extraInfo = [];
        if (Array.isArray(extraSelectors)) {
            extraSelectors.forEach(selector => {
                const elements = this.getElementsByClassPrefix(element, selector.prefix);
                if (elements.length > 0) {
                    elements.forEach(el => {
                        const info = el.textContent?.trim();
                        if (info) {
                            extraInfo.push({
                                type: selector.type || 'unknown',
                                value: info
                            });
                        }
                    });
                }
            });
        }
        return extraInfo;
    }

    // 获取所有匹配前缀的元素
    getElementsByClassPrefix(parent, prefix) {
        const elements = [];
        // 使用前缀开头匹配
        const startsWith = Array.from(parent.querySelectorAll(`[class^="${prefix}"]`));
        // 使用包含匹配
        const contains = Array.from(parent.querySelectorAll(`[class*=" ${prefix}"]`));

        return [...new Set([...startsWith, ...contains])];
    }

    // 添加基础的点击方法
    clickMatchedItem(element) {
        // 默认实现，子类可以覆盖
        console.warn('未实现点击方法');
        return false;
    }

    // 添加新方法
    setClickCandidateConfig(config) {
        this.clickCandidateConfig = {
            ...this.clickCandidateConfig,
            ...config
        };
    }

    // 基础的随机点击判断方法
    shouldClickCandidate() {
        if (!this.clickCandidateConfig.enabled) return false;
        let random = Math.random() * 10;
        //console.log('随机数:', random);
        //console.log('频率:', this.clickCandidateConfig.frequency);
        //console.log('结果:', random <= (this.clickCandidateConfig.frequency));

        return random <= (this.clickCandidateConfig.frequency);
    }

    // 获取随机查看时间
    getRandomViewDuration() {
        // 使用 filterSettings 中的延迟设置
        const min = this.filterSettings?.scrollDelayMin || 3;
        const max = this.filterSettings?.scrollDelayMax || 5;
        return Math.floor(Math.random() * (max - min + 1) + min) * 1000;
    }

    // 基础的点击候选人方法（需要被子类重写）
    async clickCandidateDetail(element) {
        throw new Error('clickCandidateDetail method must be implemented by child class');
    }

    // 基础的关闭详情方法（需要被子类重写）
    async closeDetail() {
        throw new Error('closeDetail method must be implemented by child class');
    }

    // 添加设置工具栏高度的方法
    setBrowserUIHeight(height) {
        this.browserUIHeight = height;
        // 保存到存储中，以便在不同页面间共享
        chrome.storage.local.set({ 'browser_ui_height': height });
    }

    // 加载保存的高度设置
    async loadBrowserUIHeight() {
        try {
            const result = await chrome.storage.local.get('browser_ui_height');
            if (result.browser_ui_height) {
                this.browserUIHeight = result.browser_ui_height;
            }
        } catch (error) {
            console.error('加载浏览器UI高度设置失败:', error);
        }
    }

    // 添加通用的点击方法
    async clickElementByPosition(element) {
        try {
            if (!element) {
                console.error('元素不存在');
                return false;
            }

            // 先加载保存的高度设置
            await this.loadBrowserUIHeight();

            console.log(element);
            const rect = element.getBoundingClientRect();

            // 计算元素中心点位置
            let screenX = rect.left + rect.width / 2 + window.screenX;
            let screenY = rect.top + rect.height / 2 + window.screenY + this.browserUIHeight;

            console.log(`点击坐标(中心点): X=${screenX}, Y=${screenY}, 浏览器UI高度: ${this.browserUIHeight}`);

            // 如果在iframe中,累加所有父级iframe的位置
            let currentWindow = window;
            while (currentWindow !== window.top) {
                const frameElement = currentWindow.frameElement;
                const frameRect = frameElement.getBoundingClientRect();
                screenX += frameRect.left;
                screenY += frameRect.top;
                currentWindow = currentWindow.parent;
            }

            const x = Math.round(screenX);
            const y = Math.round(screenY);

            // 调用点击接口并获取返回数据
            const response = await fetch(`http://127.0.0.1:5000/api/mouse/move?x=${x}&y=${y}&click=true`);
            const data = await response.json();
            console.log('点击接口返回:', data);

            return true;
        } catch (error) {
            console.error('点击元素失败:', error);
            return false;
        }
    }
}

export { BaseParser }; 