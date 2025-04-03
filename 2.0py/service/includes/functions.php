<?php
/**
 * 通用函数库
 */

// 设置响应头
header('Content-Type: application/json; charset=utf-8');
header('Access-Control-Allow-Origin: *');
header('Access-Control-Allow-Methods: GET, POST, OPTIONS');
header('Access-Control-Allow-Headers: Content-Type, Access-Control-Allow-Headers, Authorization, X-Requested-With');

// 处理OPTIONS请求
if ($_SERVER['REQUEST_METHOD'] === 'OPTIONS') {
    http_response_code(200);
    exit;
}

// 定义常量
define('BASE_DIR', dirname(dirname(__FILE__)));
define('USERS_DIR', BASE_DIR . '/config/users');
define('PLATFORMS_DIR', BASE_DIR . '/config/platforms');
define('LOGS_DIR', BASE_DIR . '/logs');

// 确保目录存在
if (!file_exists(USERS_DIR)) {
    mkdir(USERS_DIR, 0755, true);
}
if (!file_exists(PLATFORMS_DIR)) {
    mkdir(PLATFORMS_DIR, 0755, true);
}
if (!file_exists(LOGS_DIR)) {
    mkdir(LOGS_DIR, 0755, true);
}

/**
 * 验证手机号是否合法
 * @param string $phone 手机号
 * @return bool 是否合法
 */
function validatePhone($phone) {
    return preg_match('/^1[3-9]\d{9}$/', $phone);
}

/**
 * 发送错误响应
 * @param string $message 错误信息
 * @param int $code HTTP状态码
 */
function sendError($message, $code = 400) {
    http_response_code($code);
    echo json_encode([
        'success' => false,
        'error' => $message
    ], JSON_UNESCAPED_UNICODE);
    exit;
}

/**
 * 发送成功响应
 * @param mixed $data 响应数据
 * @param string $message 成功信息
 */
function sendSuccess($data = null, $message = '操作成功') {
    echo json_encode([
        'success' => true,
        'message' => $message,
        'data' => $data
    ], JSON_UNESCAPED_UNICODE);
    exit;
}

/**
 * 记录日志
 * @param string $action 操作
 * @param array $data 相关数据
 */
function logAction($action, $data = []) {
    $logFile = LOGS_DIR . '/' . date('Y-m-d') . '.log';
    $logData = [
        'timestamp' => date('Y-m-d H:i:s'),
        'action' => $action,
        'ip' => $_SERVER['REMOTE_ADDR'],
        'data' => $data
    ];
    
    $logLine = json_encode($logData, JSON_UNESCAPED_UNICODE) . "\n";
    file_put_contents($logFile, $logLine, FILE_APPEND);
}

/**
 * 获取用户配置文件路径
 * @param string $phone 手机号
 * @return string 文件路径
 */
function getUserConfigPath($phone) {
    return USERS_DIR . '/' . $phone . '.json';
}

/**
 * 获取用户配置
 * @param string $phone 手机号
 * @return array|null 用户配置
 */
function getUserConfig($phone) {
    $configPath = getUserConfigPath($phone);
    
    if (file_exists($configPath)) {
        $content = file_get_contents($configPath);
        $config = json_decode($content, true);
        
        if ($config) {
            return $config;
        }
    }
    
    return null;
}

/**
 * 创建默认用户配置
 * @param string $phone 手机号
 * @return array 默认配置
 */
function createDefaultUserConfig($phone) {
    $currentDate = date('Y-m-d');
    
    $config = [
        'username' => $phone,
        'version' => 'free',
        'platform' => '',
        'jobsData' => [],
        'keywordsData' => [],
        'selectedJob' => '',
        'versions' => [
            'free' => [
                'greetCount' => 0,
                'remainingQuota' => 100,
                'expiryDate' => '永久有效',
                'lastResetDate' => $currentDate
            ],
            'donation' => [
                'greetCount' => 0,
                'remainingQuota' => 0,
                'expiryDate' => '',
                'lastResetDate' => $currentDate
            ],
            'enterprise' => [
                'greetCount' => 0,
                'remainingQuota' => 0,
                'expiryDate' => '',
                'lastResetDate' => $currentDate
            ]
        ],
        'created_at' => $currentDate,
        'updated_at' => $currentDate
    ];
    
    saveUserConfig($phone, $config);
    logAction('创建用户', ['phone' => $phone]);
    
    return $config;
}

/**
 * 保存用户配置
 * @param string $phone 手机号
 * @param array $config 配置数据
 * @return bool 是否成功
 */
function saveUserConfig($phone, $config) {
    // 更新时间戳
    $config['updated_at'] = date('Y-m-d H:i:s');
    
    $configPath = getUserConfigPath($phone);
    $content = json_encode($config, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT);
    
    if (file_put_contents($configPath, $content)) {
        return true;
    }
    
    return false;
}

/**
 * 获取平台配置
 * @param string $platform 平台名称，为空则返回所有平台
 * @return array 平台配置
 */
function getPlatformConfig($platform = '') {
    if (!empty($platform)) {
        $configPath = PLATFORMS_DIR . '/' . $platform . '.json';
        
        if (file_exists($configPath)) {
            $content = file_get_contents($configPath);
            $config = json_decode($content, true);
            
            if ($config) {
                return $config;
            }
        }
        
        return null;
    } else {
        $platforms = [];
        $files = glob(PLATFORMS_DIR . '/*.json');
        
        foreach ($files as $file) {
            $platformName = basename($file, '.json');
            $content = file_get_contents($file);
            $config = json_decode($content, true);
            
            if ($config) {
                $platforms[$platformName] = $config;
            }
        }
        
        return $platforms;
    }
}

/**
 * 保存平台配置
 * @param string $platform 平台名称
 * @param array $config 配置数据
 * @return bool 是否成功
 */
function savePlatformConfig($platform, $config) {
    // 更新时间戳
    $config['updated_at'] = date('Y-m-d H:i:s');
    
    $configPath = PLATFORMS_DIR . '/' . $platform . '.json';
    $content = json_encode($config, JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT);
    
    if (file_put_contents($configPath, $content)) {
        logAction('保存平台配置', ['platform' => $platform]);
        return true;
    }
    
    return false;
} 