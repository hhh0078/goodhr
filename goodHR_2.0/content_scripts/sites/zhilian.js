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
            ]
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

    // 重写提取候选人信息的方法
    extractCandidates(elements = null) {
        console.log('开始提取智联信息');
        const candidates = [];
        
        try {
            const items = elements || this.findElements();
            console.log('找到候选人元素:', items.length);
            
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
    clickMatchedItem(element) {
        try {
            
            // 包含部分类名

            const greetButton = element.querySelector(`[class^="${this.selectors.clickTarget}"]`);
            console.log("开始打招呼");
            console.log(element);

            console.log(greetButton);
            
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

        console.log(element);

        element.click();
        
        try {
            const detailLink = element.getElementsByClassName(this.detailSelectors.detailLink)[0];
            console.log(element.getElementsByClassName(this.detailSelectors.detailLink));
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
            console.log(document.getElementsByClassName(this.detailSelectors.detailLink));
            if (closeButton) {
                closeButton.click();
                console.log('关闭候选人详情');
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