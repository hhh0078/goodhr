import { BaseParser } from './common.js';

class BossParser extends BaseParser {
    constructor() {
        super();
        // 定义完整的 class 名称
        this.fullClasses = {
            container: 'card-list',
            items: 'candidate-card-wrap',
            name: 'name',
            age: 'job-card-left_labels__wVUfs',
            education: 'base-info join-text-wrap',
            university: 'content join-text-wrap',
            description: 'content',
            clickTarget: ['btn btn-greet', 'btn btn-getcontact search-btn-tip btn-prop-common prop-card-chat']
        };
        this.urlInfo = {
            url: '/web/chat/recommend',
            site: '推荐牛人'
        };

        // 定义部分 class 名称（用于模糊匹配）
        this.selectors = {
            container: 'card-list',
            items: ['candidate-card-wrap', 'card-inner clear-fix'],
            name: 'name',
            age: ['job-card-left'],
            education: ['base-info join-text-wrap', 'geek-info-detail'],
            university: 'content join-text-wrap',
            description: 'content',
            clickTarget: ['btn btn-greet', 'btn btn-getcontact search-btn-tip btn-prop-common prop-card-chat'],
            activeText: 'active-text',
            extraSelectors: [
                { prefix: 'salary-text', type: '薪资' },
                { prefix: 'job-info-primary', type: '基本信息' },
                { prefix: 'tags-wrap', type: '标签' },
                { prefix: 'content join-text-wrap', type: '公司信息' }
            ],
            // 同事是否沟通过
            isContacted: ['colleague-collaboration']

        };

        // BOSS特定的选择器
        this.detailSelectors = {
            detailLink: ['card-inner common-wrap', 'card-inner clear-fix'],
            closeButton: ['boss-popup__close', 'boss-layer__wrapper'],
            closeButtonXpath: ['//*[@id="boss-dynamic-dialog-1j38fleo5"]/div/div[2]']
        };
    }

    // 添加一个新的查找元素的方法
    findElements() {
        let items = [];

        // 1. 首先尝试使用完整的 class 名称
        items = document.getElementsByClassName(this.fullClasses.items);

        if (items.length === 0) {
            // 2. 尝试使用简单的 class 名称
            items = document.getElementsByClassName(this.selectors.items);
        }

        if (items.length === 0) {
            // 3. 尝试使用模糊匹配
            items = document.querySelectorAll(`[class*="${this.selectors.items}"]`);
        }

        if (items.length === 0) {
            // 4. 尝试使用前缀匹配
            items = document.querySelectorAll(`[class^="${this.selectors.items}"], [class*=" ${this.selectors.items}"]`);
        }

        return items;
    }

    //提取信息
    extractCandidates(elements = null) {

        const candidates = [];
        let items;

        try {
            if (elements) {
                items = elements;
            } else {
                items = this.findElements();

                if (items.length === 0) {
                    // 输出更多调试信息


                    // 修改 class 列表的获取方式
                    const allClasses = Array.from(document.querySelectorAll('*'))
                        .map(el => {
                            if (el instanceof SVGElement) {
                                return el.className.baseVal;
                            }
                            return el.className;
                        })
                        .filter(className => {
                            return className && typeof className === 'string' && className.trim() !== '';
                        });

                    //console.log('页面上所有的 class:', allClasses.join('\n'));
                    throw new Error('未找到任何元素，请检查选择器是否正确');
                }
            }


            Array.from(items).forEach((item, index) => {
                this.highlightElement(item, 'processing');

                try {
                    // 使用多种方式查找子元素
                    const findElement = (fullClass, partialClass) => {
                        //partialClass 可能是数组
                        if (Array.isArray(partialClass)) {
                            for (const className of partialClass) {
                                const element = item.getElementsByClassName(className)[0] ||
                                    item.querySelector(`[class*="${className}"]`);
                                if (element) {
                                    return element;
                                }
                            }
                        } else {
                            return item.getElementsByClassName(fullClass)[0] ||
                                item.getElementsByClassName(partialClass)[0] ||
                                item.querySelector(`[class*="${partialClass}"]`);
                        }
                    };

                    const nameElement = findElement(this.fullClasses.name, this.selectors.name);
                    const ageElement = findElement(this.fullClasses.age, this.selectors.age);

                    console.log(ageElement);

                    const educationElement = findElement(this.fullClasses.education, this.selectors.education);
                    const universityElement = findElement(this.fullClasses.university, this.selectors.university);
                    const descriptionElement = findElement(this.fullClasses.description, this.selectors.description);
                    let activeTextElement = findElement(this.fullClasses.activeText, this.selectors.activeText);



                    if (!activeTextElement) {

                        let onlineMarker = findElement('online-marker', 'online-marker');
                        if (onlineMarker) {
                            activeTextElement = "在线";
                        } else {
                            activeTextElement = "离线";
                        }

                    } else {
                        activeTextElement = activeTextElement.textContent?.trim() || "";
                    }

                    const extraInfo = this.extractExtraInfo(item, this.selectors.extraSelectors);

                    const candidate = {
                        name: nameElement?.textContent?.trim() || '',
                        age: this.extractAge(ageElement?.textContent),
                        education: educationElement?.textContent?.trim() || '',
                        university: universityElement?.textContent?.trim() || '',
                        description: descriptionElement?.textContent?.trim() || '',
                        activeText: activeTextElement,
                        extraInfo: extraInfo
                    };


                    if (candidate.name) {
                        candidates.push(candidate);
                        this.highlightElement(item, 'matched');
                    } else {
                        this.clearHighlight(item);
                    }
                } catch (error) {
                    console.error(`处理第 ${index + 1} 个元素时出错:`, error);
                    this.clearHighlight(item);
                }
            });

        } catch (error) {
            console.error('提取信息失败:', error);
            throw error;
        }

        console.log('提取到的候选人信息:', candidates);

        return candidates;
    }

    extractAge(text) {
        if (!text) return 0;
        const matches = text.match(/(\d+)岁/);
        return matches ? parseInt(matches[1]) : 0;
    }

    async clickMatchedItem(element) {



        console.log('打招呼:', element);
        try {
            let clickElement = null;
            // 使用多种方式查找点击目标

            //触发element 的鼠标移入事件
            element.dispatchEvent(new MouseEvent('mouseover', {
                view: window,
                bubbles: true,
                cancelable: true
            }));

            //this.selectors.clickTarget 是数组

            for (const className of this.selectors.clickTarget) {

                let aaa = element.getElementsByClassName(className) ||
                    element.getElementsByClassName(className) ||
                    element.querySelector(`[class*="${className}"]`);
                if (aaa.length > 0) {

                    for (let i = 0; i <= aaa.length; i++) {
                        if (aaa[i]) {
                            aaa[i].click();
                        }
                    }
                }
            }
            // 使用多种方式查找点击目标\
            //console.log('点击元素:', clickElement);
            if (clickElement) {
                clickElement.click();
                return true;
            }
            return false;
        } catch (error) {
            console.error('点击元素时出错:', error);
            return false;
        }
    }

    // 实现点击候选人详情方法
    async clickCandidateDetail(element) {
        console.log(element);

        try {
            let detailLink = null;
            //this.detailSelectors.detailLink 是数组
            for (const className of this.detailSelectors.detailLink) {
                let element2 = element.getElementsByClassName(className)[0]
                if (element2) {
                    console.log(element2);
                    detailLink = element2;

                }
            }

            //console.log(detailLink);
            if (detailLink) {
                detailLink.click();
                console.log('点击候选人详情成功');

                return true;
            } else {
                console.error('无法查找到详情class:', this.detailSelectors.detailLink);
            }
            return false;
        } catch (error) {
            console.error('点击候选人详情失败:', error);
            return false;
        }
    }


    // Boss平台特定的简历OCR识别（继承基类方法）
    async findAndOCRCanvas(element) {
        // 调用基类的通用OCR方法，指定canvas ID为'resume'
        return await super.findAndOCRCanvas(element, 'resume');
    }

    // 注入Tesseract.js库（保留原有复杂的注入逻辑作为备用）
    async injectTesseract() {
        return new Promise((resolve, reject) => {
            console.log('开始注入Tesseract.js库...');

            // 检查主页面是否已有Tesseract
            if (window.Tesseract) {
                console.log('主页面Tesseract已存在');
            }

            // 递归查找所有iframe（包括嵌套的）
            const findAllIframes = (doc = document, depth = 0) => {
                const iframes = [];
                const currentIframes = doc.querySelectorAll('iframe');
                console.log(`在第 ${depth} 层找到 ${currentIframes.length} 个iframe`);

                for (let iframe of currentIframes) {
                    iframes.push({ iframe, depth, path: `第${depth}层-${iframes.length}` });
                    try {
                        const iframeDoc = iframe.contentDocument || iframe.contentWindow?.document;
                        if (iframeDoc) {
                            // 递归查找嵌套的iframe
                            const nestedIframes = findAllIframes(iframeDoc, depth + 1);
                            iframes.push(...nestedIframes);
                        }
                    } catch (error) {
                        console.log(`第 ${depth} 层iframe访问失败:`, error.message);
                    }
                }

                return iframes;
            };

            const allIframes = findAllIframes();
            console.log(`总共找到 ${allIframes.length} 个iframe（包括嵌套的）`);

            let injectedCount = 0;
            let totalCount = 1 + allIframes.length; // 主页面 + iframe数量

            // 注入到主页面的函数
            const injectToMainPage = () => {
                try {
                    // 检查网页页面是否已有Tesseract
                    const pageWindow = window.wrappedJSObject || window;
                    if (pageWindow.Tesseract) {
                        console.log('网页主页面Tesseract已存在，跳过注入');
                        injectedCount++;
                        checkAllInjected();
                        return;
                    }

                    console.log('注入Tesseract到网页主页面...');
                    // 注入到网页页面，不是content script页面
                    const script = document.createElement('script');
                    script.src = chrome.runtime.getURL('sounds/tesseract.min.js');

                    script.onload = () => {
                        console.log('网页主页面Tesseract脚本加载成功');
                        injectedCount++;
                        checkAllInjected();
                    };

                    script.onerror = () => {
                        console.error('网页主页面Tesseract脚本加载失败');
                        injectedCount++;
                        checkAllInjected();
                    };

                    // 注入到网页页面的document中
                    (document.head || document.documentElement).appendChild(script);
                } catch (error) {
                    console.log('注入网页主页面Tesseract时出错:', error.message);
                    injectedCount++;
                    checkAllInjected();
                }
            };

            // 注入到iframe的函数
            const injectToIframe = (iframe, index, path = '') => {
                try {
                    const iframeDoc = iframe.contentDocument || iframe.contentWindow?.document;
                    if (!iframeDoc) {
                        console.log(`${path} iframe ${index} 无法访问，跳过注入`);
                        injectedCount++;
                        checkAllInjected();
                        return;
                    }

                    if (iframeDoc.defaultView.Tesseract) {
                        console.log(`${path} iframe ${index} Tesseract已存在，跳过注入`);
                        injectedCount++;
                        checkAllInjected();
                        return;
                    }

                    console.log(`注入Tesseract到${path} iframe ${index}...`);
                    const script = iframeDoc.createElement('script');
                    script.src = chrome.runtime.getURL('sounds/tesseract.min.js');

                    console.log(`${path} iframe ${index} Tesseract脚本URL:`, script.src);
                    console.log(`${path} iframe ${index} 开始注入Tesseract脚本到head...`);

                    script.onload = () => {
                        console.log(`${path} iframe ${index} Tesseract脚本加载成功`);
                        injectedCount++;
                        checkAllInjected();
                    };

                    script.onerror = () => {
                        console.error(`${path} iframe ${index} Tesseract脚本加载失败`);
                        injectedCount++;
                        checkAllInjected();
                    };

                    // 添加超时检查
                    setTimeout(() => {
                        if (!script.onload && !script.onerror) {
                            console.log(`${path} iframe ${index} Tesseract脚本加载超时，强制继续`);
                            injectedCount++;
                            checkAllInjected();
                        }
                    }, 3000); // 5秒超时

                    iframeDoc.head.appendChild(script);
                    console.log(`${path} iframe ${index} Tesseract脚本已添加到head`);
                } catch (error) {
                    console.log(`${path} iframe ${index} 注入失败:`, error.message);
                    injectedCount++;
                    checkAllInjected();
                }
            };

            // 检查所有注入是否完成
            const checkAllInjected = () => {
                if (injectedCount >= totalCount) {
                    console.log('所有Tesseract注入完成');

                    // 使用轮询方式检查库是否可用
                    let checkAttempts = 0;
                    const maxAttempts = 20; // 最多尝试20次，每次500ms

                    const checkAvailability = () => {
                        checkAttempts++;
                        console.log(`第 ${checkAttempts} 次检查Tesseract可用性...`);

                        // 检查主页面（网页页面，不是content script页面）
                        let mainPageAvailable = false;
                        try {
                            // 获取网页页面的window对象
                            const pageWindow = window.wrappedJSObject || window;
                            if (pageWindow.Tesseract) {
                                console.log('网页主页面Tesseract可用');
                                mainPageAvailable = true;
                            } else {
                                console.log('网页主页面Tesseract不可用');
                            }
                        } catch (error) {
                            console.log('检查网页主页面Tesseract时出错:', error.message);
                        }

                        // 检查iframe
                        let iframeAvailable = false;
                        for (let i = 0; i < allIframes.length; i++) {
                            try {
                                const iframeInfo = allIframes[i];
                                const iframeDoc = iframeInfo.iframe.contentDocument || iframeInfo.iframe.contentWindow?.document;
                                if (iframeDoc) {
                                    const iframeWindow = iframeDoc.defaultView || iframeInfo.iframe.contentWindow;
                                    if (iframeWindow && iframeWindow.Tesseract) {
                                        console.log(`${iframeInfo.path} iframe ${i} Tesseract可用`);
                                        iframeAvailable = true;

                                        // 测试iframe中Tesseract是否真的可用
                                        try {
                                            console.log(`测试${iframeInfo.path} iframe ${i} Tesseract功能...`);
                                            if (typeof iframeWindow.Tesseract.recognize === 'function') {
                                                console.log(`${iframeInfo.path} iframe ${i} Tesseract.recognize函数可用`);
                                            } else {
                                                console.log(`${iframeInfo.path} iframe ${i} Tesseract.recognize函数不可用`);
                                            }
                                        } catch (error) {
                                            console.log(`测试${iframeInfo.path} iframe ${i} Tesseract时出错:`, error.message);
                                        }
                                    } else {
                                        console.log(`${iframeInfo.path} iframe ${i} Tesseract不可用`);
                                        // 调试信息
                                        console.log(`${iframeInfo.path} iframe ${i} 窗口对象:`, iframeWindow);
                                        console.log(`${iframeInfo.path} iframe ${i} Tesseract:`, iframeWindow?.Tesseract);
                                    }
                                } else {
                                    console.log(`${iframeInfo.path} iframe ${i} 无法访问文档`);
                                }
                            } catch (error) {
                                console.log(`iframe ${i} 检查时出错:`, error.message);
                            }
                        }

                        if (mainPageAvailable || iframeAvailable) {
                            console.log('Tesseract库已可用，注入完成');

                            // 测试Tesseract是否真的可用
                            try {
                                if (mainPageAvailable) {
                                    console.log('测试网页主页面Tesseract功能...');
                                    // 获取网页页面的window对象
                                    const pageWindow = window.wrappedJSObject || window;

                                    if (typeof pageWindow.Tesseract.recognize === 'function') {
                                        console.log('网页主页面Tesseract.recognize函数可用');
                                    } else {
                                        console.log('网页主页面Tesseract.recognize函数不可用');
                                    }
                                }
                            } catch (error) {
                                console.log('测试网页主页面Tesseract时出错:', error.message);
                            }

                            resolve();
                            return;
                        }

                        if (checkAttempts >= maxAttempts) {
                            console.error('Tesseract库检查超时，但继续执行');
                            // 即使检查失败也继续，因为库可能已经加载但检查有问题
                            resolve();
                            return;
                        }

                        // 继续检查
                        setTimeout(checkAvailability, 50);
                    };

                    // 开始检查
                    setTimeout(checkAvailability, 50);
                }
            };

            // 开始注入
            injectToMainPage();

            // 注入到所有iframe
            allIframes.forEach((iframeInfo, index) => {
                injectToIframe(iframeInfo.iframe, index, iframeInfo.path);
            });

            // 设置超时
            setTimeout(() => {
                reject(new Error('Tesseract注入超时'));
            }, 10000); // 10秒超时
        });
    }

    // 使用Tesseract.js进行OCR文字识别
    async performOCRWithTesseract(canvas) {
        try {
            console.log('开始使用Tesseract.js进行OCR识别...');

            // 检查canvas对象是否有效
            if (!canvas || typeof canvas.toDataURL !== 'function') {
                throw new Error('无效的canvas对象');
            }

            // 将Canvas转换为图像数据
            const imageData = canvas.toDataURL('image/png');
            console.log('Canvas转换为图像数据成功，数据长度:', imageData.length);

            // 注入并执行OCR识别
            return await this.executeOCRWithInjection(imageData);

        } catch (error) {
            console.error('Tesseract OCR识别失败:', error);
            return {
                success: false,
                error: error.message,
                stack: error.stack
            };
        }
    }

    // 简化的OCR执行方法
    async executeOCRWithInjection(imageData) {
        try {
            console.log('开始简化的OCR识别流程...');

            // 首先尝试注入Tesseract库到主页面
            await this.injectTesseractToMainPage();

            // 执行OCR识别
            return await this.performOCRRecognition(imageData);

        } catch (error) {
            console.error('简化OCR流程失败:', error);
            return {
                success: false,
                error: error.message
            };
        }
    }

    // 注入Tesseract到主页面
    async injectTesseractToMainPage() {
        return new Promise((resolve, reject) => {
            // 检查是否已存在
            if (window.Tesseract) {
                console.log('Tesseract已存在于主页面');
                resolve();
                return;
            }

            console.log('注入Tesseract到主页面...');
            const script = document.createElement('script');
            script.src = chrome.runtime.getURL('sounds/tesseract.min.js');
            script.type = 'text/javascript';

            script.onload = () => {
                console.log('Tesseract脚本加载成功');
                setTimeout(() => {
                    if (window.Tesseract) {
                        console.log('Tesseract在主页面可用');
                        resolve();
                    } else {
                        reject(new Error('Tesseract加载后仍不可用'));
                    }
                }, 2000);
            };

            script.onerror = () => {
                console.error('Tesseract脚本加载失败');
                reject(new Error('Tesseract脚本加载失败'));
            };

            (document.head || document.documentElement).appendChild(script);

            // 设置超时
            setTimeout(() => {
                reject(new Error('Tesseract注入超时'));
            }, 10000);
        });
    }

    // 执行OCR识别
    async performOCRRecognition(imageData) {
        return new Promise((resolve, reject) => {
            try {
                // 创建唯一的消息ID用于通信
                const messageId = 'ocr_' + Date.now() + '_' + Math.random();

                // 监听OCR结果
                const messageHandler = (event) => {
                    if (event.data && event.data.type === 'OCR_RESULT' && event.data.id === messageId) {
                        window.removeEventListener('message', messageHandler);
                        if (event.data.success) {
                            resolve(event.data.result);
                        } else {
                            reject(new Error(event.data.error));
                        }
                    }
                };

                window.addEventListener('message', messageHandler);

                // 注入OCR识别脚本到页面上下文
                const script = document.createElement('script');
                script.textContent = `
                    (async function() {
                        try {
                            console.log('开始页面内OCR识别...');

                            // 检查Tesseract是否可用
                            if (typeof Tesseract === 'undefined') {
                                throw new Error('Tesseract.js未加载');
                            }

                            console.log('Tesseract可用，开始识别...');

                            // 执行OCR识别（使用中英文混合识别）
                            const result = await Tesseract.recognize(
                                '${imageData}',
                                'chi_sim+eng',
                                {
                                    logger: function(m) {
                                        if (m.status === 'recognizing text') {
                                            console.log('OCR识别进度: ' + Math.round(m.progress * 100) + '%');
                                        } else {
                                            console.log('OCR状态: ' + m.status);
                                        }
                                    }
                                }
                            );

                            console.log('OCR识别完成，置信度:', result.data.confidence);

                            // 发送结果
                            window.postMessage({
                                type: 'OCR_RESULT',
                                id: '${messageId}',
                                success: true,
                                result: {
                                    success: true,
                                    text: result.data.text,
                                    confidence: result.data.confidence,
                                    words: result.data.words ? result.data.words.map(word => ({
                                        text: word.text,
                                        confidence: word.confidence,
                                        bbox: word.bbox
                                    })) : []
                                }
                            }, '*');

                        } catch (error) {
                            console.error('页面内OCR识别失败:', error);
                            window.postMessage({
                                type: 'OCR_RESULT',
                                id: '${messageId}',
                                success: false,
                                error: error.message
                            }, '*');
                        }
                    })();
                `;

                // 注入脚本
                (document.head || document.documentElement).appendChild(script);

                // 设置超时
                setTimeout(() => {
                    window.removeEventListener('message', messageHandler);
                    reject(new Error('OCR识别超时'));
                }, 30000);

            } catch (error) {
                reject(error);
            }
        });
    }

    // 在页面上下文中执行OCR识别
    async executeOCRInPageContext(imageData) {
        return new Promise((resolve, reject) => {
            try {
                // 创建唯一的消息ID
                const messageId = 'ocr_' + Date.now() + '_' + Math.random();

                // 监听OCR结果
                const messageHandler = (event) => {
                    if (event.data && event.data.type === 'OCR_RESULT' && event.data.id === messageId) {
                        window.removeEventListener('message', messageHandler);
                        if (event.data.success) {
                            resolve(event.data.result);
                        } else {
                            reject(new Error(event.data.error));
                        }
                    }
                };

                window.addEventListener('message', messageHandler);

                // 递归查找包含简历元素的iframe
                const findResumeIframe = (doc = document, depth = 0) => {
                    const iframes = doc.querySelectorAll('iframe');
                    console.log(`在第 ${depth} 层查找resume元素，找到 ${iframes.length} 个iframe`);

                    for (let iframe of iframes) {
                        try {
                            const iframeDoc = iframe.contentDocument || iframe.contentWindow?.document;
                            if (iframeDoc) {
                                // 在当前iframe中查找
                                if (iframeDoc.getElementById('resume')) {
                                    console.log(`在第 ${depth} 层iframe中找到resume元素`);
                                    return iframe;
                                }

                                // 递归查找下一层iframe
                                const nestedIframe = findResumeIframe(iframeDoc, depth + 1);
                                if (nestedIframe) {
                                    return nestedIframe;
                                }
                            }
                        } catch (error) {
                            console.log(`第 ${depth} 层iframe访问失败:`, error.message);
                        }
                    }

                    return null;
                };

                const targetIframe = findResumeIframe();
                const targetDocument = targetIframe ?
                    (targetIframe.contentDocument || targetIframe.contentWindow?.document) :
                    document;

                if (!targetDocument) {
                    reject(new Error('无法访问目标文档'));
                    return;
                }

                // 在目标文档上下文中执行OCR代码
                const script = targetDocument.createElement('script');
                script.textContent = `
                    (async function() {
                        try {
                            console.log('在目标文档上下文中开始OCR识别...');

                            // 检查Tesseract是否可用
                            if (typeof Tesseract === 'undefined') {
                                throw new Error('Tesseract.js未加载');
                            }

                            console.log('Tesseract.js可用，开始识别...');

                            // 执行OCR识别
                            const result = await Tesseract.recognize(
                                '${imageData}',
                                'chi_sim+eng',
                                {
                                    logger: m => {
                                        if (m.status === 'recognizing text') {
                                            console.log('OCR识别进度:', Math.round(m.progress * 100) + '%');
                                        } else {
                                            console.log('OCR状态:', m.status);
                                        }
                                    }
                                }
                            );

                            console.log('OCR识别完成');

                            // 发送结果回content script
                            window.postMessage({
                                type: 'OCR_RESULT',
                                id: '${messageId}',
                                success: true,
                                result: {
                                    success: true,
                                    text: result.data.text,
                                    confidence: result.data.confidence,
                                    words: result.data.words ? result.data.words.map(word => ({
                                        text: word.text,
                                        confidence: word.confidence,
                                        bbox: word.bbox
                                    })) : []
                                }
                            }, '*');

                        } catch (error) {
                            console.error('目标文档上下文OCR识别失败:', error);
                            window.postMessage({
                                type: 'OCR_RESULT',
                                id: '${messageId}',
                                success: false,
                                error: error.message
                            }, '*');
                        }
                    })();
                `;

                // 注入脚本到目标文档上下文
                (targetDocument.head || targetDocument.documentElement).appendChild(script);

                // 设置超时
                setTimeout(() => {
                    window.removeEventListener('message', messageHandler);
                    reject(new Error('OCR识别超时'));
                }, 30000); // 30秒超时

            } catch (error) {
                reject(error);
            }
        });
    }




    // 实现关闭详情方法
    async closeDetail() {
        try {
            //关闭候选人弹框
            //Xpath *[@id="boss-dynamic-dialog-1j38fleo5"]/div/div[2]
            for (let i = 0; i <= this.detailSelectors.closeButton.length; i++) {


                let aaa = document.getElementsByClassName(this.detailSelectors.closeButton[i]) ||
                    document.getElementsByClassName(this.detailSelectors.closeButton[i]) ||
                    document.querySelector(`[class*="${this.detailSelectors.closeButton[i]}"]`);
                if (aaa.length > 0) {
                    console.log("找到了关闭按钮");
                    console.log(aaa);

                    for (let i = 0; i <= aaa.length; i++) {
                        if (aaa[i]) {
                            aaa[i].click();
                        }
                    }


                } else {
                }
            }

            // let aaa = document.evaluate(this.detailSelectors.closeButtonXpath, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue;
            // if (aaa) {
            //     aaa.click();
            // } else {
            //     console.log('关闭按钮不存在');
            // }

            return true;
        } catch (error) {
            console.error('关闭详情失败:', error);
            return false;
        }
    }


    //查询同事沟通过候选人的信息
    async queryColleagueContactedInfo(candidate) {
        try {
            //参考boss_resume_downloader.js中的processNextCandidate方法
            for (let i = 0; i <= this.selectors.isContacted.length; i++) {
                let aaa = document.getElementsByClassName(this.selectors.isContacted[i]);
                if (aaa.length > 0) {
                    return aaa[0].textContent.trim();
                } else {
                }
            }
        } catch (error) {
            console.error('查询同事沟通过候选人的信息失败:', error);
        }
    }

    // Boss平台特定的测试OCR功能方法
    async testOCRFunction() {
        // 调用基类的测试方法，指定canvas ID为'resume'
        return await super.testOCRFunction('resume');
    }
}

export { BossParser };