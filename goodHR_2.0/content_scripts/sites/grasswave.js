import { BaseParser } from './common.js';

class GrassWaveParser extends BaseParser {
    constructor() {
        super();
        // 定义要提取文本的CSS选择器
        this.textSelectors = [
            '.job-title',     // 职位标题
            '.job-desc',      // 职位描述
            '.company-info',  // 公司信息
            '.requirements'   // 要求信息
        ];
    }

    extractCandidates(elements = null) {
        const candidates = [];
        const items = elements || document.querySelectorAll('tr');
        
        items.forEach(item => {
            if (item.parentElement.tagName === 'THEAD') return;
            
            // 收集所有指定选择器的文本
            let fullText = '';
            this.textSelectors.forEach(selector => {
                const elements = item.querySelectorAll(selector);
                elements.forEach(el => {
                    fullText += el.textContent.trim() + ' ';
                });
            });

            // 如果没有找到指定的class，则使用整个tr的文本
            if (!fullText.trim()) {
                fullText = item.textContent.trim();
            }

            const cells = item.cells;
            if (cells.length >= 4) {
                const candidate = {
                    name: cells[0]?.textContent?.trim() || '',
                    age: parseInt(cells[1]?.textContent) || 0,
                    education: cells[2]?.textContent?.trim().replace(/学历$/, ''),
                    university: cells[3]?.textContent?.trim() || '',
                    description: fullText // 使用收集到的完整文本作为描述
                };
                
                // 确保数据有效
                if (candidate.name && candidate.age && candidate.education) {
                    candidates.push(candidate);
                }
            }
        });

        return candidates;
    }

    highlightCandidate(candidate) {
        // 高亮匹配的候选人
        const rows = document.querySelectorAll('tr');
        rows.forEach(row => {
            if (row.cells[0]?.textContent?.trim() === candidate.name) {
                row.style.backgroundColor = '#e8f0fe';
            }
        });
    }
}

export { GrassWaveParser }; 