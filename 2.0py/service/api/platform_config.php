<?php
/**
 * 平台配置 API
 */
require_once '../includes/functions.php';

// 处理 GET 请求 - 获取平台配置
if ($_SERVER['REQUEST_METHOD'] === 'GET') {
    // 获取平台名称
    $platform = isset($_GET['platform']) ? $_GET['platform'] : '';
    
    // 如果指定了平台名称，则获取特定平台的配置
    if (!empty($platform)) {
        $config = getPlatformConfig($platform);
        
        if ($config) {
            logAction('获取平台配置', ['platform' => $platform]);
            sendSuccess($config);
        } else {
            sendError("未找到平台配置");
        }
    } else {
        // 获取所有平台的配置
        $platforms = getPlatformConfig();
        logAction('获取所有平台配置');
        sendSuccess($platforms);
    }
}

// 处理 POST 请求 - 更新平台配置
if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    // 获取请求数据
    $requestData = json_decode(file_get_contents('php://input'), true);
    
    if (!$requestData) {
        sendError("无效的请求数据");
    }
    
    // 获取平台名称
    $platform = $requestData['platform'] ?? '';
    
    if (empty($platform)) {
        sendError("未指定平台名称");
    }
    
    // 获取平台配置
    $config = $requestData['config'] ?? [];
    
    if (empty($config)) {
        sendError("平台配置为空");
    }
    
    // 如果配置中没有更新时间，添加创建时间
    if (!isset($config['created_at'])) {
        $config['created_at'] = date('Y-m-d H:i:s');
    }
    
    // 保存平台配置
    if (savePlatformConfig($platform, $config)) {
        logAction('更新平台配置', ['platform' => $platform]);
        sendSuccess(null, "平台配置已保存");
    } else {
        sendError("保存平台配置失败");
    }
} 