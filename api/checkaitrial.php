<?php
header('Content-Type: application/json');
header('Access-Control-Allow-Origin: *');
header('Access-Control-Allow-Methods: POST, GET, OPTIONS');
header('Access-Control-Allow-Headers: Content-Type, Authorization, X-Requested-With');

// 处理OPTIONS请求（预检请求）
if ($_SERVER['REQUEST_METHOD'] === 'OPTIONS') {
    exit(0);
}

// 获取请求方法
$method = $_SERVER['REQUEST_METHOD'];

// 处理GET请求（向后兼容）
if ($method === 'GET') {
    // 获取请求参数
    $fingerprint = $_GET['fingerprint'] ?? '';
    
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
    $deviceUsed = isset($fingerprints[$fingerprint]);
    $associatedPhone = $deviceUsed ? $fingerprints[$fingerprint]['phone'] : null;
    
    // 如果设备已使用过，检查该手机号是否还有试用期
    $hasTrial = false;
    if ($deviceUsed && $associatedPhone) {
        $phoneFile = $storageDir . $associatedPhone . '.json';
        if (file_exists($phoneFile)) {
            $phoneData = json_decode(file_get_contents($phoneFile), true);
            if ($phoneData && isset($phoneData['ai_expire_time'])) {
                $expireTime = $phoneData['ai_expire_time'];
                $currentDate = date('Y-m-d');
                $hasTrial = $expireTime >= $currentDate;
            }
        }
    }
    
    // 返回结果
    echo json_encode([
        'device_used' => $deviceUsed,
        'associated_phone' => $associatedPhone,
        'has_trial' => $hasTrial
    ]);
    exit;
}

// 只处理POST请求
if ($method !== 'POST') {
    echo json_encode(['success' => false, 'message' => '只支持POST请求']);
    exit;
}

// 获取POST数据
$json = file_get_contents('php://input');
$data = json_decode($json, true);

// 验证必需的参数
if (!isset($data['phone']) || !isset($data['fingerprint'])) {
    echo json_encode(['success' => false, 'message' => '缺少必需的参数']);
    exit;
}

$phone = $data['phone'];
$fingerprint = $data['fingerprint'];
$action = isset($data['action']) ? $data['action'] : 'check';

// 验证手机号格式
if (!preg_match('/^1\d{10}$/', $phone)) {
    echo json_encode(['success' => false, 'message' => '手机号格式不正确']);
    exit;
}

// 数据存储目录
$storageDir = 'storage/';
$fingerprintFile = $storageDir . 'fingerprints.json';

// 确保数据目录存在
if (!file_exists($storageDir)) {
    mkdir($storageDir, 0755, true);
}

// 读取现有的指纹数据
$fingerprints = [];
if (file_exists($fingerprintFile)) {
    $jsonContent = file_get_contents($fingerprintFile);
    $fingerprints = json_decode($jsonContent, true) ?: [];
}

// 根据操作类型处理
switch ($action) {
    case 'record':
        // 检查设备是否已绑定其他手机号
        if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] !== $phone) {
            echo json_encode([
                'success' => false, 
                'message' => '此设备已绑定其他手机号，无法绑定新手机号'
            ]);
            break;
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
                'success' => false, 
                'message' => '此手机号已绑定其他设备，一个手机号只能绑定一个设备'
            ]);
            break;
        }
        
        // 检查是否是同一设备重复绑定同一手机号
        if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] === $phone) {
            echo json_encode([
                'success' => true, 
                'message' => '设备已绑定此手机号，无需重复绑定'
            ]);
            break;
        }
        
        // 使用文件锁防止并发写入
        $file = fopen($fingerprintFile, 'c+');
        if (flock($file, LOCK_EX)) {
            // 重新读取文件以确保获取最新数据
            $jsonContent = stream_get_contents($file);
            if ($jsonContent !== false && !empty($jsonContent)) {
                $fingerprints = json_decode($jsonContent, true) ?: [];
            }
            
            // 再次检查设备是否已绑定其他手机号（防止在读取文件后被其他请求修改）
            if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] !== $phone) {
                flock($file, LOCK_UN);
                fclose($file);
                echo json_encode([
                    'success' => false, 
                    'message' => '此设备已绑定其他手机号，无法绑定新手机号'
                ]);
                break;
            }
            
            // 再次检查手机号是否已绑定其他设备（防止在读取文件后被其他请求修改）
            $phoneBoundToOtherDevice = false;
            foreach ($fingerprints as $fp => $data) {
                if (isset($data['phone']) && $data['phone'] === $phone && $fp !== $fingerprint) {
                    $phoneBoundToOtherDevice = true;
                    break;
                }
            }
            
            if ($phoneBoundToOtherDevice) {
                flock($file, LOCK_UN);
                fclose($file);
                echo json_encode([
                    'success' => false, 
                    'message' => '此手机号已绑定其他设备，一个手机号只能绑定一个设备'
                ]);
                exit;
            }
            
            // 再次检查是否是同一设备重复绑定同一手机号
            if (isset($fingerprints[$fingerprint]) && $fingerprints[$fingerprint]['phone'] === $phone) {
                flock($file, LOCK_UN);
                fclose($file);
                echo json_encode([
                    'success' => true, 
                    'message' => '设备已绑定此手机号，无需重复绑定'
                ]);
                exit;
            }
            
            // 记录设备指纹
            $fingerprints[$fingerprint] = [
                'phone' => $phone,
                'first_used' => isset($fingerprints[$fingerprint]['first_used']) ? $fingerprints[$fingerprint]['first_used'] : date('Y-m-d H:i:s'),
                'last_used' => date('Y-m-d H:i:s'),
                'used' => true
            ];
            
            // 写入文件
            ftruncate($file, 0);
            rewind($file);
            fwrite($file, json_encode($fingerprints, JSON_PRETTY_PRINT));
            flock($file, LOCK_UN);
        }
        fclose($file);
        
        echo json_encode([
            'success' => true, 
            'message' => '设备指纹记录成功'
        ]);
        break;
        
    case 'check':
        // 检查设备指纹
        $response = [
            'success' => true,
            'used' => false,
            'hasAccess' => true,
            'message' => '设备未使用过AI功能'
        ];
        
        if (isset($fingerprints[$fingerprint])) {
            $fpData = $fingerprints[$fingerprint];
            $response['used'] = $fpData['used'];
            
            // 检查是否绑定了手机号
            if ($fpData['phone'] !== $phone) {
                $response['hasAccess'] = false;
                $response['message'] = '此设备已绑定其他手机号';
            } else {
                // 检查AI试用期是否过期
                $phoneFile = $storageDir . $phone . '.json';
                if (file_exists($phoneFile)) {
                    $userData = json_decode(file_get_contents($phoneFile), true) ?: [];
                    if (isset($userData['ai_expire_time'])) {
                        $expireDate = new DateTime($userData['ai_expire_time']);
                        $now = new DateTime();
                        if ($now > $expireDate) {
                            $response['hasAccess'] = false;
                            $response['message'] = 'AI试用期已过期';
                        } else {
                            $response['message'] = 'AI试用期有效';
                        }
                    }
                }
            }
            
            // 使用文件锁更新最后使用时间
            $file = fopen($fingerprintFile, 'c+');
            if (flock($file, LOCK_EX)) {
                // 重新读取文件以确保获取最新数据
                $jsonContent = stream_get_contents($file);
                if ($jsonContent !== false && !empty($jsonContent)) {
                    $fingerprints = json_decode($jsonContent, true) ?: [];
                }
                
                // 更新最后使用时间
                if (isset($fingerprints[$fingerprint])) {
                    $fingerprints[$fingerprint]['last_used'] = date('Y-m-d H:i:s');
                    
                    // 写入文件
                    ftruncate($file, 0);
                    rewind($file);
                    fwrite($file, json_encode($fingerprints, JSON_PRETTY_PRINT));
                }
                flock($file, LOCK_UN);
            }
            fclose($file);
        }
        
        echo json_encode($response);
        break;
        
    default:
        echo json_encode(['success' => false, 'message' => '未知操作类型']);
        break;
}
?>