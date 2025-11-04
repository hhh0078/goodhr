<?php
header('Access-Control-Allow-Origin: *');
header('Content-Type: application/json; charset=utf-8');

if ($_SERVER['REQUEST_METHOD'] !== 'POST') {
    http_response_code(405);
    echo json_encode(['error' => '只支持POST请求']);
    exit;
}

$phone = $_GET['phone'] ?? '';
if (!preg_match('/^1[3-9]\d{9}$/', $phone)) {
    http_response_code(400);
    echo json_encode(['error' => '无效的手机号格式']);
    exit;
}

$jsonData = file_get_contents('php://input');
$data = json_decode($jsonData, true);
if ($data === null) {
    http_response_code(400);
    echo json_encode(['error' => '无效的JSON数据']);
    exit;
}

$storageDir = 'storage/';
if (!is_dir($storageDir)) {
    mkdir($storageDir, 0777, true);
}

$filename = $storageDir . $phone . '.json';

// 使用文件锁防止并发写入
$file = fopen($filename, 'c+');
if (flock($file, LOCK_EX)) {
    $localJson = file_get_contents($filename);
    $localData = json_decode($localJson, true);

    // 如果本地有非null的ai_expire_time，强制使用它
    if ($localData !== null && isset($localData['ai_expire_time']) && $localData['ai_expire_time'] !== null) {
        $data['ai_expire_time'] = $localData['ai_expire_time'];
    }

    // 清空文件并写入新数据
    ftruncate($file, 0);
    fwrite($file, json_encode($data));
    flock($file, LOCK_UN);
}
fclose($file);

echo json_encode(['success' => true, 'message' => '配置已保存']);
?>