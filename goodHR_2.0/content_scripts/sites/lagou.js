import { BaseParser } from './common.js';

class LagouParser extends BaseParser {
    constructor() {
        super();
        this.selectors = {
            container: 'position-list',
            items: 'position-item',          // 候选人卡片
            name: 'position-name',           // 职位名称（拉勾网显示职位而不是姓名）
            age: 'age-info',                 // 年龄信息
            education: 'edu-background',      // 教育背景
            university: 'school-name',        // 学校名称
            description: 'position-detail',   // 职位描述
            clickTarget: 'position-title',    // 点击目标
            extraSelectors: [
                { prefix: 'salary', type: '薪资' },
                { prefix: 'company-name', type: '公司' },
                { prefix: 'industry-field', type: '行业' },
                { prefix: 'position-address', type: '地点' }
            ]
        };
        this.urlInfo = {
            url: 'https://www.liepin.com/zhaopin/?key=python',
            site: '推荐人才'
        }
    }

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

    extractCandidates(elements = null) {
        // 与 BossParser 相同的实现...
        // 可以直接复制 BossParser 中的 extractCandidates 方法
    }
}

export { LagouParser }; 