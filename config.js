// GoodHR 配置文件
const CONFIG = {
    // 服务器配置
    API_BASE: 'https://goodhr.58it.cn',

    // 轨迹流动AI配置
    GUJJI_API: {
        baseUrl: 'https://api.siliconflow.cn/v1/chat/completions',
        maxTokens: 100,
        temperature: 0.1
    },
    // 版本信息
    VERSION: '2.8.1',

    // 默认设置
    DEFAULTS: {
        matchLimit: 200,
        scrollDelayMin: 3,
        scrollDelayMax: 5,
        clickFrequency: 7,
        enableSound: true
    },

    // 公司信息配置
    COMPANY_INFO: {
        name: "",           // 公司名称
        address: "",        // 公司地址
        scale: "",          // 公司规模
        industry: "",       // 公司行业
        description: "",    // 公司简介
        culture: "",        // 公司文化
        benefits: "",       // 福利待遇
        location: "",       // 工作地点
        website: ""         // 公司官网
    },

    // 岗位信息配置
    JOB_INFO: {
        position: "",       // 岗位名称
        responsibilities: [], // 岗位职责
        requirements: [],   // 任职要求
        salary: "",         // 薪资范围
        workHours: "",      // 工作时间
        overtime: "",       // 是否加班
        benefits: "",       // 岗位福利
        growth: "",         // 晋升空间
        team: "",           // 团队介绍
        environment: ""     // 工作环境
    },

    // 沟通处理配置
    COMMUNICATION_CONFIG: {
        enabled: false,     // 是否启用沟通处理功能
        collectPhone: true, // 是否收集手机号
        collectWechat: true,// 是否收集微信号
        collectResume: true // 是否收集简历附件
    },

    // 运行模式配置
    RUN_MODE_CONFIG: {
        greetingEnabled: true,     // 是否启用打招呼功能
        communicationEnabled: true // 是否启用沟通处理功能
    }
};

// 如果在浏览器环境中，将配置暴露给全局
if (typeof window !== 'undefined') {
    window.GOODHR_CONFIG = CONFIG;
}

// 如果在Node.js环境中，导出配置
if (typeof module !== 'undefined' && module.exports) {
    module.exports = CONFIG;
}
