import { BaseParser } from './common.js';

class LiepinParser extends BaseParser {
    constructor() {
        super();
        this.selectors = {
            container: 'recommandResumes--',
            items: 'newResumeItemWrap--',
            name: 'nest-resume-personal-name',
            age: 'personal-detail-age',
            education: 'personal-detail-edulevel',
            university: 'resume-university',
            description: 'resume-description',
            clickTarget: 'ant-lpt-btn ant-lpt-btn-primary ant-lpt-teno-btn ant-lpt-teno-btn-secondary',
            extraSelectors: [
                { prefix: 'nest-resume-personal-skills', type: '技能' },
                { prefix: 'personal-expect-content', type: '薪资' },
                { prefix: 'personal-detail-location', type: 'location' },
            ]
        };

        this.urlInfo = {
            url: 'https://www.liepin.com/zhaopin/?key=python',
            site: '推荐人才'
        };

        // 添加猎聘特定的详情页选择器
        this.detailSelectors = {
            detailLink: 'newResumeItem', // 点击姓名打开详情
            closeButton: 'closeBtn--', // 关闭按钮
            viewButton: '.ant-lpt-btn.ant-lpt-btn-primary' // 查看按钮
        };
    }

    getElementByClassPrefix(parent, prefix) {
        console.log(parent);

        console.log(prefix);
        
        let element = parent.querySelector(`[class^="${prefix}"]`);
        if (!element) {
            element = parent.querySelector(`[class*="${prefix}"]`);
        }
        return element;
    }

    extractCandidates(elements = null) {
        console.log('开始提取候选人信息');
        console.log('传入的元素:', elements);
        
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
                this.highlightElement(item, 'processing');
                
                try {
                    const nameElement = this.getElementByClassPrefix(item, this.selectors.name);
                    const ageElement = this.getElementByClassPrefix(item, this.selectors.age);
                    const educationElement = this.getElementByClassPrefix(item, this.selectors.education);
                    const universityElement = this.getElementByClassPrefix(item, this.selectors.university);
                    const descriptionElement = this.getElementByClassPrefix(item, this.selectors.description);
                    
                    const extraInfo = this.extractExtraInfo(item, this.selectors.extraSelectors);
                    
                    const candidate = {
                        name: nameElement?.textContent?.trim() || '',
                        age: parseInt(ageElement?.textContent) || 0,
                        education: educationElement?.textContent?.trim() || '',
                        university: universityElement?.textContent?.trim() || '',
                        description: descriptionElement?.textContent?.trim() || item.textContent?.trim() || '',
                        extraInfo: extraInfo
                    };
                    
                    
                    if (candidate.name) {
                        candidates.push(candidate);
                        this.highlightElement(item, 'matched');
                    } else {
                        this.clearHighlight(item);
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
    clickMatchedItem(element) {
        try {
            const clickElement = this.getElementByClassPrefix(element, this.selectors.clickTarget);
            if (clickElement) {
                console.log('点击元素:', clickElement);
                clickElement.click();
                return true;
            } else {
                console.error('未找到可点击的元素');
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
            // 首先尝试点击姓名链接
            const detailLink = this.getElementByClassPrefix(element, this.detailSelectors.detailLink);
            
            if (detailLink) {
                console.log('查看候选人详情:', detailLink);
                detailLink.click();
                
                // 等待查看按钮出现并点击（某些情况下需要）
                // await new Promise(resolve => setTimeout(resolve, 500));
                // const viewButton = document.querySelector(this.detailSelectors.viewButton);
                // if (viewButton) {
                //     console.log('找到查看按钮，点击');
                //     viewButton.click();
                // }
                
                return true;
            }
            
            console.log('未找到详情链接');
            return false;
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
            console.log(closeButton);
            
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
}

export { LiepinParser }; 