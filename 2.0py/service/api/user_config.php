<?php
/**
 * 用户配置 API
 */
require_once '../includes/functions.php';

// 处理 GET 请求 - 获取用户配置
if ($_SERVER['REQUEST_METHOD'] === 'GET') {
    // 获取手机号
    $phone = isset($_GET['phone']) ? $_GET['phone'] : '';
    
    // 验证手机号
    if (empty($phone) || !validatePhone($phone)) {
        sendError("手机号格式不正确");
    }
    
    // 获取用户配置
    $config = getUserConfig($phone);
    
    // 如果用户配置不存在，创建默认配置
    if (empty($config)) {
        $config = createDefaultUserConfig($phone);
        logAction('获取用户配置', ['phone' => $phone, 'created' => true]);
    } else {
        logAction('获取用户配置', ['phone' => $phone]);
    }
    
    // 检查是否需要重置每日数据
    $currentDate = date('Y-m-d');
    $version = $config['version'] ?? 'free';
    $versionData = &$config['versions'][$version];
    
    if (isset($versionData['lastResetDate']) && $versionData['lastResetDate'] !== $currentDate) {
        $versionData['lastResetDate'] = $currentDate;
        
        // 只有免费版每天有固定配额
        if ($version === 'free') {
            $versionData['greetCount'] = 0;
            $versionData['remainingQuota'] = 100;
        }
        
        // 保存更新后的配置
        saveUserConfig($phone, $config);
    }
    
    sendSuccess($config);
}

// 处理 POST 请求 - 更新用户配置
if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    // 获取请求数据
    $requestData = json_decode(file_get_contents('php://input'), true);
    
    if (!$requestData) {
        sendError("无效的请求数据");
    }
    
    // 获取手机号
    $phone = $requestData['phone'] ?? '';
    
    // 验证手机号
    if (empty($phone) || !validatePhone($phone)) {
        sendError("手机号格式不正确");
    }
    
    // 获取现有配置
    $existingConfig = getUserConfig($phone);
    
    // 如果配置不存在，创建默认配置
    if (empty($existingConfig)) {
        $existingConfig = createDefaultUserConfig($phone);
    }
    
    // 合并新配置
    // 忽略一些敏感字段，防止客户端误修改
    unset($requestData['created_at']);
    
    // 如果客户端没有提供 versions 字段，保留现有的
    if (!isset($requestData['versions']) && isset($existingConfig['versions'])) {
        $requestData['versions'] = $existingConfig['versions'];
    }
    
    // 合并配置
    $newConfig = array_merge($existingConfig, $requestData);
    
    // 保存配置
    if (saveUserConfig($phone, $newConfig)) {
        logAction('更新用户配置', ['phone' => $phone]);
        sendSuccess(null, "配置已保存");
    } else {
        sendError("保存配置失败");
    }
} 