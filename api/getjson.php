<?php
header('Access-Control-Allow-Origin: *');
header('Content-Type: application/json; charset=utf-8');

// 获取手机号参数
$phone = $_GET['phone'] ?? '';

// 验证手机号格式
if (!preg_match('/^1[3-9]\d{9}$/', $phone)) {
    http_response_code(400);
    echo json_encode(['error' => '无效的手机号格式']);
    exit;
}

// 数据存储路径
$storageDir = 'storage/';
$filename = $storageDir . $phone . '.json';

// 检查文件是否存在
if (!file_exists($filename)) {
    echo json_encode([]); // 返回空对象
    exit;
}

// 读取数据
$jsonData = file_get_contents($filename);
if ($jsonData === false) {
    http_response_code(500);
    echo json_encode(['error' => '读取配置失败']);
    exit;
}

// 验证JSON格式
$data = json_decode($jsonData);
if ($data === null) {
    http_response_code(500);
    echo json_encode(['error' => '配置数据格式错误']);
    exit;
}

// 返回数据
echo $jsonData;
?>