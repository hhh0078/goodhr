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
        const allText = candidate

        if (allText == null) {
            // alert("插件获取候选人文本失败");
            return false;
        }
      

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
        // return false;
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
}

export { BaseParser };