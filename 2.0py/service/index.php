<?php
/**
 * API 入口文件
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

// 获取请求路径
$requestUri = $_SERVER['REQUEST_URI'];
$basePath = '/service/';
$path = '';

// 提取API路径
if (strpos($requestUri, $basePath) === 0) {
    $path = substr($requestUri, strlen($basePath));
}

// 截取查询字符串
if (($pos = strpos($path, '?')) !== false) {
    $path = substr($path, 0, $pos);
}

// 去除尾部斜杠
$path = rtrim($path, '/');

// 路由到对应的API
switch ($path) {
    case 'user':
    case 'user_config':
        require_once 'api/user_config.php';
        break;
    
    case 'count':
    case 'update_count':
        require_once 'api/update_count.php';
        break;
    
    case 'platform':
    case 'platform_config':
        require_once 'api/platform_config.php';
        break;
    
    case 'admin':
        require_once 'api/admin.php';
        break;
    
    case '':
    case 'index':
        // API 首页，显示可用的 API 列表
        echo json_encode([
            'success' => true,
            'message' => '智联招聘助手 API 服务',
            'apis' => [
                'user_config' => '用户配置 API',
                'update_count' => '打招呼计数更新 API',
                'platform_config' => '平台配置 API'
            ]
        ], JSON_UNESCAPED_UNICODE);
        break;
    
    default:
        // API 不存在
        http_response_code(404);
        echo json_encode([
            'success' => false,
            'error' => '请求的API不存在'
        ], JSON_UNESCAPED_UNICODE);
        break;
} 