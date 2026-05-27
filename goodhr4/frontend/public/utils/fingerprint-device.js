/**
 * 设备指纹工具类 - 使用FingerprintJS库
 */
class DeviceFingerprint {
    constructor() {
        this.fingerprint = null;
        this.isLoaded = false;
    }

    /**
     * 加载FingerprintJS库
     * @returns {Promise<void>}
     */
    async loadFingerprintJS() {
        return new Promise((resolve, reject) => {
            if (window.FingerprintJS) {
                this.isLoaded = true;
                resolve();
                return;
            }

            const script = document.createElement('script');
            script.src = chrome.runtime.getURL('utils/fp.min.js');
            script.onload = () => {
                this.isLoaded = true;
                resolve();
            };
            script.onerror = () => {
                reject(new Error('Failed to load FingerprintJS'));
            };
            document.head.appendChild(script);
        });
    }

    /**
     * 生成设备指纹
     * @returns {Promise<string>} 设备指纹
     */
    async generateFingerprint() {
        try {
            // 首先检查是否已有存储的指纹
            const storedFingerprint = this.getStoredFingerprint();
            if (storedFingerprint) {
                this.fingerprint = storedFingerprint;
                return this.fingerprint;
            }

            // 确保FingerprintJS已加载
            if (!this.isLoaded) {
                await this.loadFingerprintJS();
            }

            // 初始化FingerprintJS v5
            const fp = await window.FingerprintJS.load({
                // 使用更稳定的配置选项
                debug: false
            });

            // 获取访客ID - v5 API
            const result = await fp.get();
            
            // 生成唯一指纹
            this.fingerprint = 'hr_device_' + result.visitorId;
            
            // 存储指纹以提高稳定性
            this.storeFingerprint(this.fingerprint);
            
            return this.fingerprint;
        } catch (error) {
            console.error('生成设备指纹失败:', error);
            // 降级方案：使用时间戳和随机数生成临时指纹
            this.fingerprint = 'hr_device_' + Date.now().toString(16) + '_' + Math.random().toString(36).substring(2);
            // 也存储降级指纹
            this.storeFingerprint(this.fingerprint);
            return this.fingerprint;
        }
    }

    /**
     * 存储设备指纹到本地存储
     * @param {string} fingerprint - 设备指纹
     */
    storeFingerprint(fingerprint) {
        try {
            // 使用localStorage存储指纹
            localStorage.setItem('goodhr_device_fingerprint', fingerprint);
            // 同时记录生成时间，用于判断是否需要刷新
            localStorage.setItem('goodhr_fingerprint_time', Date.now().toString());
        } catch (error) {
            console.error('存储设备指纹失败:', error);
        }
    }

    /**
     * 从本地存储获取设备指纹
     * @returns {string|null} 存储的设备指纹
     */
    getStoredFingerprint() {
        try {
            const fingerprint = localStorage.getItem('goodhr_device_fingerprint');
            const timestamp = localStorage.getItem('goodhr_fingerprint_time');
            
            if (!fingerprint || !timestamp) {
                return null;
            }
            
            // 检查指纹是否过期（7天）
            const now = Date.now();
            const sevenDaysInMs = 7 * 24 * 60 * 60 * 1000;
            
            if (now - parseInt(timestamp, 10) > sevenDaysInMs) {
                // 指纹已过期，清除并返回null
                localStorage.removeItem('goodhr_device_fingerprint');
                localStorage.removeItem('goodhr_fingerprint_time');
                return null;
            }
            
            return fingerprint;
        } catch (error) {
            console.error('获取存储的设备指纹失败:', error);
            return null;
        }
    }

    /**
     * 获取当前设备指纹
     * @returns {string|null} 当前设备指纹
     */
    getFingerprint() {
        return this.fingerprint;
    }

    /**
     * 检查设备是否已使用过AI功能
     * @param {string} phone - 用户手机号
     * @returns {Promise<Object>} 检查结果
     */
    async checkDeviceUsage(phone) {
        try {
            // 确保已生成指纹
            if (!this.fingerprint) {
                await this.generateFingerprint();
            }

            // 获取API基础URL
            const apiBase = window.GOODHR_CONFIG ? window.GOODHR_CONFIG.API_BASE : 'https://goodhr.58it.cn';
            
            // 调用API检查设备使用状态
            const response = await fetch(`${apiBase}/checkfingerprint.php?fingerprint=${encodeURIComponent(this.fingerprint)}&phone=${encodeURIComponent(phone)}`);
            const data = await response.json();
            
            return data;
        } catch (error) {
            console.error('检查设备使用状态失败:', error);
            return {
                device_used: false,
                associated_phone: null,
                phone: phone
            };
        }
    }

    /**
     * 检查设备是否有AI试用期
     * @returns {Promise<Object>} 检查结果
     */
    async checkAITrialStatus() {
        try {
            // 确保已生成指纹
            if (!this.fingerprint) {
                await this.generateFingerprint();
            }

            // 获取API基础URL
            const apiBase = window.GOODHR_CONFIG ? window.GOODHR_CONFIG.API_BASE : 'https://goodhr.58it.cn';
            
            // 调用API检查试用期状态
            const response = await fetch(`${apiBase}/checkaitrial.php?fingerprint=${encodeURIComponent(this.fingerprint)}`);
            const data = await response.json();
            
            return data;
        } catch (error) {
            console.error('检查AI试用期状态失败:', error);
            return {
                device_used: false,
                associated_phone: null,
                has_trial: false
            };
        }
    }
}

// 导出类
if (typeof module !== 'undefined' && module.exports) {
    module.exports = DeviceFingerprint;
} else {
    window.DeviceFingerprint = DeviceFingerprint;
}