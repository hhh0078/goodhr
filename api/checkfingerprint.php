<?php
header('Access-Control-Allow-Origin: *');
header('Content-Type: application/json; charset=utf-8');

// 获取请求参数
$fingerprint = $_GET['fingerprint'] ?? '';
$phone = $_GET['phone'] ?? '';

// 验证指纹格式
if (empty($fingerprint)) {
    http_response_code(400);
    echo json_encode(['error' => '缺少设备指纹参数']);
    exit;
}

// 数据存储路径
$storageDir = 'storage/';
$fingerprintFile = $storageDir . 'fingerprints.json';

// 确保存储目录存在
if (!is_dir($storageDir)) {
    mkdir($storageDir, 0777, true);
}

// 读取现有指纹数据
$fingerprints = [];
if (file_exists($fingerprintFile)) {
    $jsonContent = file_get_contents($fingerprintFile);
    if ($jsonContent !== false) {
        $fingerprints = json_decode($jsonContent, true) ?: [];
    }
}

// 检查设备是否已使用过AI功能
$deviceUsed = false;
$associatedPhone = null;

if (isset($fingerprints[$fingerprint])) {
    $deviceUsed = true;
    $associatedPhone = $fingerprints[$fingerprint]['phone'];
}

// 如果提供了手机号，则记录指纹和手机号的关联
if (!empty($phone) && preg_match('/^1[3-9]\d{9}$/', $phone)) {
    // 检查设备是否已绑定其他手机号
    if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] !== $phone) {
        // 设备已绑定其他手机号，不允许覆盖
        echo json_encode([
            'device_used' => true,
            'associated_phone' => $fingerprints[$fingerprint]['phone'],
            'phone' => $phone,
            'error' => '此设备已绑定其他手机号，无法绑定新手机号'
        ]);
        exit;
    }
    
    // 检查手机号是否已绑定其他设备
    $phoneBoundToOtherDevice = false;
    foreach ($fingerprints as $fp => $data) {
        if (isset($data['phone']) && $data['phone'] === $phone && $fp !== $fingerprint) {
            $phoneBoundToOtherDevice = true;
            break;
        }
    }
    
    if ($phoneBoundToOtherDevice) {
        echo json_encode([
            'device_used' => true,
            'associated_phone' => $data['phone'],
            'phone' => $phone,
            'error' => '此手机号已绑定其他设备，一个手机号只能绑定一个设备'
        ]);
        exit;
    }
    
    // 检查是否是同一设备重复绑定同一手机号
    if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] === $phone) {
        // 更新最后使用时间
        $fingerprints[$fingerprint]['last_used'] = date('Y-m-d H:i:s');
        
        // 使用文件锁防止并发写入
        $file = fopen($fingerprintFile, 'c+');
        if (flock($file, LOCK_EX)) {
            ftruncate($file, 0);
            fwrite($file, json_encode($fingerprints));
            flock($file, LOCK_UN);
        }
        fclose($file);
        
        echo json_encode([
            'device_used' => true,
            'associated_phone' => $phone,
            'phone' => $phone,
            'message' => '设备已绑定此手机号，无需重复绑定'
        ]);
        exit;
    }
    
    $fingerprints[$fingerprint] = [
        'phone' => $phone,
        'first_used' => isset($fingerprints[$fingerprint]['first_used']) ? $fingerprints[$fingerprint]['first_used'] : date('Y-m-d H:i:s'),
        'last_used' => date('Y-m-d H:i:s')
    ];
    
    // 使用文件锁防止并发写入
    $file = fopen($fingerprintFile, 'c+');
    if (flock($file, LOCK_EX)) {
        ftruncate($file, 0);
        fwrite($file, json_encode($fingerprints));
        flock($file, LOCK_UN);
    }
    fclose($file);
}

// 返回结果
echo json_encode([
    'device_used' => $deviceUsed,
    'associated_phone' => $associatedPhone,
    'phone' => $phone
]);
?>