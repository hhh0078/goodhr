let isRunning = false;
let keywords = [];
let excludeKeywords = [];
let isAndMode = false;
let matchCount = 0;
let matchLimit = 200;
let enableSound = true;

// 添加职位相关状态
let positions = [];
let currentPosition = null;

// 添加下载简历相关状态
let isDownloading = false;
let downloadCount = 0;

// 添加新的状态变量
let scrollDelayMin = 3;  // 默认最小延迟秒数
let scrollDelayMax = 5;  // 默认最大延迟秒数

// 添加手机号相关变量
let boundPhone = '';
const API_BASE = 'https://goodhr.58it.cn';

// 添加日志持久化相关的函数
async function saveLogs(logs) {
	try {
		await chrome.storage.local.set({ 'hr_assistant_logs': logs });
	} catch (error) {
		console.error('保存日志失败:', error);
	}
}

async function loadLogs() {
	try {
		const result = await chrome.storage.local.get('hr_assistant_logs');
		return result.hr_assistant_logs || [];
	} catch (error) {
		console.error('加载日志失败:', error);
		return [];
	}
}

// 添加错误提示函数
function showError(error) {
	addLog(`错误: ${error.message}`, 'error');
	console.error('详细错误:', error);
}

// 添加自动保存设置函数
async function saveSettings() {
	try {
		// 获取当前设置
		const currentSettings = {
			positions,
			currentPosition: currentPosition?.name || '',
			isAndMode,
			matchLimit: parseInt(document.getElementById('match-limit')?.value) || 200,
			enableSound,
			scrollDelayMin: parseInt(document.getElementById('delay-min')?.value) || 3,
			scrollDelayMax: parseInt(document.getElementById('delay-max')?.value) || 5,
			clickFrequency: parseInt(document.getElementById('click-frequency')?.value) || 7
		};

		// 保存到本地存储
		await chrome.storage.local.set(currentSettings);

		// 如果绑定了手机号，直接同步到服务器
		if (boundPhone) {
			try {
				const response = await fetch(`${API_BASE}/updatejson.php?phone=${boundPhone}`, {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json'
					},
					body: JSON.stringify(currentSettings)
				});

				if (!response.ok) {
					throw new Error('服务器同步失败');
				}

				addLog('设置已更新并同步到服务器', 'success');
			} catch (error) {
				addLog('同步到服务器失败: ' + error.message, 'error');
				throw error;
			}
		} else {
			addLog('设置已保存到本地', 'success');
		}

		// 通知 content script 设置已更新
		chrome.tabs.query({
			active: true,
			currentWindow: true
		}, function (tabs) {
			if (tabs[0]) {
				chrome.tabs.sendMessage(tabs[0].id, {
					type: 'SETTINGS_UPDATED',
					data: {
						...currentSettings,
						keywords: currentPosition?.keywords || [],
						excludeKeywords: currentPosition?.excludeKeywords || []
					}
				});
			}
		});

	} catch (error) {
		showError(error);
	}
}

// 定义基础的关键词函数
function addKeywordBase() {
	const input = document.getElementById('keyword-input');
	if (!input) {
		console.error('找不到关键词输入框元素');
		addLog('⚠️ 系统错误：找不到关键词输入框', 'error');
		return;
	}

	const keyword = input.value.trim();
	if (keyword && !keywords.includes(keyword)) {
		keywords.push(keyword);
		renderKeywords();
		input.value = '';
	}
}

function removeKeyword(keyword) {
	if (!currentPosition) return;

	currentPosition.keywords = currentPosition.keywords.filter(k => k !== keyword);
	keywords = [...currentPosition.keywords];
	renderKeywords();
	saveSettings();

	// 实时通知 content script 关键词更新
	if (isRunning) {
		notifyKeywordsUpdate();
	}
}

// 包装函数，添加自动保存功能
function addKeyword() {
	if (!currentPosition) {
		addLog('⚠️ 请先选择岗位', 'error');
		return;
	}

	const input = document.getElementById('keyword-input');
	if (!input) {
		console.error('找不到关键词输入框元素');
		addLog('⚠️ 系统错误：找不到关键词输入框', 'error');
		return;
	}

	const keyword = input.value.trim();
	if (keyword && !currentPosition.keywords.includes(keyword)) {
		currentPosition.keywords.push(keyword);
		keywords = [...currentPosition.keywords];
		renderKeywords();
		input.value = '';
		saveSettings();

		// 实时通知 content script 关键词更新
		if (isRunning) {
			notifyKeywordsUpdate();
		}
	}
}

// 添加排除关键词的函数
function addExcludeKeyword() {
	if (!currentPosition) {
		addLog('⚠️ 请先选择岗位', 'error');
		return;
	}

	const input = document.getElementById('keyword-input');
	if (!input) {
		console.error('找不到关键词输入框元素');
		addLog('⚠️ 系统错误：找不到关键词输入框', 'error');
		return;
	}

	const keyword = input.value.trim();
	if (keyword && !currentPosition.excludeKeywords.includes(keyword)) {
		currentPosition.excludeKeywords.push(keyword);
		excludeKeywords = [...currentPosition.excludeKeywords];
		renderExcludeKeywords();
		input.value = '';
		saveSettings();

		// 实时通知 content script 关键词更新
		if (isRunning) {
			notifyKeywordsUpdate();
		}
	}
}

// 删除排除关键词的函数
function removeExcludeKeyword(keyword) {
	if (!currentPosition) return;

	currentPosition.excludeKeywords = currentPosition.excludeKeywords.filter(k => k !== keyword);
	excludeKeywords = [...currentPosition.excludeKeywords];
	renderExcludeKeywords();
	saveSettings();

	// 实时通知 content script 关键词更新
	if (isRunning) {
		notifyKeywordsUpdate();
	}
}

// 渲染排除关键词列表
function renderExcludeKeywords() {
	const container = document.getElementById('exclude-keyword-list');
	if (!container) {
		throw new Error('找不到排除关键词列表容器');
	}

	container.innerHTML = '';

	excludeKeywords.forEach(keyword => {
		const keywordDiv = document.createElement('div');
		keywordDiv.className = 'keyword-tag';
		keywordDiv.style.backgroundColor = '#ffe0e0'; // 使用红色背景区分
		keywordDiv.style.borderColor = '#ff4444';
		keywordDiv.style.color = '#ff4444';
		keywordDiv.innerHTML = `
            ${keyword}
            <button class="remove-keyword" data-keyword="${keyword}" style="color: #ff4444;">&times;</button>
        `;

		const removeButton = keywordDiv.querySelector('.remove-keyword');
		removeButton.addEventListener('click', () => {
			removeExcludeKeyword(keyword);
		});

		container.appendChild(keywordDiv);
	});
}

// 在文件开头添加状态持久化相关函数
async function saveState() {
	await chrome.storage.local.set({
		isRunning,
		isDownloading,
		matchCount,
		downloadCount
	});
}

async function loadState() {
	try {
		const state = await new Promise((resolve) => {
			chrome.storage.local.get({  // 添加默认值对象
				isRunning: false,
				isDownloading: false,
				matchCount: 0,
				downloadCount: 0
			}, (result) => {
				resolve(result);
			});
		});

		isRunning = state.isRunning;
		isDownloading = state.isDownloading;
		matchCount = state.matchCount;
		downloadCount = state.downloadCount;

		// 更新UI以反映当前状态
		updateUI();

		// 如果有正在进行的操作，显示相应的状态
		if (isRunning) {
			addLog(`继续运行中，已匹配 ${matchCount} 个候选人`, 'info');
		}
		if (isDownloading) {
			addLog(`继续下载中，已下载 ${downloadCount} 份简历`, 'info');
		}
	} catch (error) {
		console.error('加载状态失败:', error);
		// 使用默认值
		isRunning = false;
		isDownloading = false;
		matchCount = 0;
		downloadCount = 0;
	}
}

// 将所有按钮事件监听器移到 DOMContentLoaded 事件处理函数中
document.addEventListener('DOMContentLoaded', async () => {
	try {
		// 设置版本号
		const version = chrome.runtime.getManifest().version;
		document.getElementById('version').textContent = version;

		// 加载已绑定的手机号
		const stored = await chrome.storage.local.get('hr_assistant_phone');
		if (stored.hr_assistant_phone) {
			boundPhone = stored.hr_assistant_phone;
			document.getElementById('phone-input').value = boundPhone;
			// 尝试从服务器同步配置
			await syncSettingsFromServer();
		}

		// 绑定手机号按钮事件
		const phoneInput = document.getElementById('phone-input');
		const bindPhoneBtn = document.getElementById('bind-phone');

		bindPhoneBtn.addEventListener('click', async () => {
			try {
				await bindPhone(phoneInput.value.trim());
			} catch (error) {
				console.error('绑定手机号失败:', error);
			}
		});

		// 手机号输入框回车事件
		phoneInput.addEventListener('keydown', async (e) => {
			if (e.key === 'Enter') {
				e.preventDefault();
				try {
					await bindPhone(phoneInput.value.trim());
				} catch (error) {
					console.error('绑定手机号失败:', error);
				}
			}
		});

		// 加载并显示历史日志
		const logs = await loadLogs();
		const logContainer = document.getElementById('log-container');
		logContainer.innerHTML = ''; // 清空默认的系统就绪消息

		logs.forEach(log => {
			const logEntry = document.createElement('div');
			logEntry.className = 'log-entry';
			logEntry.style.display = 'flex';
			logEntry.innerHTML = log.html;
			logContainer.appendChild(logEntry);
		});

		// 如果没有历史日志，显示系统就绪消息
		if (logs.length === 0) {
			const logEntry = document.createElement('div');
			logEntry.className = 'log-entry';
			logEntry.style.display = 'flex';
			logEntry.innerHTML = `
				<span style="color: #666; margin-right: 8px;">></span>
				<span>系统就绪，等待开始...</span>
			`;
			logContainer.appendChild(logEntry);
		}

		await loadState();  // 加载保存的状态

		// 加载设置
		const settings = await getSettings();
		const matchLimitInput = document.getElementById('match-limit');
		const enableSoundCheckbox = document.getElementById('enable-sound');

		// 监听年龄选择变更
		document.getElementById('ageMin')?.addEventListener('change', saveSettings);
		document.getElementById('ageMax')?.addEventListener('change', saveSettings);

		// 监听学历选择变更
		document.querySelectorAll('input[id^="edu-"]').forEach(checkbox => {
			checkbox.addEventListener('change', saveSettings);
		});

		// 监听性别选择变更
		document.querySelectorAll('input[id^="gender-"]').forEach(checkbox => {
			checkbox.addEventListener('change', saveSettings);
		});

		// 绑定关键词相关事件
		const keywordInput = document.getElementById('keyword-input');
		const addKeywordBtn = document.getElementById('add-keyword');
		const addExcludeKeywordBtn = document.getElementById('add-exclude-keyword');
		const positionInput = document.getElementById('position-input');
		const addPositionBtn = document.getElementById('add-position');

		if (!keywordInput || !addKeywordBtn || !addExcludeKeywordBtn || !positionInput || !addPositionBtn) {
			console.error('找不到关键词或岗位相关元素');
			addLog('⚠️ 系统错误：界面初始化失败', 'error');
			return;
		}

		// 关键词输入框回车事件
		keywordInput.addEventListener('keydown', (e) => {
			if (e.key === 'Enter') {
				e.preventDefault(); // 阻止默认行为
				addKeyword();
			}
		});

		// 岗位输入框回车事件
		positionInput.addEventListener('keydown', (e) => {
			if (e.key === 'Enter') {
				e.preventDefault(); // 阻止默认行为
				addPosition();
			}
		});

		// 按钮点击事件
		addKeywordBtn.addEventListener('click', () => addKeyword());
		addExcludeKeywordBtn.addEventListener('click', () => addExcludeKeyword());
		addPositionBtn.addEventListener('click', () => addPosition());

		// 加载与/或模式设置
		const andModeCheckbox = document.getElementById('keywords-and-mode');
		if (settings.isAndMode !== undefined) {
			isAndMode = settings.isAndMode;
			andModeCheckbox.checked = isAndMode;
		}

		// 监听与/或模式变化
		andModeCheckbox.addEventListener('change', (e) => {
			isAndMode = e.target.checked;
			saveSettings();
			addLog(`关键词匹配模式: ${isAndMode ? '全部匹配' : '任一匹配'}`, 'info');
		});

		// 设置关键词
		if (settings.keywords && settings.keywords.length > 0) {
			keywords = settings.keywords;
			renderKeywords();
			addLog(`已加载 ${keywords.length} 个关键词`, 'info');
		}

		// 设置排除关键词
		if (settings.excludeKeywords && settings.excludeKeywords.length > 0) {
			excludeKeywords = settings.excludeKeywords;
			renderExcludeKeywords();
			addLog(`已加载 ${excludeKeywords.length} 个排除关键词`, 'info');
		}

		// 加载匹配限制和声音设置
		if (settings.matchLimit !== undefined) {
			matchLimit = settings.matchLimit;
			matchLimitInput.value = matchLimit;
		}

		if (settings.enableSound !== undefined) {
			enableSound = settings.enableSound;
			enableSoundCheckbox.checked = enableSound;
		}

		// 监听设置变化
		matchLimitInput.addEventListener('change', () => {
			matchLimit = parseInt(matchLimitInput.value) || 10;
			saveSettings();
			addLog(`设置匹配暂停数量: ${matchLimit}`, 'info');
		});

		enableSoundCheckbox.addEventListener('change', (e) => {
			enableSound = e.target.checked;
			saveSettings();
			addLog(`${enableSound ? '启用' : '禁用'}提示音`, 'info');
		});

		// 加载职位数据
		if (settings.positions) {
			positions = settings.positions;
			renderPositions();

			if (settings.currentPosition) {
				selectPosition(settings.currentPosition);
			}
		}

		// 绑定职位相关事件
		document.getElementById('position-input')?.addEventListener('keypress', (e) => {
			if (e.key === 'Enter') {
				addPosition();
			}
		});

		document.getElementById('add-position')?.addEventListener('click', addPosition);

		// 绑定打招呼和下载按钮事件
		document.getElementById('scrollButton')?.addEventListener('click', () => {
			startAutoScroll();  // 开始打招呼
		});

		document.getElementById('downloadButton')?.addEventListener('click', () => {
			alert("出于安全考虑,该功能已禁止使用")
			return
			startDownload();    // 开始下载
		});

		// 绑定停止按钮事件
		document.getElementById('stopButton')?.addEventListener('click', () => {
			if (isRunning) {
				stopAutoScroll();  // 停止打招呼
			}
			if (isDownloading) {
				stopDownload();    // 停止下载简历
			}
		});

		// 加载完成提示
		addLog('设置加载完成', 'success');

		// 加载延迟设置
		const delayMinInput = document.getElementById('delay-min');
		const delayMaxInput = document.getElementById('delay-max');

		if (settings.scrollDelayMin !== undefined) {
			scrollDelayMin = settings.scrollDelayMin;
			delayMinInput.value = scrollDelayMin;
		} else {
			delayMinInput.value = 3; // 设置默认值
		}

		if (settings.scrollDelayMax !== undefined) {
			scrollDelayMax = settings.scrollDelayMax;
			delayMaxInput.value = scrollDelayMax;
		} else {
			delayMaxInput.value = 5; // 设置默认值
		}

		// 监听延迟输入框变化
		delayMinInput.addEventListener('change', saveSettings);
		delayMaxInput.addEventListener('change', saveSettings);

		// 加载点击频率设置
		const clickFrequencyInput = document.getElementById('click-frequency');
		if (settings.clickFrequency !== undefined) {
			clickFrequencyInput.value = settings.clickFrequency;
		}

		// 监听点击频率变化
		clickFrequencyInput?.addEventListener('change', saveSettings);

		// 获取并显示排行榜数据
		await loadRankingData();
	} catch (error) {
		showError(error);
	}
});

// 修改 getSettings 函数
async function getSettings() {
	return new Promise((resolve, reject) => {
		chrome.storage.local.get([
			'positions',
			'currentPosition',
			'isAndMode',
			'matchLimit',
			'enableSound',
			'scrollDelayMin',
			'scrollDelayMax',
			'clickFrequency'
		], (result) => {
			if (chrome.runtime.lastError) {
				reject(chrome.runtime.lastError);
				return;
			}
			resolve(result);
		});
	});
}

// 修改 startAutoScroll 函数
async function startAutoScroll() {
	if (!currentPosition) {
		addLog('⚠️ 请先选择岗位', 'error');
		isRunning = false;
		updateUI();
		return;
	}

	// 获取打招呼暂停数
	const matchLimitInput = document.getElementById('match-limit');
	matchLimit = parseInt(matchLimitInput.value) || 200; // 默认值为200

	// 检查是否有关键词
	if (!currentPosition.keywords.length && !currentPosition.excludeKeywords.length) {
		if (!confirm('当前岗位没有设置任何关键词，将会给所有候选人打招呼，是否继续？')) {
			return;
		}
		addLog('⚠️ 无关键词，将给所有候选人打招呼', 'warning');
	}

	if (isRunning) return;

	try {
		isRunning = true;
		matchCount = 0;
		updateUI();
		addLog('开始运行自动滚动...', 'info');
		addLog(`设置打招呼暂停数: ${matchLimit}`, 'info');
		addLog(`随机延迟时间: ${scrollDelayMin}-${scrollDelayMax}秒`, 'info');

		chrome.tabs.query({
			active: true,
			currentWindow: true
		}, tabs => {
			if (tabs[0]) {
				chrome.tabs.sendMessage(
					tabs[0].id, {
					action: 'START_SCROLL',
					data: {
						keywords: currentPosition.keywords,
						excludeKeywords: currentPosition.excludeKeywords,
						isAndMode: isAndMode,
						matchLimit: matchLimit,
						scrollDelayMin: scrollDelayMin,
						scrollDelayMax: scrollDelayMax,
						clickFrequency: parseInt(document.getElementById('click-frequency')?.value) || 7,
					}
				},
					response => {
						if (chrome.runtime.lastError) {
							console.error('发送消息失败:', chrome.runtime.lastError);
							addLog('⚠️ 无法连接到页面，请刷新页面', 'error');
							isRunning = false;
							updateUI();
							return;
						}
						console.log('收到响应:', response);
					}
				);
			}
		});

		await saveState();
	} catch (error) {
		console.error('启动失败:', error);
		isRunning = false;
		updateUI();
		addLog('启动失败: ' + error.message, 'error');
	}
}

// 停止自动滚动
async function stopAutoScroll() {
	if (!isRunning) return;

	try {
		isRunning = false;
		updateUI();
		addLog(`停止自动滚动，当前已匹配 ${matchCount} 个候选人`, 'warning');

		chrome.tabs.query({
			active: true,
			currentWindow: true
		}, tabs => {
			if (tabs[0]) {
				chrome.tabs.sendMessage(tabs[0].id, {
					action: 'STOP_SCROLL'
				}, response => {
					if (chrome.runtime.lastError) {
						console.error('发送停止消息失败:', chrome.runtime.lastError);
						return;
					}
					console.log('停止响应:', response);
				});
			}
		});

		await saveState();  // 保存状态
	} catch (error) {
		console.error('停止失败:', error);
		addLog('停止失败: ' + error.message, 'error');
	} finally {
		// 确保状态被重置
		matchCount = 0;
		isRunning = false;
		updateUI();
	}
}

// 更新UI状态
function updateUI() {
	const initialButtons = document.getElementById('initialButtons');
	const stopButtons = document.getElementById('stopButtons');

	// 如果正在运行任何操作，显示停止按钮
	if (isRunning || isDownloading) {
		initialButtons.classList.add('hidden');
		stopButtons.classList.remove('hidden');
	} else {
		initialButtons.classList.remove('hidden');
		stopButtons.classList.add('hidden');
	}
}

function renderKeywords() {
	const container = document.getElementById('keyword-list');
	if (!container) {
		throw new Error('找不到关键词列表容器');
	}

	// 移除旧的事件监听器
	container.innerHTML = '';

	// 为每个关键词创建元素
	keywords.forEach(keyword => {
		const keywordDiv = document.createElement('div');
		keywordDiv.className = 'keyword-tag';
		keywordDiv.innerHTML = `
            ${keyword}
            <button class="remove-keyword" data-keyword="${keyword}">&times;</button>
        `;

		// 为删除按钮添加事件监听器
		const removeButton = keywordDiv.querySelector('.remove-keyword');
		removeButton.addEventListener('click', () => {
			removeKeyword(keyword);
		});

		container.appendChild(keywordDiv);
	});
}

// 修改添加日志的函数
async function addLog(message, type = 'info') {
	const logContainer = document.getElementById('log-container');
	const logEntry = document.createElement('div');
	logEntry.className = 'log-entry';
	logEntry.style.display = 'flex';

	const timestamp = new Date().toLocaleTimeString('zh-CN', {
		hour12: false,
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});

	let color = '#00ff00'; // 默认绿色
	let prefix = '>';

	switch (type) {
		case 'error':
			color = '#ff4444';
			prefix = '!';
			break;
		case 'warning':
			color = '#ffaa00';
			prefix = '?';
			break;
		case 'success':
			color = '#00ff00';
			prefix = '√';
			break;
		case 'info':
			color = '#00ff00';
			prefix = '>';
			break;
	}

	const logHtml = `
        <span style="color: #666;font-size: 10px; margin-right: 8px;">${prefix}</span>
        <span style="color: ${color};font-size: 10px;">[${timestamp}] ${message}</span>
    `;

	logEntry.innerHTML = logHtml;
	logContainer.appendChild(logEntry);

	// 自动滚动到底部
	const parentContainer = logContainer.parentElement;
	parentContainer.scrollTop = parentContainer.scrollHeight;

	// 保存日志到存储
	try {
		const logs = await loadLogs();
		logs.push({
			message,
			type,
			timestamp,
			html: logHtml
		});

		// 只保留最近的100条日志
		if (logs.length > 100) {
			logs.splice(0, logs.length - 100);
		}

		await saveLogs(logs);
	} catch (error) {
		console.error('保存日志失败:', error);
	}
}

// 发送消息
chrome.runtime.sendMessage({ message: "hello" }, function (response) {
	console.log("收到来自接收者的回复：", response);
});

// 修改 chrome.runtime.onMessage 监听器
chrome.runtime.onMessage.addListener(async (message, sender, sendResponse) => {
	console.log('插件收到页面收到消息:', message.data.message);

	if (message.type === 'MATCH_SUCCESS') {
		const {
			name,
			age,
			education,
			university,
			extraInfo,
			clicked
		} = message.data;
		matchCount++;
		let logText = ` [${matchCount}] ${name} `;

		if (extraInfo && extraInfo.length > 0) {
			const extraInfoText = extraInfo
				.map(info => `${info.type}: ${info.value}`)
				.join(' | ');
			logText += ` | ${extraInfoText}`;
		}

		if (clicked) {
			logText += ' [已点击]';
		}

		addLog(logText, 'success');

		// 播放提示音
		if (enableSound) {
			playNotificationSound();
		}

		// 检查是否达到匹配限制
		if (matchCount >= matchLimit) {
			stopAutoScroll();
			addLog(`已达到设定的打招呼数量 ${matchLimit}，自动停止`, 'warning');
			// 播放特殊的完成提示音
			if (enableSound) {
				playNotificationSound();
				// 连续播放两次以示区分
				setTimeout(() => playNotificationSound(), 500);
			}
		}

		await saveState();
	} else if (message.type === 'SCROLL_COMPLETE') {
		isRunning = false;
		await saveState();
		updateUI();
		addLog(`滚动完成，共匹配 ${matchCount} 个候选人`, 'success');
		matchCount = 0;
	} else if (message.type === 'LOG_MESSAGE') {
		// 处理日志消息
		addLog(message.data.message, message.data.type);
	} else if (message.type === 'ERROR') {
		addLog(message.error, 'error');
	}
});

// 添加提示音函数
function playNotificationSound() {
	const audio = new Audio(chrome.runtime.getURL('sounds/notification.mp3'));
	audio.volume = 0.5; // 设置音量
	audio.play().catch(error => console.error('播放提示音失败:', error));
}

const VERSION_API = `${API_BASE}/ai-v.json?t=${Date.now()}`;
const CURRENT_VERSION = chrome.runtime.getManifest().version; // 从 manifest.json 获取版本号
let NEW_VERSION = "0";
let GONGGAO = null;

async function checkVersion() {
	try {
		const response = await fetch(`${VERSION_API}&_=${Date.now()}`);
		const data = await response.json();
		NEW_VERSION = data.version;
		GONGGAO = data.gonggao;
		return {
			needUpdate: data.version !== CURRENT_VERSION,
			releaseNotes: data.releaseNotes
		};
	} catch (error) {
		console.error('版本检查失败:', error);
		return {
			needUpdate: false
		};
	}
}

// 检查版本更新
async function checkForUpdates() {
	const result = await checkVersion();

	if (result.needUpdate) {
		alert(`发现新版本！\n\n更新说明：\n${result.releaseNotes || '暂无更新说明'}\n\n点击确定前往更新` + CURRENT_VERSION + "->" + NEW_VERSION);
		if (result.releaseNotes.includes('必须更新')) {
			chrome.tabs.create({ url: API_BASE });
		}

	}
	if (GONGGAO) {
		alert(GONGGAO);
	}
}
checkForUpdates();

// 添加职位相关函数
function addPosition() {
	const input = document.getElementById('position-input');
	const positionName = input.value.trim();

	if (positionName && !positions.find(p => p.name === positionName)) {
		const newPosition = {
			name: positionName,
			keywords: [],
			excludeKeywords: []
		};

		positions.push(newPosition);
		renderPositions();
		input.value = '';
		saveSettings();
		selectPosition(positionName);
	}
}

function removePosition(positionName) {
	if (confirm(`确定要删除职位"${positionName}"吗？\n删除后该职位的所有关键词都将被删除。`)) {
		positions = positions.filter(p => p.name !== positionName);
		if (currentPosition?.name === positionName) {
			currentPosition = null;
		}
		renderPositions();
		renderKeywords();
		renderExcludeKeywords();
		saveSettings();
	}
}

function selectPosition(positionName) {
	currentPosition = positions.find(p => p.name === positionName);

	// 更新关键词显示
	keywords = currentPosition ? [...currentPosition.keywords] : [];
	excludeKeywords = currentPosition ? [...currentPosition.excludeKeywords] : [];

	renderKeywords();
	renderExcludeKeywords();
	renderPositions();
}

function renderPositions() {
	const container = document.getElementById('position-list');
	container.innerHTML = '';

	positions.forEach(position => {
		const positionDiv = document.createElement('div');
		positionDiv.className = `position-tag ${currentPosition?.name === position.name ? 'active' : ''}`;
		positionDiv.innerHTML = `
            ${position.name}
            <button class="remove-btn" data-position="${position.name}">&times;</button>
        `;

		positionDiv.querySelector('button').addEventListener('click', (e) => {
			e.stopPropagation();
			removePosition(position.name);
		});

		positionDiv.addEventListener('click', () => {
			selectPosition(position.name);
		});

		container.appendChild(positionDiv);
	});

	// 如果没有职位,显示提示文本
	if (positions.length === 0) {
		const emptyTip = document.createElement('div');
		emptyTip.style.cssText = 'color: #999; font-size: 12px; padding: 4px;';
		emptyTip.textContent = '请添加职位...';
		container.appendChild(emptyTip);
	}
}

// 添加通知关键词更新的函数
function notifyKeywordsUpdate() {
	chrome.tabs.query({
		active: true,
		currentWindow: true
	}, tabs => {
		if (tabs[0]) {
			chrome.tabs.sendMessage(tabs[0].id, {
				action: 'UPDATE_KEYWORDS',
				data: {
					keywords: currentPosition.keywords,
					excludeKeywords: currentPosition.excludeKeywords,
					isAndMode: isAndMode
				}
			});
		}
	});
}

// 开始下载简历
async function startDownload() {
	if (isDownloading) return;

	try {
		isDownloading = true;
		downloadCount = 0;
		updateUI();
		addLog('开始下载简历...', 'info');

		chrome.tabs.query({
			active: true,
			currentWindow: true
		}, tabs => {
			if (tabs[0]) {
				chrome.tabs.sendMessage(
					tabs[0].id,
					{ action: 'START_DOWNLOAD' },
					response => {
						if (chrome.runtime.lastError) {
							console.error('发送消息失败:', chrome.runtime.lastError);
							addLog('⚠️ 无法连接到页面，请刷新页面', 'error');
							isDownloading = false;
							updateUI();
							return;
						}
					}
				);
			}
		});

		await saveState();  // 保存状态
	} catch (error) {
		console.error('启动下载失败:', error);
		isDownloading = false;
		updateUI();
		addLog('启动下载失败: ' + error.message, 'error');
	}
}

// 停止下载
async function stopDownload() {
	if (!isDownloading) return;

	try {
		isDownloading = false;
		updateUI();
		addLog(`停止下载，共下载 ${downloadCount} 份简历`, 'warning');

		chrome.tabs.query({
			active: true,
			currentWindow: true
		}, tabs => {
			if (tabs[0]) {
				chrome.tabs.sendMessage(tabs[0].id, {
					action: 'STOP_DOWNLOAD'
				});
			}
		});

		await saveState();  // 保存状态
	} catch (error) {
		console.error('停止下载失败:', error);
		addLog('停止下载失败: ' + error.message, 'error');
	}
}

// 获取并显示排行榜数据
async function loadRankingData() {
	try {
		console.log('准备获取排行榜数据');

		fetchRankingData()
			.then(data => {
				renderRankingList(data);
			})
			.catch(error => {
				console.error('获取排行榜数据失败:', error);
				sendResponse({ status: 'error', error: error.message });
			});
	} catch (error) {
		console.error('加载排行榜数据出错:', error);
		// addLog('加载排行榜数据出错: ' + error.message, 'error');
	}
}

// 获取打赏排行榜数据
async function fetchRankingData() {
	try {
		const response = await fetch(`${API_BASE}/dashang.json?t=${Date.now()}`);
		const data = await response.json();
		return data;
	} catch (error) {
		console.error('获取排行榜数据失败:', error);
		return [];
	}
}

// 渲染排行榜
function renderRankingList(data) {
	console.log('渲染排行榜数据:', data);
	const container = document.getElementById('ranking-list');
	if (!container) return;

	if (!Array.isArray(data) || data.length === 0) {
		container.innerHTML = '<div class="ranking-item" style="text-align: center; color: #666;">暂无打赏数据</div>';
		return;
	}

	container.innerHTML = data.map((item, index) => `
		<div class="ranking-item">
			<div class="ranking-number">${index + 1}</div>
			<div class="ranking-info">
				<div class="ranking-name">${item.name || '匿名用户'}</div>
				<div class="ranking-desc">${item.describe || '无留言'}</div>
			</div>
			<div class="ranking-price">￥${item.price || 0}</div>
		</div>
	`).join('');
}

// 添加手机号绑定相关函数
async function bindPhone(phone) {
	try {
		if (!phone || !/^1\d{10}$/.test(phone)) {
			throw new Error('请输入正确的手机号');
		}

		// 先保存旧的手机号
		const oldPhone = boundPhone;
		boundPhone = phone;

		// 保存新手机号到存储
		await chrome.storage.local.set({ 'hr_assistant_phone': phone });

		// 如果是新绑定的手机号，先尝试从服务器同步数据
		if (phone !== oldPhone) {
			const hasServerData = await syncSettingsFromServer();
			if (hasServerData) {
				addLog(`已从手机号 ${phone} 同步配置`, 'success');
			} else {
				addLog(`手机号 ${phone} 绑定成功，暂无配置数据`, 'success');
			}
		}
	} catch (error) {
		addLog(error.message, 'error');
		throw error;
	}
}

// 从服务器同步设置
async function syncSettingsFromServer() {
	try {
		if (!boundPhone) return null;

		const response = await fetch(`${API_BASE}/getjson.php?phone=${boundPhone}`);
		if (!response.ok) {
			throw new Error('获取配置失败');
		}

		const data = await response.json();
		if (data && Object.keys(data).length > 0) {
			// 更新本地设置
			positions = data.positions || [];
			currentPosition = data.currentPosition || null;
			isAndMode = data.isAndMode || false;
			matchLimit = data.matchLimit || 200;
			enableSound = data.enableSound !== undefined ? data.enableSound : true;
			scrollDelayMin = data.scrollDelayMin || 3;
			scrollDelayMax = data.scrollDelayMax || 5;

			// 保存到本地存储，但不触发服务器同步
			await chrome.storage.local.set({
				positions,
				currentPosition,
				isAndMode,
				matchLimit,
				enableSound,
				scrollDelayMin,
				scrollDelayMax
			});

			// 更新UI
			renderPositions();
			if (currentPosition) {
				selectPosition(currentPosition.name);
			}

			// 更新输入框的值
			document.getElementById('match-limit').value = matchLimit;
			document.getElementById('delay-min').value = scrollDelayMin;
			document.getElementById('delay-max').value = scrollDelayMax;
			document.getElementById('enable-sound').checked = enableSound;
			document.getElementById('keywords-and-mode').checked = isAndMode;

			addLog('已从服务器同步配置', 'success');
			return true;
		}
		return false;
	} catch (error) {
		console.error('同步配置失败:', error);
		addLog('同步配置失败: ' + error.message, 'error');
		return false;
	}
}

// 同步设置到服务器
async function syncSettingsToServer() {
	try {
		if (!boundPhone) return;

		const settings = {
			positions,
			currentPosition,
			isAndMode,
			matchLimit,
			enableSound,
			scrollDelayMin,
			scrollDelayMax
		};

		const response = await fetch(`${API_BASE}/updatejson.php?phone=${boundPhone}`, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify(settings)
		});

		if (!response.ok) {
			throw new Error('保存配置失败');
		}

		addLog('配置已同步到服务器', 'success');
	} catch (error) {
		console.error('同步配置失败:', error);
		addLog('同步配置失败: ' + error.message, 'error');
	}
}