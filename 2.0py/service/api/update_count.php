<?php
/**
 * 打招呼计数更新 API
 */
require_once '../includes/functions.php';

// 只处理 POST 请求
if ($_SERVER['REQUEST_METHOD'] !== 'POST') {
    sendError("不支持的请求方法", 405);
}

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
$config = getUserConfig($phone);

// 如果配置不存在，创建默认配置
if (empty($config)) {
    $config = createDefaultUserConfig($phone);
}

// 获取当前版本
$version = $config['version'] ?? 'free';
$versionData = &$config['versions'][$version];

// 获取当前日期
$currentDate = date('Y-m-d');

// 检查是否需要重置每日数据
if (isset($versionData['lastResetDate']) && $versionData['lastResetDate'] !== $currentDate) {
    $versionData['lastResetDate'] = $currentDate;
    
    // 只有免费版每天有固定配额
    if ($version === 'free') {
        $versionData['greetCount'] = 0;
        $versionData['remainingQuota'] = 100;
    }
}

// 更新打招呼计数
$greetCount = (int)($versionData['greetCount'] ?? 0);
$greetCount++;
$versionData['greetCount'] = $greetCount;

// 更新剩余配额
if ($version === 'free') {
    $remainingQuota = (int)($versionData['remainingQuota'] ?? 0);
    $remainingQuota = max(0, $remainingQuota - 1);
    $versionData['remainingQuota'] = $remainingQuota;
}

// 保存配置
if (saveUserConfig($phone, $config)) {
    logAction('更新打招呼计数', [
        'phone' => $phone,
        'version' => $version,
        'greetCount' => $greetCount
    ]);
    
    // 构造版本信息
    $versionInfo = [
        'version' => $version,
        'greetCount' => $versionData['greetCount'],
        'remainingQuota' => $versionData['remainingQuota'] ?? 0,
        'expiryDate' => $versionData['expiryDate'] ?? '',
    ];
    
    sendSuccess($versionInfo, "打招呼计数已更新");
} else {
    sendError("更新打招呼计数失败");
} 