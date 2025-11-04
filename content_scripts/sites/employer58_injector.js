// 猎聘网拦截器注入器 - 在document_start时运行 
// console.log('✅ 猎聘网拦截器注入器已加载'); 

// 页面加载完成后，准备候选人信息
window.addEventListener('load', async function() {
    // console.log('页面加载完成，开始准备候选人信息');
    
    // 检查是否是候选人详情页
    if (isCandidateDetailPage()) {
        // console.log('确认为候选人详情页，开始准备信息');
        
        // 检查时间戳，判断是否是插件自动点击
        const clickTimestamp = localStorage.getItem('goodhr-candidate-click-timestamp');
        const currentTime = Date.now();
        
        if (clickTimestamp) {
            const timeDiff = currentTime - parseInt(clickTimestamp);
            // console.log(`点击时间戳: ${clickTimestamp}, 当前时间: ${currentTime}, 时间差: ${timeDiff}ms`);
            
            // 如果时间差小于10秒，则是插件自动点击
            if (timeDiff < 10000) {
                // console.log('确认为插件自动点击，继续处理候选人信息');
                await processCandidateDetail();
            } else {
                // console.log('时间差超过10秒，可能是用户手动点击，不处理');
                localStorage.removeItem('goodhr-candidate-click-timestamp'); // 清除旧的时间戳

                return false;
                // window.close();
            }
        } else {
            console.log('未找到点击时间戳，可能是用户手动点击，不处理');
            // window.close();
        }
    } else {
        console.log('不是候选人详情页，不处理');
        // window.close();
    }
});

// 处理候选人详情的函数
async function processCandidateDetail() {
    try {
        // 准备候选人详细信息
        const candidateDetail = await extractCandidateDetail();
        // console.log('候选人信息准备完成:', candidateDetail);
        
        // 生成一个唯一的请求ID
        const requestId = 'req-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
        // console.log('准备存储候选人信息到缓存，requestId:', requestId);
        
        // 将候选人信息存储到localStorage
        const storageData = {
            source: 'goodhr-plugin',
            type: 'candidate-detail-response',
            requestId: requestId,
            data: candidateDetail,
            timestamp: Date.now()
        };
        localStorage.setItem('goodhr-candidate-detail', JSON.stringify(storageData));
        // console.log('候选人信息已存储到localStorage, requestId:', requestId);
        
        // 清除点击时间戳
        localStorage.removeItem('goodhr-candidate-click-timestamp');
        
        // 延迟关闭页面，确保数据存储完成
        setTimeout(() => {
            // console.log('准备关闭页面');
            window.close();
        }, 2000);
        
    } catch (error) {
        console.error('处理候选人信息时出错:', error);
        localStorage.removeItem('goodhr-candidate-click-timestamp'); // 出错时也要清除时间戳
        window.close();
    }
}

// 检查当前页面是否是候选人详情页
function isCandidateDetailPage() {
    // 检查URL是否包含候选人详情页的特征
    return window.location.href.includes('/showresumedetail/');
}

// 提取候选人详细信息的函数
async function extractCandidateDetail() {
    try {
        const candidate = {};
        let nameElement = null;
        let textElement = null;
        let attempts = 0;
        const maxAttempts = 20;
        
        // 循环获取候选人姓名和文本内容，最多尝试20次
        while (attempts < maxAttempts) {
            // 获取候选人姓名
            nameElement = document.querySelector('[class*="name-box"]');
            // 获取文本内容
            textElement = document.querySelector('[class*="ant-tabs-tabpane ant-tabs-tabpane-active"]');
            
            // 检查两个值是否都非空
            if (nameElement && nameElement.textContent.trim() && 
                textElement && textElement.textContent.trim()) {
                // 两个值都有效，退出循环
                break;
            }
            
            // 如果任一值为空，等待0.1秒后重试
            // console.log(`尝试获取候选人信息 (${attempts + 1}/${maxAttempts})，等待0.1秒后重试...`);
            await new Promise(resolve => setTimeout(resolve, 100));
            attempts++;
        }
        
        // 获取候选人姓名
        candidate.name = nameElement ? nameElement.textContent.trim() : '';
        // 获取文本内容
        candidate.textContent = textElement ? textElement.textContent.trim() : '';
        
        // 如果尝试了最大次数仍未获取到有效信息，记录警告
        if (attempts >= maxAttempts && (!candidate.name || !candidate.textContent)) {
            console.warn(`尝试${maxAttempts}次后仍未获取到完整的候选人信息`);
        }
        
        return candidate;
    } catch (error) {
        console.error('提取候选人详细信息时出错:', error);
        return { error: '提取候选人详细信息时出错', message: error.message };
    }
}

// console.log('猎聘网拦截器注入器初始化完成');
