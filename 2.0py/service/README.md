# 智联招聘助手服务端

## 简介

本服务端提供了智联招聘助手的配置管理API，支持用户配置的存储和检索、打招呼计数的管理、以及平台配置的管理。

## 目录结构

```
service/
  ├── api/                 # API接口文件
  │   ├── user_config.php       # 用户配置API
  │   ├── update_count.php      # 打招呼计数API
  │   └── platform_config.php   # 平台配置API
  ├── config/              # 配置文件目录
  │   ├── users/                # 用户配置文件
  │   └── platforms/            # 平台配置文件
  ├── includes/            # 公共函数库
  │   └── functions.php         # 通用函数
  ├── logs/                # 日志目录
  ├── .htaccess            # Apache配置文件
  ├── index.php            # 入口文件
  └── README.md            # 说明文档
```

## 部署方法

1. 将整个 `service` 目录上传到支持PHP的Web服务器（如Apache、Nginx+PHP-FPM）
2. 确保PHP版本 >= 7.0
3. 确保 `config` 和 `logs` 目录有写入权限
4. 如果使用Apache，确保开启了 `mod_rewrite` 模块
5. 访问 `http://your-domain.com/service/` 测试是否部署成功

## API说明

### 1. 用户配置 API

- 接口: `/service/user_config`
- 方法: `GET` 和 `POST`

#### GET 请求 - 获取用户配置

参数:
- `phone`: 用户手机号

示例请求:
```
GET /service/user_config?phone=13812345678
```

示例响应:
```json
{
  "success": true,
  "message": "操作成功",
  "data": {
    "username": "13812345678",
    "version": "free",
    "platform": "",
    "jobsData": [],
    "keywordsData": {},
    "selectedJob": "",
    "versions": {
      "free": {
        "greetCount": 0,
        "remainingQuota": 100,
        "expiryDate": "永久有效",
        "lastResetDate": "2025-03-18"
      },
      "donation": {
        "greetCount": 0,
        "remainingQuota": 0,
        "expiryDate": "",
        "lastResetDate": "2025-03-18"
      },
      "enterprise": {
        "greetCount": 0,
        "remainingQuota": 0,
        "expiryDate": "",
        "lastResetDate": "2025-03-18"
      }
    },
    "created_at": "2025-03-18",
    "updated_at": "2025-03-18 10:00:00"
  }
}
```

#### POST 请求 - 更新用户配置

参数:
- 请求体: JSON格式的用户配置数据

示例请求:
```
POST /service/user_config
Content-Type: application/json

{
  "phone": "13812345678",
  "jobsData": ["Java工程师", "前端工程师"],
  "keywordsData": {
    "Java工程师": {
      "include": ["Java", "Spring"],
      "exclude": ["实习"],
      "relation": "OR",
      "description": "Java工程师"
    }
  },
  "selectedJob": "Java工程师",
  "platform": "智联招聘",
  "version": "free"
}
```

示例响应:
```json
{
  "success": true,
  "message": "配置已保存"
}
```

### 2. 打招呼计数 API

- 接口: `/service/update_count`
- 方法: `POST`

参数:
- 请求体: JSON格式，包含 `phone` 字段

示例请求:
```
POST /service/update_count
Content-Type: application/json

{
  "phone": "13812345678"
}
```

示例响应:
```json
{
  "success": true,
  "message": "打招呼计数已更新",
  "data": {
    "version": "free",
    "greetCount": 1,
    "remainingQuota": 99,
    "expiryDate": "永久有效"
  }
}
```

### 3. 平台配置 API

- 接口: `/service/platform_config`
- 方法: `GET` 和 `POST`

#### GET 请求 - 获取平台配置

参数:
- `platform`: (可选) 平台名称，为空则获取所有平台配置

示例请求:
```
GET /service/platform_config?platform=智联招聘
```

示例响应:
```json
{
  "success": true,
  "message": "操作成功",
  "data": {
    "name": "智联招聘",
    "url": "https://www.zhaopin.com",
    "selector": {
      "jobList": ".job-list",
      "jobTitle": ".job-title"
    },
    "created_at": "2025-03-18 10:00:00",
    "updated_at": "2025-03-18 10:00:00"
  }
}
```

#### POST 请求 - 更新平台配置

参数:
- 请求体: JSON格式，包含 `platform` 和 `config` 字段

示例请求:
```
POST /service/platform_config
Content-Type: application/json

{
  "platform": "智联招聘",
  "config": {
    "name": "智联招聘",
    "url": "https://www.zhaopin.com",
    "selector": {
      "jobList": ".job-list",
      "jobTitle": ".job-title"
    }
  }
}
```

示例响应:
```json
{
  "success": true,
  "message": "平台配置已保存"
}
```

## 注意事项

1. 所有API响应均为JSON格式，包含 `success` 字段表示是否成功
2. 错误响应会包含 `error` 字段说明错误原因
3. 成功响应包含 `message` 字段和可选的 `data` 字段
4. 用户手机号必须是11位中国大陆手机号
5. 用户配置文件保存在 `config/users/` 目录下，以手机号为文件名
6. 平台配置文件保存在 `config/platforms/` 目录下，以平台名为文件名
7. 系统日志保存在 `logs/` 目录下，按日期命名 