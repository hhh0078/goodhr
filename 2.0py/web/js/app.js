document.addEventListener('DOMContentLoaded', function () {
    initApp();
});

let selectedJob = null;
let isRunning = false;
let config_data = {};
let versionInfo = {
    version: '',
    greetCount: 0,
    remainingQuota: 0,
    expiryDate: ''
};

let jobsData = [];
let keywordsData = {};

async function initApp() {
    // 加载配置
    await loadConfig();

    // 设置程序版本号
    await setProgramVersion();

    // 检查版本更新和公告
    await checkVersionAndAnnouncement();

    // 加载平台配置并渲染平台选择选项
    await loadPlatformConfigs();

    // 检查是否有手机号
    const phone = document.getElementById('phone').value.trim();
    if (!phone || phone.length !== 11) {
        // 如果基本设置中没有手机号，显示弹框
        showPhoneInputDialog();
    } else {
        // 如果已有手机号，尝试获取服务器数据
        await fetchJobList(phone);
    }

    // 初始化岗位和关键词数据
    initJobsAndKeywords();

    // 加载版本信息
    await loadVersionInfo();

    // 根据当前版本初始化UI显示
    const currentVersion = config_data.version || 'free';
    updateVersionUI(currentVersion);

    // 绑定事件监听器
    bindEventListeners();

    // 加载最近的日志
    await loadRecentLogs();

    addLog('应用已启动');
}

// 获取程序版本号并设置标题
async function setProgramVersion() {
    try {
        const updateInfo = await eel.check_version_and_announcement()();
        const version = updateInfo.currentVersion;
        document.title = `GoodHR 自动化工具 v${version} -17607080935`;
        document.querySelector('h1').textContent = `GoodHR 自动化工具 v${version}`;
    } catch (error) {
        console.error('获取版本号失败:', error);
    }
}

// 检查版本更新和公告
async function checkVersionAndAnnouncement() {
    try {
        // 获取版本信息
        const updateInfo = await eel.check_version_and_announcement()();

        if (updateInfo.needUpdate) {
            // 更新模态框内容
            document.getElementById('currentVersion').textContent = updateInfo.currentVersion;  // 使用当前版本
            document.getElementById('newVersion').textContent = updateInfo.version;  // 使用服务器版本
            document.getElementById('releaseNotes').textContent = updateInfo.releaseNotes;

            // 如果是强制更新，隐藏关闭按钮和"稍后更新"按钮
            if (updateInfo.forceUpdate) {
                document.getElementById('updateCloseBtn').style.display = 'none';
                document.getElementById('updateLaterBtn').style.display = 'none';
            }

            // 显示更新模态框
            const updateModal = new bootstrap.Modal(document.getElementById('updateModal'));
            updateModal.show();
        }

        // 如果有公告，显示公告
        if (updateInfo.announcement) {
            document.getElementById('announcementContent').textContent = updateInfo.announcement;
            const announcementModal = new bootstrap.Modal(document.getElementById('announcementModal'));
            announcementModal.show();
        }
    } catch (error) {
        console.error('检查版本更新失败:', error);
    }
}

function initJobsAndKeywords() {
    // 只有在jobsData为空时才初始化默认值
    if (!jobsData || jobsData.length === 0) {
        // 尝试从配置中加载岗位数据
        jobsData = config_data.jobsData || [];

        // 如果配置中也没有岗位数据，检查是否有手机号
        if (jobsData.length === 0) {
            if (config_data.username && config_data.username.length === 11) {
                addLog("检测到手机号，将从服务器获取数据");
                // 不立即设置默认值，让fetchJobList函数处理数据加载
                return;
            } else {
                // 只有在没有手机号的情况下才使用默认值，但不会自动同步到服务器
                jobsData = ['成都销售'];
                keywordsData = {
                    '成都销售': {
                        include: ['电话销售', '在线销售', '电销'],
                        exclude: ['保险销售'],
                        relation: 'OR',
                        description: ''
                    }
                };
                addLog("未检测到手机号，使用默认岗位和关键词数据（仅本地）");
                // 不调用 saveJobsAndKeywordsToConfig，避免同步到服务器
            }
        } else {
            addLog(`从本地缓存加载了 ${jobsData.length} 个岗位`);
        }
    }

    // 如果keywordsData为空，则从配置中加载
    if (!keywordsData || Object.keys(keywordsData).length === 0) {
        keywordsData = config_data.keywordsData || {};
        if (Object.keys(keywordsData).length > 0) {
            addLog(`从本地缓存加载了 ${Object.keys(keywordsData).length} 组关键词数据`);
        }
    }

    // 如果selectedJob为空，则从配置中加载
    if (!selectedJob) {
        selectedJob = config_data.selectedJob || null;
        if (selectedJob) {
            addLog(`从本地缓存加载了选中岗位: ${selectedJob}`);
        }
    }

    // 渲染岗位列表
    renderJobList();

    // 如果有选中的岗位，渲染关键词列表
    if (selectedJob) {
        renderKeywordList(selectedJob);
        enableKeywordButtons();
    } else {
        disableKeywordButtons();
    }
}

async function saveJobsAndKeywordsToConfig() {
    try {
        // 保存岗位数据
        await eel.save_config_item('jobsData', jobsData, '岗位数据')();

        // 保存关键词数据
        await eel.save_config_item('keywordsData', keywordsData, '关键词数据')();

        // 保存选中的岗位
        if (selectedJob) {
            await eel.save_config_item('selected_job', selectedJob, '选中岗位')();
        }

        addLog("岗位和关键词数据已保存，配置将同步到服务器");
        return true;
    } catch (error) {
        showError(`保存岗位和关键词数据失败: ${error.message}`);
        console.error("保存岗位和关键词数据出错:", error);
        return false;
    }
}

function renderJobList() {
    const jobListElement = document.getElementById('job-list');
    const noJobsElement = document.getElementById('no-jobs');

    if (!jobListElement) {
        console.error('找不到job-list元素');
        return;
    }

    jobListElement.innerHTML = '';

    if (jobsData.length === 0) {
        if (noJobsElement) {
            noJobsElement.style.display = 'block';
        }
        return;
    }

    if (noJobsElement) {
        noJobsElement.style.display = 'none';
    }

    jobsData.forEach(job => {
        const jobTag = document.createElement('div');
        jobTag.className = 'job-tag';
        jobTag.dataset.id = job;

        if (job === selectedJob) {
            jobTag.classList.add('selected');
        }

        jobTag.innerHTML = `
            ${job}
            <span class="delete-icon">×</span>
        `;

        jobTag.addEventListener('click', function (e) {
            if (e.target.classList.contains('delete-icon')) {
                return;
            }

            document.querySelectorAll('.job-tag').forEach(tag => {
                tag.classList.remove('selected');
            });

            this.classList.add('selected');

            selectedJob = job;
            saveSelectedJob(job);

            renderKeywordList(job);

            // 加载岗位描述（企业版需要显示岗位描述）
            const currentVersion = document.querySelector('input[name="version"]:checked');
            if (currentVersion && currentVersion.value === 'enterprise') {
                loadJobDescription(job);
            }

            enableKeywordButtons();
        });

        const deleteIcon = jobTag.querySelector('.delete-icon');
        if (deleteIcon) {
            deleteIcon.addEventListener('click', function (e) {
                e.stopPropagation();
                deleteJob(job);
            });
        }

        jobListElement.appendChild(jobTag);
    });
}

function enableKeywordButtons() {
    const addIncludeKeywordBtn = document.getElementById('add-include-keyword-btn');
    const addExcludeKeywordBtn = document.getElementById('add-exclude-keyword-btn');

    if (addIncludeKeywordBtn) {
        addIncludeKeywordBtn.disabled = false;
    }

    if (addExcludeKeywordBtn) {
        addExcludeKeywordBtn.disabled = false;
    }
}

function disableKeywordButtons() {
    const addIncludeKeywordBtn = document.getElementById('add-include-keyword-btn');
    const addExcludeKeywordBtn = document.getElementById('add-exclude-keyword-btn');

    if (addIncludeKeywordBtn) {
        addIncludeKeywordBtn.disabled = true;
    }

    if (addExcludeKeywordBtn) {
        addExcludeKeywordBtn.disabled = true;
    }
}

function renderKeywordList(job) {
    const keywordListElement = document.getElementById('keyword-list');
    const noKeywordsElement = document.getElementById('no-keywords');
    const relationSelectorElement = document.getElementById('keyword-relation');

    // 确保DOM元素存在
    if (!keywordListElement) {
        console.error('找不到keyword-list元素');
        return;
    }

    // 清空关键词列表
    keywordListElement.innerHTML = '';

    // 如果没有选中的岗位，显示提示信息
    if (!job) {
        if (noKeywordsElement) {
            noKeywordsElement.style.display = 'block';
        }
        if (relationSelectorElement) {
            relationSelectorElement.style.display = 'none';
        }
        return;
    }

    // 获取当前岗位的关键词
    const keywordData = keywordsData[job] || {
        include: [],
        exclude: [],
        relation: 'OR'
    };

    // 如果没有关键词，显示提示信息
    if (keywordData.include.length === 0 && keywordData.exclude.length === 0) {
        if (noKeywordsElement) {
            noKeywordsElement.textContent = '暂无关键词，请点击"添加关键词"按钮添加';
            noKeywordsElement.style.display = 'block';
        }
        if (relationSelectorElement) {
            relationSelectorElement.style.display = 'none';
        }
        return;
    }

    // 隐藏提示信息
    if (noKeywordsElement) {
        noKeywordsElement.style.display = 'none';
    }

    // 显示关系选择器
    if (relationSelectorElement) {
        relationSelectorElement.style.display = 'block';
        // 设置当前关系
        const andRadio = document.getElementById('relation-and');
        const orRadio = document.getElementById('relation-or');
        if (andRadio && orRadio) {
            if (keywordData.relation === 'AND') {
                andRadio.checked = true;
            } else {
                orRadio.checked = true;
            }
        }
    }

    // 添加关键词标题
    const keywordTitle = document.createElement('div');
    keywordTitle.className = 'keyword-section-title';
    keywordTitle.textContent = '关键词列表';
    keywordListElement.appendChild(keywordTitle);

    // 渲染所有关键词标签
    // 先渲染包含关键词
    keywordData.include.forEach(keyword => {
        const keywordTag = document.createElement('div');
        keywordTag.className = 'keyword-tag include';
        keywordTag.innerHTML = `
            ${keyword}
            <span class="delete-icon">×</span>
        `;

        // 点击删除图标时删除关键词
        const deleteIcon = keywordTag.querySelector('.delete-icon');
        if (deleteIcon) {
            deleteIcon.addEventListener('click', function () {
                deleteKeyword(job, keyword, 'include');
            });
        }

        keywordListElement.appendChild(keywordTag);
    });

    // 再渲染排除关键词
    keywordData.exclude.forEach(keyword => {
        const keywordTag = document.createElement('div');
        keywordTag.className = 'keyword-tag exclude';
        keywordTag.innerHTML = `
            ${keyword}
            <span class="delete-icon">×</span>
        `;

        // 点击删除图标时删除关键词
        const deleteIcon = keywordTag.querySelector('.delete-icon');
        if (deleteIcon) {
            deleteIcon.addEventListener('click', function () {
                deleteKeyword(job, keyword, 'exclude');
            });
        }

        keywordListElement.appendChild(keywordTag);
    });
}

// 添加岗位
function addJob(jobName) {
    // 添加岗位
    jobsData.push(jobName);

    // 初始化关键词数组和岗位描述
    keywordsData[jobName] = {
        include: [],
        exclude: [],
        relation: 'OR',
        description: ''  // 添加岗位描述字段
    };

    // 保存数据到配置文件
    saveJobsAndKeywordsToConfig();

    // 选中新添加的岗位
    selectedJob = jobName;
    saveSelectedJob(jobName);

    // 重新渲染岗位列表
    renderJobList();

    // 渲染关键词列表
    renderKeywordList(jobName);

    // 启用添加关键词按钮
    enableKeywordButtons();

    // 确保新添加的岗位在UI上被选中
    const jobTags = document.querySelectorAll('.job-tag');
    jobTags.forEach(tag => {
        if (tag.dataset.id === jobName) {
            // 移除其他岗位的选中样式
            jobTags.forEach(t => t.classList.remove('selected'));
            // 添加当前岗位的选中样式
            tag.classList.add('selected');
            // 滚动到该元素
            tag.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
    });

    return jobName;
}

// 删除岗位
function deleteJob(jobName) {
    // 确认删除
    const confirmModal = new bootstrap.Modal(document.getElementById('customAlertModal'));
    document.getElementById('customAlertTitle').textContent = "确认删除";
    document.getElementById('customAlertMessage').textContent = `确定要删除岗位"${jobName}"吗？相关的关键词也会被删除。`;

    // 替换确定按钮
    const footer = document.querySelector('#customAlertModal .modal-footer');
    const originalFooterContent = footer.innerHTML;
    footer.innerHTML = `
        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">取消</button>
        <button type="button" class="btn btn-danger" id="confirmDeleteJob">删除</button>
    `;

    // 添加确认删除事件
    document.getElementById('confirmDeleteJob').addEventListener('click', function () {
        // 从数组中删除岗位
        jobsData = jobsData.filter(job => job !== jobName);

        // 删除关联的关键词
        delete keywordsData[jobName];

        // 保存数据到配置文件
        saveJobsAndKeywordsToConfig();

        // 如果删除的是当前选中的岗位，清除选中状态
        if (jobName === selectedJob) {
            selectedJob = null;
            saveSelectedJob('');

            // 禁用添加关键词按钮
            disableKeywordButtons();
        }

        // 重新渲染岗位列表
        renderJobList();

        // 重新渲染关键词列表
        renderKeywordList(selectedJob);

        // 添加日志
        addLog(`已删除岗位: ${jobName}`);

        // 关闭模态框
        confirmModal.hide();

        // 恢复原始的footer内容
        footer.innerHTML = originalFooterContent;
    });

    // 显示模态框
    confirmModal.show();

    // 监听模态框关闭事件，恢复原始的footer内容
    const modalElement = document.getElementById('customAlertModal');
    modalElement.addEventListener('hidden.bs.modal', function onHidden() {
        footer.innerHTML = originalFooterContent;
        modalElement.removeEventListener('hidden.bs.modal', onHidden);
    });
}

// 添加关键词
function addKeyword(jobName, keyword, type = 'include') {
    // 确保关键词数据结构存在
    if (!keywordsData[jobName]) {
        keywordsData[jobName] = {
            include: [],
            exclude: [],
            relation: 'OR'
        };
    }

    // 检查关键词是否已存在
    if (keywordsData[jobName].include.includes(keyword) || keywordsData[jobName].exclude.includes(keyword)) {
        showAlert('该关键词已存在');
        return false;
    }

    // 根据类型添加关键词
    if (type === 'include') {
        keywordsData[jobName].include.push(keyword);
    } else {
        keywordsData[jobName].exclude.push(keyword);
    }

    // 保存数据到配置文件
    saveJobsAndKeywordsToConfig();

    // 重新渲染关键词列表
    renderKeywordList(jobName);

    return true;
}

// 设置关键词关系
function setKeywordRelation(jobName, relation) {
    // 确保关键词数据结构存在
    if (!keywordsData[jobName]) {
        keywordsData[jobName] = {
            include: [],
            exclude: [],
            relation: relation
        };
    } else {
        keywordsData[jobName].relation = relation;
    }

    // 保存数据到配置文件
    saveJobsAndKeywordsToConfig();

    // 添加日志
    addLog(`已设置关键词关系为: ${relation === 'AND' ? '全部匹配' : '任一匹配'}`);
}

// 删除关键词
function deleteKeyword(jobName, keyword, type) {
    // 使用自定义确认弹框
    const confirmModal = new bootstrap.Modal(document.getElementById('customAlertModal'));
    document.getElementById('customAlertTitle').textContent = "确认删除";
    document.getElementById('customAlertMessage').textContent = `确定要删除${type === 'include' ? '包含关键词' : '排除关键词'}"${keyword}"吗？`;

    // 替换确定按钮
    const footer = document.querySelector('#customAlertModal .modal-footer');
    const originalFooterContent = footer.innerHTML;
    footer.innerHTML = `
        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">取消</button>
        <button type="button" class="btn btn-danger" id="confirmDeleteKeyword">删除</button>
    `;

    // 添加确认删除事件
    document.getElementById('confirmDeleteKeyword').addEventListener('click', function () {
        // 从数组中删除关键词
        if (type === 'include') {
            keywordsData[jobName].include = keywordsData[jobName].include.filter(k => k !== keyword);
        } else {
            keywordsData[jobName].exclude = keywordsData[jobName].exclude.filter(k => k !== keyword);
        }

        // 保存数据到配置文件
        saveJobsAndKeywordsToConfig();

        // 重新渲染关键词列表
        renderKeywordList(jobName);

        // 添加日志
        addLog(`已删除${type === 'include' ? '包含关键词' : '排除关键词'}: ${keyword}`);

        // 关闭模态框
        confirmModal.hide();

        // 监听模态框关闭事件，在完全关闭后恢复原始状态
        const modalElement = document.getElementById('customAlertModal');
        modalElement.addEventListener('hidden.bs.modal', function onHidden() {
            // 恢复模态框原始状态
            footer.innerHTML = originalFooterContent;
            // 移除事件监听器，避免多次触发
            modalElement.removeEventListener('hidden.bs.modal', onHidden);
        }, { once: true });
    });

    confirmModal.show();
}

// 加载配置
async function loadConfig() {
    try {
        addLog("正在从本地缓存加载配置...");
        const config = await eel.get_config()();

        // 保存全局配置数据
        config_data = config;
        addLog("本地缓存配置加载完成");

        // 设置版本选择
        if (config.version) {
            // 更新隐藏的单选按钮
            const versionRadio = document.querySelector(`input[name="version"][value="${config.version}"]`);
            if (versionRadio) {
                versionRadio.checked = true;
                addLog(`已从本地缓存加载版本: ${getVersionDisplayName(config.version)}`);
            }

            // 更新导航栏选中状态
            updateVersionNavSelection(config.version);
        }

        // 设置平台选择
        if (config.platform) {
            const platformRadio = document.querySelector(`input[name="platform"][value="${config.platform}"]`);
            if (platformRadio) {
                platformRadio.checked = true;
                addLog(`已从本地缓存加载招聘平台: ${config.platform}`);
            }
        }

        // 设置手机号
        if (config.username) {
            document.getElementById('phone').value = config.username;
            addLog(`已从本地缓存加载手机号: ${config.username}`);
        }

        // 设置浏览器路径
        if (config.browser_path) {
            document.getElementById('browser-path').value = config.browser_path;
            addLog(`已从本地缓存加载浏览器路径: ${config.browser_path}`);
        }

        // 设置延迟时间
        if (config.minDelay !== undefined && config.maxDelay !== undefined) {
            document.getElementById('min-delay').value = config.minDelay;
            document.getElementById('max-delay').value = config.maxDelay;
            addLog(`已从本地缓存加载延迟时间设置: ${config.minDelay}-${config.maxDelay}秒`);
        } else {
            // 设置默认值
            document.getElementById('min-delay').value = 7;
            document.getElementById('max-delay').value = 12;
        }

        // 设置选中的岗位
        selectedJob = config.selectedJob || null;

        // 如果有选中的岗位，启用添加关键词按钮
        if (selectedJob) {
            enableKeywordButtons();
            addLog(`已从本地缓存加载选中岗位: ${selectedJob}`);
        }

        // 加载岗位数据
        if (config.jobsData && config.jobsData.length > 0) {
            jobsData = config.jobsData;
            addLog(`已从本地缓存加载 ${jobsData.length} 个岗位`);
        }

        // 加载关键词数据
        if (config.keywordsData && Object.keys(config.keywordsData).length > 0) {
            keywordsData = config.keywordsData;
            addLog(`已从本地缓存加载 ${Object.keys(keywordsData).length} 组关键词数据`);
        }

        // 重新渲染岗位列表和关键词列表
        renderJobList();
        if (selectedJob) {
            renderKeywordList(selectedJob);
        }

        // 初始化岗位描述存储
        if (!config.jobDescriptions) {
            config.jobDescriptions = {};
        }

        return config;
    } catch (error) {
        console.error('加载配置失败:', error);
        addLog('加载配置失败: ' + error.toString());
        return {};
    }
}

// 加载版本信息
async function loadVersionInfo() {
    try {
        const versionInfo = await eel.get_version_info()();
        updateVersionInfoDisplay(versionInfo);
        return versionInfo;
    } catch (error) {
        console.error('加载版本信息失败:', error);
        return null;
    }
}

// 更新版本信息显示
function updateVersionInfoDisplay(versionInfo) {
    const currentVersion = document.getElementById('current-version');
    const greetCount = document.getElementById('greet-count');
    const remainingQuota = document.getElementById('remaining-quota');
    const expiryDate = document.getElementById('expiry-date');

    if (versionInfo) {
        // 显示当前版本
        currentVersion.textContent = getVersionDisplayName(versionInfo.version);

        // 显示打招呼次数
        greetCount.textContent = versionInfo.greetCount || 0;

        // 根据版本显示不同的额度信息
        if (versionInfo.version === 'enterprise') {
            // 企业版显示剩余分析次数
            remainingQuota.textContent = `${versionInfo.remainingQuota || 0}次`;
            // 显示企业版到期时间
            expiryDate.textContent = versionInfo.expiryDate || '无限制';
        } else if (versionInfo.version === 'free') {
            // 免费版显示剩余打招呼次数
            remainingQuota.textContent = versionInfo.remainingQuota || 0;
            expiryDate.textContent = '每日重置';
        } else if (versionInfo.version === 'donation') {
            // 捐赠版显示到期时间
            remainingQuota.textContent = '无限制';
            expiryDate.textContent = versionInfo.expiryDate || '-';
        } else {
            remainingQuota.textContent = '0';
            expiryDate.textContent = '-';
        }
    } else {
        currentVersion.textContent = '未选择';
        greetCount.textContent = '0';
        remainingQuota.textContent = '0';
        expiryDate.textContent = '-';
    }
}

// 获取版本显示名称
function getVersionDisplayName(version) {
    switch (version) {
        case 'free': return '免费版';
        case 'donation': return '捐赠版';
        case 'enterprise': return '企业版';
        default: return '未选择';
    }
}

// 更新版本导航栏选中状态
function updateVersionNavSelection(version) {
    // 移除所有导航项的选中样式
    document.querySelectorAll('.version-nav .nav-link').forEach(link => {
        link.classList.remove('active');
    });

    // 为选中的版本导航项添加选中样式
    if (version) {
        const navLink = document.querySelector(`#version-${version}`);
        if (navLink) {
            navLink.classList.add('active');
        }
    }
}

// 绑定事件监听器
function bindEventListeners() {
    // 功能选择变化事件
    document.querySelectorAll('input[name="function"]').forEach(radio => {
        radio.addEventListener('change', function () {
            if (this.disabled) {
                showError('该功能正在开发中，敬请期待！');
                // 重置为打招呼功能
                document.getElementById('function1').checked = true;
                return;
            }
            addLog(`已选择功能: ${this.nextElementSibling.textContent.trim()}`);
        });
    });

    // 版本导航栏点击事件
    document.querySelectorAll('.version-nav .nav-link').forEach(link => {
        link.addEventListener('click', async function (e) {
            e.preventDefault();
            const version = this.dataset.value;

            // 更新隐藏的单选按钮
            const radio = document.querySelector(`input[name="version"][value="${version}"]`);
            if (radio) {
                radio.checked = true;
            }

            // 保存版本选择并更新UI
            await saveVersion(version);
            updateVersionUI(version);
            await loadVersionInfo();

            // 更新导航栏选中状态
            updateVersionNavSelection(version);
        });
    });

    // 平台选择变化
    document.querySelectorAll('input[name="platform"]').forEach(radio => {
        radio.addEventListener('change', function () {
            savePlatform(this.value);
        });
    });

    // 手机号输入变化
    const phoneInput = document.getElementById('phone');
    phoneInput.addEventListener('input', async function () {
        const phone = this.value.trim();

        // 保存手机号
        await saveUsername(phone);

        // 如果手机号是11位，尝试从服务器获取配置
        if (phone.length === 11 && /^1\d{10}$/.test(phone)) {
            try {
                addLog(`检测到完整手机号 ${phone}，尝试从服务器获取配置...`);

                // 显示加载中的提示
                showAlert("正在从服务器获取数据，请稍候...");

                // 获取岗位列表
                const serverData = await eel.fetch_job_list(phone)();
                if (!serverData) {
                    addLog("从服务器获取岗位列表失败");
                    return;
                }

                // 获取当前的关键词数据
                const config = await eel.get_config()();
                const currentKeywordsData = config.keywordsData || {};

                // 更新岗位列表
                jobsData = serverData.jobs || [];

                // 合并关键词数据，保留现有的岗位描述
                const newKeywordsData = {};
                for (const job of jobsData) {
                    // 如果服务器有这个岗位的数据，使用服务器数据
                    if (serverData.keywords && serverData.keywords[job]) {
                        newKeywordsData[job] = {
                            ...serverData.keywords[job],
                            // 如果本地有描述，保留本地描述
                            description: (currentKeywordsData[job] && currentKeywordsData[job].description) || ''
                        };
                    }
                    // 如果服务器没有这个岗位的数据，但本地有，保留本地数据
                    else if (currentKeywordsData[job]) {
                        newKeywordsData[job] = currentKeywordsData[job];
                    }
                    // 如果都没有，创建新的空数据
                    else {
                        newKeywordsData[job] = {
                            include: [],
                            exclude: [],
                            relation: 'OR',
                            description: ''
                        };
                    }
                }

                // 更新关键词数据
                keywordsData = newKeywordsData;
                await eel.save_config_item('keywordsData', keywordsData, '关键词数据')();

                // 更新UI
                renderJobList();
                addLog("已更新岗位列表和关键词数据");

            } catch (error) {
                console.error("获取服务器配置时出错:", error);
                addLog(`获取服务器配置时出错: ${error.toString()}`);
                showError(`获取服务器配置时出错: ${error.toString()}`);
            }
        }
    });

    // 延迟时间输入框变化
    const minDelayInput = document.getElementById('min-delay');
    const maxDelayInput = document.getElementById('max-delay');

    // 最小延迟时间变化
    minDelayInput.addEventListener('change', function () {
        const minDelay = parseInt(this.value);
        const maxDelay = parseInt(maxDelayInput.value);

        // 验证输入范围
        if (minDelay < 5) {
            this.value = 5;
        } else if (minDelay > 20) {
            this.value = 20;
        }

        // 确保最小值不大于最大值
        if (minDelay > maxDelay) {
            maxDelayInput.value = minDelay;
        }

        // 保存延迟时间设置
        saveDelaySettings();
    });

    // 最大延迟时间变化
    maxDelayInput.addEventListener('change', function () {
        const maxDelay = parseInt(this.value);
        const minDelay = parseInt(minDelayInput.value);

        // 验证输入范围
        if (maxDelay < 5) {
            this.value = 5;
        } else if (maxDelay > 20) {
            this.value = 20;
        }

        // 确保最大值不小于最小值
        if (maxDelay < minDelay) {
            minDelayInput.value = maxDelay;
        }

        // 保存延迟时间设置
        saveDelaySettings();
    });

    // 选择浏览器按钮点击
    document.getElementById('select-browser').addEventListener('click', function () {
        selectBrowser();
    });

    // 开始按钮点击
    document.getElementById('start-button').addEventListener('click', function () {
        startAutomation();
    });

    // 停止按钮点击
    document.getElementById('stop-button').addEventListener('click', function () {
        stopAutomation();
    });

    // 清空日志按钮点击
    document.getElementById('clear-log').addEventListener('click', function () {
        clearLog();
    });

    // 添加岗位按钮点击
    document.getElementById('add-job-btn').addEventListener('click', function () {
        // 使用自定义输入弹框替代模态框
        showInputDialog("请输入岗位名称", "添加岗位", function (jobName) {
            // 添加岗位
            addJob(jobName);
            addLog(`已添加岗位: ${jobName}`);
        });
    });

    // 添加包含关键词按钮点击
    document.getElementById('add-include-keyword-btn').addEventListener('click', function () {
        // 如果没有选中的岗位，不执行操作
        if (!selectedJob) {
            showAlert('请先选择岗位');
            return;
        }

        // 使用自定义输入弹框
        showInputDialog("请输入包含关键词", "添加包含关键词", function (keyword) {
            // 添加关键词
            if (addKeyword(selectedJob, keyword, 'include')) {
                addLog(`已添加包含关键词: ${keyword}`);
            }
        });
    });

    // 添加排除关键词按钮点击
    document.getElementById('add-exclude-keyword-btn').addEventListener('click', function () {
        // 如果没有选中的岗位，不执行操作
        if (!selectedJob) {
            showAlert('请先选择岗位');
            return;
        }

        // 使用自定义输入弹框
        showInputDialog("请输入排除关键词", "添加排除关键词", function (keyword) {
            // 添加关键词
            if (addKeyword(selectedJob, keyword, 'exclude')) {
                addLog(`已添加排除关键词: ${keyword}`);
            }
        });
    });

    // 关键词关系选择变化
    document.querySelectorAll('input[name="keyword-relation"]').forEach(radio => {
        radio.addEventListener('change', function () {
            if (selectedJob) {
                setKeywordRelation(selectedJob, this.value);
            }
        });
    });

    // 监听ESC键
    document.addEventListener('keydown', function (event) {
        if (event.key === 'Escape' && isRunning) {
            stopAutomation();
        }
    });

    // 监听回车键
    const jobNameInput = document.getElementById('job-name');
    if (jobNameInput) {
        jobNameInput.addEventListener('keydown', function (event) {
            if (event.key === 'Enter') {
                const saveJobBtn = document.getElementById('save-job-btn');
                if (saveJobBtn) {
                    saveJobBtn.click();
                }
            }
        });
    }

    const keywordTextInput = document.getElementById('keyword-text');
    if (keywordTextInput) {
        keywordTextInput.addEventListener('keydown', function (event) {
            if (event.key === 'Enter') {
                const saveKeywordBtn = document.getElementById('save-keyword-btn');
                if (saveKeywordBtn) {
                    saveKeywordBtn.click();
                }
            }
        });
    }

    // 修改获取岗位按钮的点击事件处理
    const fetchJobsBtn = document.getElementById('fetch-jobs-btn');
    const usernameInput = document.getElementById('username-input');
    if (fetchJobsBtn) {
        fetchJobsBtn.addEventListener('click', async function () {
            if (!usernameInput) {
                showError('找不到手机号输入框');
                return;
            }
            const phone = usernameInput.value.trim();
            if (!phone) {
                showError('请输入手机号');
                return;
            }

            // 保存手机号
            await saveUsername(phone);

            // 获取岗位列表
            await fetchJobList(phone);
        });
    }

    // 绑定岗位描述相关事件
    bindJobDescriptionEvents();

    // 初始化时根据当前版本更新UI
    const currentVersion = document.querySelector('input[name="version"]:checked');
    if (currentVersion) {
        updateVersionUI(currentVersion.value);
    }
}

// 保存版本选择
async function saveVersion(version) {
    try {
        const saved = await eel.save_config_item('version', version, '版本')();
        if (!saved) {
            throw new Error('保存版本失败');
        }
        return true;
    } catch (error) {
        console.error('保存版本出错:', error);
        showError('保存版本失败，请重试');
        return false;
    }
}

// 获取岗位列表
async function fetchJobList(phone) {
    try {
        // 获取服务器数据
        const serverData = await eel.fetch_job_list(phone)();
        if (!serverData) {
            addLog("从服务器获取岗位列表失败");
            return;
        }

        // 获取当前的关键词数据
        const config = await eel.get_config()();
        const currentKeywordsData = config.keywordsData || {};

        // 更新岗位列表
        jobsData = serverData.jobs || [];

        // 合并关键词数据，保留现有的岗位描述
        const newKeywordsData = {};
        for (const job of jobsData) {
            // 如果服务器有这个岗位的数据，使用服务器数据
            if (serverData.keywords && serverData.keywords[job]) {
                newKeywordsData[job] = {
                    ...serverData.keywords[job],
                    // 如果本地有描述，保留本地描述
                    description: (currentKeywordsData[job] && currentKeywordsData[job].description) || ''
                };
            }
            // 如果服务器没有这个岗位的数据，但本地有，保留本地数据
            else if (currentKeywordsData[job]) {
                newKeywordsData[job] = currentKeywordsData[job];
            }
            // 如果都没有，创建新的空数据
            else {
                newKeywordsData[job] = {
                    include: [],
                    exclude: [],
                    relation: 'OR',
                    description: ''
                };
            }
        }

        // 更新关键词数据
        keywordsData = newKeywordsData;
        await eel.save_config_item('keywordsData', keywordsData, '关键词数据')();

        // 更新UI
        renderJobList();
        addLog("已更新岗位列表和关键词数据");

    } catch (error) {
        addLog(`获取岗位列表出错: ${error}`);
        showError("获取岗位列表失败，请稍后重试" + error);
    }
}

// 更新岗位列表
function updateJobList(jobs) {
    // 检查jobs是否为有效数据
    if (!jobs || !Array.isArray(jobs) || jobs.length === 0) {
        addLog("没有获取到有效的岗位数据");

        // 清空岗位列表UI
        const jobListElement = document.getElementById('job-list');
        if (jobListElement) {
            jobListElement.innerHTML = `
                <div class="text-center text-muted" id="no-jobs">
                    未找到岗位，请点击"添加岗位"按钮添加
                </div>
            `;
        }
        return;
    }

    // 提取岗位名称
    let jobNames = [];

    // 处理不同格式的岗位数据
    jobs.forEach(job => {
        // 如果是API返回的格式，使用positionName或name字段
        if (typeof job === 'object') {
            const jobName = job.positionName || job.name || job.id;
            if (jobName) {
                jobNames.push(jobName);
            }
        }
        // 如果是字符串格式，直接使用
        else if (typeof job === 'string') {
            jobNames.push(job);
        }
    });

    addLog(`提取了 ${jobNames.length} 个岗位名称`);

    // 更新全局jobsData
    jobsData = jobNames;

    // 清空岗位列表
    const jobListElement = document.getElementById('job-list');
    const noJobsElement = document.getElementById('no-jobs');

    if (!jobListElement) {
        console.error('找不到job-list元素');
        return;
    }

    jobListElement.innerHTML = '';

    if (jobNames.length === 0) {
        if (noJobsElement) {
            noJobsElement.style.display = 'block';
        }
        return;
    }

    if (noJobsElement) {
        noJobsElement.style.display = 'none';
    }

    // 添加岗位到列表
    jobNames.forEach(job => {
        const jobTag = document.createElement('div');
        jobTag.className = 'job-tag';
        jobTag.dataset.id = job;

        if (job === selectedJob) {
            jobTag.classList.add('selected');
        }

        jobTag.innerHTML = `
            ${job}
            <span class="delete-icon">×</span>
        `;

        jobTag.addEventListener('click', function (e) {
            if (e.target.classList.contains('delete-icon')) {
                return;
            }

            document.querySelectorAll('.job-tag').forEach(tag => {
                tag.classList.remove('selected');
            });

            this.classList.add('selected');

            selectedJob = job;
            saveSelectedJob(job);

            renderKeywordList(job);

            // 加载岗位描述（企业版需要显示岗位描述）
            const currentVersion = document.querySelector('input[name="version"]:checked');
            if (currentVersion && currentVersion.value === 'enterprise') {
                loadJobDescription(job);
            }

            enableKeywordButtons();
        });

        const deleteIcon = jobTag.querySelector('.delete-icon');
        if (deleteIcon) {
            deleteIcon.addEventListener('click', function (e) {
                e.stopPropagation();
                deleteJob(job);
            });
        }

        jobListElement.appendChild(jobTag);
    });

    // 如果没有选中的岗位，选择第一个岗位
    if (!selectedJob && jobNames.length > 0) {
        selectedJob = jobNames[0];
        saveSelectedJob(selectedJob);
        renderKeywordList(selectedJob);
        enableKeywordButtons();

        // 选中第一个岗位标签
        const firstJobTag = jobListElement.querySelector('.job-tag');
        if (firstJobTag) {
            firstJobTag.classList.add('selected');
        }

        addLog(`已自动选择岗位: ${selectedJob}`);
    }

    // 保存岗位数据到配置
    saveJobsAndKeywordsToConfig();
}

// 保存平台选择
async function savePlatform(platform) {
    try {
        const saved = await eel.save_config_item('platform', platform, '招聘平台')();
        if (saved) {
            addLog(`已选择招聘平台: ${platform}`);
        } else {
            throw new Error('保存平台失败');
        }
    } catch (error) {
        console.error('保存平台出错:', error);
        showError('保存平台失败，请重试');
    }
}

// 保存用户名（手机号）
async function saveUsername(username) {
    try {
        const saved = await eel.save_config_item('username', username, '手机号')();
        if (saved) {
            addLog(`已保存手机号: ${username}`);
        } else {
            throw new Error('保存手机号失败');
        }
    } catch (error) {
        console.error('保存手机号出错:', error);
        showError('保存手机号失败，请重试');
    }
}

// 保存选中的岗位
async function saveSelectedJob(jobName) {
    try {
        const saved = await eel.save_config_item('selectedJob', jobName, '选中岗位')();
        if (saved) {
            addLog(`已选择岗位: ${jobName}`);
        } else {
            throw new Error('保存选中岗位失败');
        }
    } catch (error) {
        console.error('保存选中岗位出错:', error);
        showError('保存选中岗位失败，请重试');
    }
}

// 选择浏览器
async function selectBrowser() {
    try {
        const browserPath = await eel.select_browser()();
        if (browserPath) {
            document.getElementById('browser-path').value = browserPath;
        }
    } catch (error) {
        console.error('选择浏览器失败:', error);
        addLog('选择浏览器失败: ' + error.toString());
    }
}

// 开始自动化流程
async function startAutomation() {
    try {
        // 检查是否选择了版本
        const version = document.querySelector('input[name="version"]:checked');
        if (!version) {
            showError('请选择版本');
            return;
        }

        // 检查是否选择了平台
        const platform = document.querySelector('input[name="platform"]:checked');
        if (!platform) {
            showError('请选择招聘平台');
            return;
        }

        // 检查是否输入了手机号
        const phone = document.getElementById('phone').value.trim();
        if (!phone || phone.length !== 11) {
            showError('请输入正确的手机号');
            return;
        }

        // 检查是否选择了岗位
        if (!selectedJob) {
            showError('请选择岗位');
            return;
        }

        // 获取岗位描述
        const jobDescription = document.getElementById('job-description').value.trim();
        if (version.value === 'enterprise' && !jobDescription) {
            showError('未找到岗位描述，请先填写岗位描述');
            return;
        }

        // 获取停止数量
        const stopCount = parseInt(document.getElementById('stop-count').value);
        if (isNaN(stopCount) || stopCount < 1) {
            showError('请设置有效的停止数量');
            return;
        }

        // 检查关键词（非企业版）
        if (version.value !== 'enterprise') {
            const keywordData = keywordsData[selectedJob] || {};
            if (!keywordData.include || keywordData.include.length === 0) {
                showError('请为选中的岗位添加包含关键词');
                return;
            }
        }

        // 获取延迟时间设置
        const minDelay = parseInt(document.getElementById('min-delay').value);
        const maxDelay = parseInt(document.getElementById('max-delay').value);

        // 验证延迟时间设置
        if (isNaN(minDelay) || isNaN(maxDelay) || minDelay < 5 || minDelay > 20 || maxDelay < 5 || maxDelay > 20 || minDelay > maxDelay) {
            showError('延迟时间设置无效，请确保最小延迟和最大延迟在5-20秒之间，且最小延迟不大于最大延迟');
            return;
        }

        // 检查版本限制
        if (version.value === 'free' && versionInfo.greetCount >= 100) {
            showError('免费版每天只能打100个招呼，今日额度已用完');
            return;
        }

        // 检查企业版余额
        if (version.value === 'enterprise') {
            const latestVersionInfo = await eel.get_version_info()();
            versionInfo = latestVersionInfo;
            if (typeof versionInfo.remainingQuota === 'undefined' || versionInfo.remainingQuota <= 0) {
                showError('企业版余额不足，请充值后再使用');
                return;
            }
        }

        // 准备自动化配置
        const automationConfig = {
            version: version.value,
            platform: platform.value,
            phone: phone,
            jobName: selectedJob,
            jobDescription: jobDescription,
            stopCount: stopCount,
            keywords: version.value === 'enterprise' ? null : {
                include: keywordsData[selectedJob]?.include || [],
                exclude: keywordsData[selectedJob]?.exclude || [],
                relation: keywordsData[selectedJob]?.relation || 'OR'
            },
            delay: {
                min: minDelay,
                max: maxDelay
            }
        };

        // 更新按钮状态
        isRunning = true;
        updateButtonState();

        // 开始自动化流程
        addLog('正在启动自动化流程...');
        addLog(`岗位: ${automationConfig.jobName}`);
        if (version.value === 'enterprise') {
            addLog('使用AI智能匹配模式');
        } else {
            addLog(`包含关键词: ${automationConfig.keywords.include.join(', ')}`);
            if (automationConfig.keywords.exclude.length > 0) {
                addLog(`排除关键词: ${automationConfig.keywords.exclude.join(', ')}`);
            }
            addLog(`关键词匹配方式: ${automationConfig.keywords.relation === 'AND' ? '全部匹配' : '任一匹配'}`);
        }
        addLog(`停止条件: ${automationConfig.stopCount}次`);
        addLog(`候选人打开延迟时间: ${automationConfig.delay.min}-${automationConfig.delay.max}秒`);

        // 调用Python函数开始自动化
        await eel.start_automation(automationConfig)();
    } catch (error) {
        console.error('启动自动化流程失败:', error);
        addLog('启动自动化流程失败: ' + error.toString());
        showError('启动自动化流程失败: ' + error.toString());

        // 恢复按钮状态
        isRunning = false;
        updateButtonState();
    }
}

// 停止自动化流程
async function stopAutomation() {
    try {
        addLog('正在停止自动化流程...');
        await eel.stop_automation()();

        // 更新按钮状态
        isRunning = false;
        updateButtonState();
    } catch (error) {
        console.error('停止自动化流程失败:', error);
        addLog('停止自动化流程失败: ' + error.toString());
    }
}

// 更新按钮状态
function updateButtonState() {
    const startButton = document.getElementById('start-button');
    const stopButton = document.getElementById('stop-button');

    if (isRunning) {
        startButton.disabled = true;
        stopButton.disabled = false;
    } else {
        startButton.disabled = false;
        stopButton.disabled = true;
    }
}

// 加载最近的日志
async function loadRecentLogs() {
    try {
        const logs = await eel.get_recent_logs()();
        const logContainer = document.getElementById('log-container');

        logs.forEach(log => {
            const logEntry = document.createElement('div');
            logEntry.className = 'log-entry';
            logEntry.textContent = log;
            logContainer.appendChild(logEntry);
        });

        // 滚动到底部
        logContainer.scrollTop = logContainer.scrollHeight;
    } catch (error) {
        console.error('加载最近日志失败:', error);
    }
}

// 添加日志
function addLog(message) {
    const logContainer = document.getElementById('log-container');
    const time = new Date().toTimeString().split(' ')[0];
    const logEntry = document.createElement('div');
    logEntry.className = 'log-entry';

    // 判断是否为错误日志
    const isError = /错误|失败|出错|异常|error|failed|exception/i.test(message);

    if (isError) {
        logEntry.classList.add('log-error');
    }

    logEntry.textContent = `[${time}] ${message}`;
    logContainer.appendChild(logEntry);

    // 滚动到底部
    logContainer.scrollTop = logContainer.scrollHeight;
}

// 清空日志
function clearLog() {
    document.getElementById('log-container').innerHTML = '';
    addLog('日志已清空');
}

// 显示自定义提示弹框
function showAlert(message, title = "提示") {
    // 关闭所有已打开的模态框
    const openModals = document.querySelectorAll('.modal.show');
    openModals.forEach(modalEl => {
        const modalInstance = bootstrap.Modal.getInstance(modalEl);
        if (modalInstance) {
            modalInstance.hide();
        }
    });

    // 获取模态框元素
    const modalElement = document.getElementById('customAlertModal');

    // 移除之前可能存在的事件监听器
    const newModalElement = modalElement.cloneNode(true);
    modalElement.parentNode.replaceChild(newModalElement, modalElement);

    // 设置标题和消息
    newModalElement.querySelector('#customAlertTitle').textContent = title;
    newModalElement.querySelector('#customAlertMessage').textContent = message;

    // 保存触发模态框的元素
    const previousActiveElement = document.activeElement;

    // 添加关闭事件监听器
    newModalElement.addEventListener('hidden.bs.modal', function () {
        // 在移除模态框之前，确保焦点返回到之前的元素
        if (previousActiveElement) {
            previousActiveElement.focus();
        }

        // 移除所有 inert 属性
        document.querySelectorAll('[inert]').forEach(el => {
            el.removeAttribute('inert');
        });

        // 确保模态框背景被移除
        const modalBackdrops = document.querySelectorAll('.modal-backdrop');
        modalBackdrops.forEach(backdrop => {
            backdrop.parentNode.removeChild(backdrop);
        });

        // 移除body上的modal-open类
        document.body.classList.remove('modal-open');
        document.body.style.overflow = '';
        document.body.style.paddingRight = '';
    });

    // 显示模态框时设置其他元素为 inert
    newModalElement.addEventListener('shown.bs.modal', function () {
        // 将主要内容区域设置为 inert
        document.querySelector('.container').setAttribute('inert', '');

        // 聚焦到模态框的第一个可聚焦元素
        const focusableElements = newModalElement.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
        if (focusableElements.length > 0) {
            focusableElements[0].focus();
        }
    });

    // 显示模态框
    const modal = new bootstrap.Modal(newModalElement);
    modal.show();
}

// 显示自定义错误弹框
function showError(message) {
    // 关闭所有已打开的模态框
    const openModals = document.querySelectorAll('.modal.show');
    openModals.forEach(modalEl => {
        const modalInstance = bootstrap.Modal.getInstance(modalEl);
        if (modalInstance) {
            modalInstance.hide();
        }
    });

    // 获取模态框元素
    const modalElement = document.getElementById('customErrorModal');

    // 移除之前可能存在的事件监听器
    const newModalElement = modalElement.cloneNode(true);
    modalElement.parentNode.replaceChild(newModalElement, modalElement);

    // 设置消息
    newModalElement.querySelector('#customErrorMessage').textContent = message;

    // 保存触发模态框的元素
    const previousActiveElement = document.activeElement;

    // 添加关闭事件监听器
    newModalElement.addEventListener('hidden.bs.modal', function () {
        // 在移除模态框之前，确保焦点返回到之前的元素
        if (previousActiveElement) {
            previousActiveElement.focus();
        }

        // 移除所有 inert 属性
        document.querySelectorAll('[inert]').forEach(el => {
            el.removeAttribute('inert');
        });

        // 确保模态框背景被移除
        const modalBackdrops = document.querySelectorAll('.modal-backdrop');
        modalBackdrops.forEach(backdrop => {
            backdrop.parentNode.removeChild(backdrop);
        });

        // 移除body上的modal-open类
        document.body.classList.remove('modal-open');
        document.body.style.overflow = '';
        document.body.style.paddingRight = '';
    });

    // 显示模态框时设置其他元素为 inert
    newModalElement.addEventListener('shown.bs.modal', function () {
        // 将主要内容区域设置为 inert
        document.querySelector('.container').setAttribute('inert', '');

        // 聚焦到模态框的第一个可聚焦元素
        const focusableElements = newModalElement.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
        if (focusableElements.length > 0) {
            focusableElements[0].focus();
        }
    });

    // 显示模态框
    const modal = new bootstrap.Modal(newModalElement);
    modal.show();
}

// 显示自定义输入弹框
function showInputDialog(prompt, title, callback) {
    // 关闭所有已打开的模态框
    const openModals = document.querySelectorAll('.modal.show');
    openModals.forEach(modalEl => {
        const modalInstance = bootstrap.Modal.getInstance(modalEl);
        if (modalInstance) {
            modalInstance.hide();
        }
    });

    const modalElement = document.getElementById('customInputModal');

    // 创建一个新的模态框元素来替换旧的，以清除所有事件监听器
    const newModalElement = modalElement.cloneNode(true);
    modalElement.parentNode.replaceChild(newModalElement, modalElement);

    const confirmBtn = newModalElement.querySelector('#customInputConfirm');
    const inputField = newModalElement.querySelector('#customInputField');

    newModalElement.querySelector('#customInputTitle').textContent = title;
    newModalElement.querySelector('#customInputPrompt').textContent = prompt;
    inputField.value = '';

    // 添加错误提示元素
    let errorElement = newModalElement.querySelector('#customInputError');
    if (!errorElement) {
        errorElement = document.createElement('div');
        errorElement.id = 'customInputError';
        errorElement.className = 'text-danger mt-2';
        errorElement.style.display = 'none';
        inputField.parentNode.appendChild(errorElement);
    } else {
        // 初始时隐藏错误提示
        errorElement.style.display = 'none';
    }

    // 添加新的事件监听器
    function handleConfirm() {
        const value = inputField.value.trim();
        if (value) {
            // 隐藏错误提示
            errorElement.style.display = 'none';

            // 如果是手机号输入，验证格式
            if (title === "欢迎使用" && value.length !== 11) {
                errorElement.textContent = "请输入正确的11位手机号";
                errorElement.style.display = 'block';
                return;
            }

            callback(value);
            modal.hide();
        } else {
            // 显示错误提示在输入框下方
            errorElement.textContent = "请输入有效内容";
            errorElement.style.display = 'block';
        }
    }

    confirmBtn.addEventListener('click', handleConfirm);

    // 监听回车键
    function handleKeydown(event) {
        if (event.key === 'Enter') {
            handleConfirm();
        }
    }

    inputField.addEventListener('keydown', handleKeydown);

    // 输入时隐藏错误提示
    inputField.addEventListener('input', function () {
        errorElement.style.display = 'none';
    });

    // 监听模态框关闭事件，清理事件监听器和遮罩层
    function onModalHidden() {
        // 清理事件监听器
        confirmBtn.removeEventListener('click', handleConfirm);
        inputField.removeEventListener('keydown', handleKeydown);

        // 确保模态框背景被移除
        const modalBackdrops = document.querySelectorAll('.modal-backdrop');
        modalBackdrops.forEach(backdrop => {
            backdrop.parentNode.removeChild(backdrop);
        });

        // 移除body上的modal-open类
        document.body.classList.remove('modal-open');
        document.body.style.overflow = '';
        document.body.style.paddingRight = '';

        // 移除事件监听器
        newModalElement.removeEventListener('hidden.bs.modal', onModalHidden);
    }

    newModalElement.addEventListener('hidden.bs.modal', onModalHidden);

    // 显示模态框
    const modal = new bootstrap.Modal(newModalElement);
    modal.show();

    // 在模态框显示后聚焦输入框
    newModalElement.addEventListener('shown.bs.modal', function onShown() {
        inputField.focus();
        newModalElement.removeEventListener('shown.bs.modal', onShown);
    });
}

// 显示手机号输入弹框
function showPhoneInputDialog() {
    showInputDialog(
        "请输入您的手机号，如果服务器上有您的数据，输入正确的手机号后将自动同步下来。如果不输入手机号，将使用默认配置。",
        "欢迎使用",
        async (phone) => {
            try {
                // 如果用户没有输入手机号，使用默认配置
                if (!phone || phone.trim() === '') {
                    addLog("用户未输入手机号，将使用默认配置");
                    return;
                }

                // 验证手机号格式
                if (phone.length !== 11 || !/^1\d{10}$/.test(phone)) {
                    addLog(`输入的手机号 ${phone} 格式不正确`);
                    showError("请输入正确的11位手机号");
                    setTimeout(showPhoneInputDialog, 1000);
                    return;
                }

                addLog(`输入的手机号: ${phone}，正在处理...`);

                // 更新基本设置页面中的手机号输入框
                document.getElementById('phone').value = phone;

                // 尝试从服务器获取数据
                showAlert("正在从服务器获取数据，请稍候...");

                try {
                    // 获取岗位列表
                    addLog(`正在从服务器获取手机号 ${phone} 的岗位列表...`);
                    const jobs = await eel.fetch_job_list(phone)();

                    // 检查是否成功获取到数据
                    if (jobs && Array.isArray(jobs) && jobs.length > 0) {
                        addLog(`成功获取到 ${jobs.length} 个岗位，正在更新UI...`);

                        // 现在可以安全地保存手机号，因为我们已经确认服务器有数据
                        await saveUsername(phone);

                        // 重新加载配置，确保使用最新的服务器配置
                        addLog("重新加载配置以应用服务器数据...");
                        const updatedConfig = await eel.get_config()();

                        // 更新全局配置
                        config_data = updatedConfig;
                        addLog("已更新全局配置");

                        // 更新版本信息
                        await loadVersionInfo();

                        // 更新岗位列表UI
                        updateJobList(jobs);

                        addLog(`岗位列表UI已更新`);
                        showAlert(`成功获取到 ${jobs.length} 个岗位，页面已更新`);
                    } else {
                        addLog("服务器未找到岗位数据，将使用默认配置");

                        // 现在可以保存手机号，因为我们需要创建新的配置
                        await saveUsername(phone);

                        // 使用默认配置
                        jobsData = ['成都销售'];
                        keywordsData = {
                            '成都销售': {
                                include: ['电话销售', '在线销售', '电销'],
                                exclude: ['保险销售'],
                                relation: 'OR',
                                description: ''
                            }
                        };
                        // 保存默认配置到服务器
                        await saveJobsAndKeywordsToConfig();
                        // 更新UI
                        renderJobList();
                        showAlert("未找到岗位数据，已使用默认配置。您可以根据需要修改岗位和关键词。");
                    }
                } catch (error) {
                    console.error("获取岗位列表时出错:", error);
                    addLog(`获取岗位列表时出错: ${error.toString()}`);
                    showError(`获取岗位列表时出错: ${error.toString()}`);
                }
            } catch (error) {
                console.error("处理手机号时出错:", error);
                addLog(`处理手机号时出错: ${error.toString()}`);
                showError(`处理手机号时出错: ${error.toString()}`);

                // 如果出错，再次显示输入弹框
                setTimeout(showPhoneInputDialog, 1000);
            }
        }
    );
}

// 注册Python回调函数，用于接收日志
eel.expose(addLogFromPython);
function addLogFromPython(message) {
    // 因为Python日志已经包含时间戳，所以直接添加到日志容器
    const logContainer = document.getElementById('log-container');
    const logEntry = document.createElement('div');
    logEntry.className = 'log-entry';

    // 判断是否为错误日志
    const isError = /错误|失败|出错|异常|error|failed|exception/i.test(message);

    if (isError) {
        logEntry.classList.add('log-error');
    }

    logEntry.textContent = message;
    logContainer.appendChild(logEntry);

    // 滚动到底部
    logContainer.scrollTop = logContainer.scrollHeight;
}

// 注册Python回调函数，用于更新自动化状态
eel.expose(updateAutomationStatus);
function updateAutomationStatus(running) {
    isRunning = running;
    updateButtonState();
}

// 注册Python回调函数，用于更新版本信息
eel.expose(updateVersionInfoFromPython);
function updateVersionInfoFromPython(info) {
    versionInfo = info;
    updateVersionInfoDisplay(info);
}

// 保存延迟时间设置
async function saveDelaySettings() {
    try {
        const minDelay = parseInt(document.getElementById('min-delay').value);
        const maxDelay = parseInt(document.getElementById('max-delay').value);

        // 使用save_config_item保存
        await eel.save_config_item('minDelay', minDelay, '最小延迟时间')();
        await eel.save_config_item('maxDelay', maxDelay, '最大延迟时间')();

        addLog(`已保存延迟时间设置: 最小=${minDelay}秒, 最大=${maxDelay}秒`);
    } catch (error) {
        console.error('保存延迟时间设置出错:', error);
        showError('保存延迟时间设置失败，请重试');
    }
}

function updateVersionUI(version) {
    const isEnterprise = version === 'enterprise';
    const jobDescriptionContainer = document.getElementById('job-description-container');
    const keywordSettingsContainer = document.getElementById('keyword-settings-container');
    const keywordButtons = document.querySelector('.keyword-buttons');
    const keywordRelation = document.getElementById('keyword-relation');

    // 记录日志
    addLog(`正在更新UI显示，当前版本: ${version}`);

    if (jobDescriptionContainer) {
        jobDescriptionContainer.style.display = isEnterprise ? 'block' : 'none';
    } else {
        console.error('找不到job-description-container元素');
    }

    if (keywordSettingsContainer) {
        keywordSettingsContainer.style.display = isEnterprise ? 'none' : 'block';
    } else {
        console.error('找不到keyword-settings-container元素');
    }

    if (keywordButtons) {
        keywordButtons.style.display = isEnterprise ? 'none' : 'flex';
    }

    if (keywordRelation) {
        keywordRelation.style.display = isEnterprise ? 'none' : 'block';
    }

    // 加载已保存的岗位描述
    if (isEnterprise && selectedJob) {
        addLog(`企业版模式：正在加载岗位"${selectedJob}"的描述`);
        loadJobDescription(selectedJob);
    }

    addLog(`UI显示更新完成，${isEnterprise ? '已显示岗位描述输入框' : '已显示关键词设置'}`);
}

// 加载岗位描述
async function loadJobDescription(jobName) {
    if (!jobName) return;

    const jobDescriptionTextarea = document.getElementById('job-description');
    if (!jobDescriptionTextarea) {
        console.error('找不到job-description元素');
        return;
    }

    try {
        // 获取最新的关键词数据
        const config = await eel.get_config()();
        const keywordsData = config.keywordsData || {};

        // 设置描述内容
        const description = keywordsData[jobName]?.description || '';
        jobDescriptionTextarea.value = description;

        addLog(description ? `已加载岗位"${jobName}"的描述` : `岗位"${jobName}"暂无描述`);
    } catch (error) {
        console.error('加载岗位描述失败:', error);
        addLog('加载岗位描述失败: ' + error.toString());
    }
}

// 修改版本切换事件处理
async function handleVersionChange(version) {
    try {
        await saveVersion(version);
        updateVersionUI(version);
        await loadVersionInfo();
    } catch (error) {
        console.error('切换版本失败:', error);
        showError('切换版本失败');
    }
}

function selectJob(jobName) {
    // 移除其他岗位的选中状态
    document.querySelectorAll('.job-item').forEach(item => {
        item.classList.remove('selected');
    });

    // 选中当前岗位
    const jobItem = document.querySelector(`.job-item[data-job="${jobName}"]`);
    if (jobItem) {
        jobItem.classList.add('selected');
    }

    // 更新选中的岗位
    selectedJob = jobName;

    // 启用关键词按钮
    document.getElementById('add-include-keyword-btn').disabled = false;
    document.getElementById('add-exclude-keyword-btn').disabled = false;

    // 显示关键词关系选择器
    document.getElementById('keyword-relation').style.display = 'block';

    // 加载关键词
    loadKeywords(jobName);

    // 加载岗位描述
    loadJobDescription(jobName);

    // 隐藏"无关键词"提示
    document.getElementById('no-keywords').style.display = 'none';
}

// 岗位描述自动保存
function bindJobDescriptionEvents() {
    const jobDescriptionTextarea = document.getElementById('job-description');
    if (!jobDescriptionTextarea) {
        console.error('找不到job-description元素');
        return;
    }

    // 失去焦点时保存岗位描述
    jobDescriptionTextarea.addEventListener('blur', async function () {
        if (!selectedJob) return;

        try {
            const description = this.value;
            addLog(`正在保存岗位"${selectedJob}"的描述...`);

            // 获取当前的关键词数据
            const config = await eel.get_config()();
            const currentData = config.keywordsData || {};

            // 更新描述
            if (!currentData[selectedJob]) {
                currentData[selectedJob] = {
                    include: [],
                    exclude: [],
                    relation: 'OR',
                    description: description
                };
            } else {
                currentData[selectedJob].description = description;
            }

            // 保存更新后的数据
            const saved = await eel.save_config_item('keywordsData', currentData, '岗位描述')();

            if (saved) {
                addLog(`已保存岗位"${selectedJob}"的描述`);
            } else {
                throw new Error('保存失败');
            }
        } catch (error) {
            console.error('保存岗位描述失败:', error);
            addLog('保存岗位描述失败: ' + error.toString());
            showError('保存岗位描述失败');
        }
    });
}

// 加载平台配置并渲染平台选择选项
async function loadPlatformConfigs() {
    try {
        // 获取平台配置
        const platformConfigs = await eel.get_platform_configs()();

        // 获取平台选择容器
        const platformOptionsContainer = document.querySelector('.platform-options');

        // 确保容器存在
        if (!platformOptionsContainer) {
            console.error('找不到平台选择容器');
            return;
        }

        // 清空现有的平台选项
        platformOptionsContainer.innerHTML = '';

        // 获取平台列表
        const platforms = platformConfigs.platforms || ["BOSS直聘", "智联招聘", "前程无忧", "猎聘"];

        // 为每个平台创建选项
        platforms.forEach((platform, index) => {
            const platformOption = document.createElement('div');
            platformOption.className = 'form-check form-check-inline mb-3';
            platformOption.style = "width: 120px;";
            platformOption.innerHTML = `
                <input class="form-check-input" type="radio" name="platform" id="platform${index + 1}"
                    value="${platform}">
                <div class="d-flex align-items-center mb-2">
                    <label class="form-check-label" for="platform${index + 1}">${platform}</label>
                </div>
            `;
            platformOptionsContainer.appendChild(platformOption);
        });

        // 从配置中获取已选择的平台
        const selectedPlatform = config_data.platform;
        if (selectedPlatform) {
            // 找到对应的radio按钮并选中
            const radioBtn = document.querySelector(`input[name="platform"][value="${selectedPlatform}"]`);
            if (radioBtn) {
                radioBtn.checked = true;
                addLog(`已从本地缓存加载招聘平台: ${selectedPlatform}`);
            }
        }

        // 重新绑定平台选择事件
        document.querySelectorAll('input[name="platform"]').forEach(radio => {
            radio.addEventListener('change', function () {
                savePlatform(this.value);
            });
        });

        addLog('平台配置加载完成');
    } catch (error) {
        console.error('加载平台配置失败:', error);
        addLog(`加载平台配置失败: ${error.message}`);
    }
}

// 根据平台名称返回对应的描述
function getPlatformDescription(platform) {
    const descriptions = {
        "BOSS直聘": "招聘效率高，推荐使用",
        "智联招聘": "招聘人才多样化",
        "前程无忧": "招聘资源丰富",
        "猎聘": "适合高端人才招聘"
    };

    return descriptions[platform] || "招聘平台";
}
