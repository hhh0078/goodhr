# GoodHR 5 完整流程文档

> 2026-05-19 | 按代码逐模块追踪

---

## 目录
1. [登录认证](#1-登录认证)
2. [登录后初始化](#2-登录后初始化)
3. [平台账号管理（Profile/Cookie）](#3-平台账号管理)
4. [岗位模板管理](#4-岗位模板管理)
5. [任务创建](#5-任务创建)
6. [任务执行（核心链路）](#6-任务执行)
7. [任务列表与日志](#7-任务列表与日志)
8. [候选人查看与管理](#8-候选人查看与管理)
9. [边界场景分析](#9-边界场景)

---

## 1. 登录认证

### 1.1 账号来源

**没有注册流程**。用户直接输入邮箱，系统发送验证码，验证通过即登录。

- Go 后端 `POST /api/auth/send-code`：接收邮箱 → 生成 4 位数字码 → 存入 AuthStore（Redis/内存，TTL=5 分钟）→ 调用 Mailer 发信。
- **SMTP 未配置时**：使用 DevMailer，不真实发邮件，在响应中返回 `debug_code`，前端自动填入验证码框。
- PostgreSQL `users` 表存在但 **Login 流程未写入**——用户通过验证码后只创建 session，不创建 users 记录。

### 1.2 发送验证码

```
前端 LoginForm.vue：
  用户输入邮箱 → 点击 [ 发送验证码 ]
  ├─ useAuth.sendCode()
  │   └─ POST {CLOUD_API_BASE}/api/auth/send-code
  │      Body: { "email": "user@example.com" }
  │
  └─► Go auth.go SendCode():
       ├─ normalizeEmail() → 转小写 + net/mail 校验格式
       │   失败 → 400 "invalid email"
       ├─ randomDigits(4) → 4 位数字（0-9 各一位）
       │   失败 → 500
       ├─ AuthStore.SaveLoginCode(email, code, 5分钟)
       ├─ Mailer.SendLoginCode(email, code)
       │   ├─ 配置 SMTP → SMTPMailer 通过 163 SMTP 发送
       │   │   失败 → 500 "failed to send code"
       │   └─ 未配置 SMTP → DevMailer 不发邮件，只记录日志
       └─ 返回 JSON:
          { "ok": true, "email": "...", "expires_in": 300 }
          开发模式额外: { "debug_code": "1234" }
```

**边界**：
- 邮箱格式错误（如无@、特殊字符）→ 400
- 同一邮箱可多次发送，每次覆盖之前的验证码（5 分钟 TTL 重置）
- 邮件发送本身无频率限制（依赖 SMTP 服务商的限制）

### 1.3 登录

```
前端 LoginForm.vue：
  用户输入验证码 → 点击 [ 登录 ]（或回车）
  ├─ useAuth.login()
  │   └─ POST {CLOUD_API_BASE}/api/auth/login
  │      Body: { "email": "...", "code": "1234" }
  │
  └─► Go auth.go Login():
       ├─ normalizeEmail() 校验
       ├─ code 长度 != 4 → 400 "invalid code"
       ├─ ConsumeLoginCode(email, code) → 一次性验证
       │   ├─ 匹配 → 删除验证码（无法重用）
       │   └─ 不匹配/过期 → 401 "code is invalid or expired"
       ├─ randomToken() → "gh5_" + hex(32字节) = 约 70 字符
       ├─ SaveSession(token, {Email, CreatedAt}, TTL=2小时)
       └─ 返回:
          { "ok": true,
            "access_token": "gh5_abc...",
            "token_type": "Bearer",
            "expires_in": 7200,
            "user": { "email": "user@example.com" }  ← 无 id!
          }
```

**返回后前端操作**：
```js
token = data.access_token
localStorage.setItem("goodhr5_access_token", token)
user = data.user   // { email: "..." }
```

**边界**：
- 验证码过期（>5 分钟）→ 401，需重新发送
- 验证码错误 → 401
- 验证码重复使用 → 第二次使用已被 Consume 删除，401
- 同时登录多设备 → 每个设备独立 session，互不影响

---

## 2. 登录后初始化

### 2.1 触发时机

登录成功通过 `watch(user)` 触发（App.vue），或页面刷新通过 `onMounted → loadCurrentUser()` 触发。

### 2.2 恢复登录态（刷新页面）

```
onMounted → auth.loadCurrentUser():
  │
  ├─ 读取 localStorage("goodhr5_access_token")
  │   无 token → 直接停留在登录页
  │
  ├─ GET {CLOUD_API_BASE}/api/auth/me
  │   Headers: Authorization: Bearer {token}
  │
  └─► Go auth.go Me():
       ├─ SessionFromRequest(r):
       │   ├─ 从 Header 提取 "Bearer ..."
       │   ├─ AuthStore.GetSession(token)
       │   │   ├─ Redis: GET session:{token}
       │   │   └─ 内存: map 查询
       │   └─ 返回 Session{Email, CreatedAt}
       ├─ 失败 → 401 "session is invalid or expired"
       └─ 成功 → 返回 { user: { email: "..." } }
           │
           └─► 前端:
               user = data.user
               ├─ agent.detect(user, token)    ← 重新探测本地 Agent
               ├─ positions.load()             ← 重新加载岗位模板
               └─ tasks.load()                 ← 重新加载任务列表
```

### 2.3 探测本地 Agent

```
agent.detect():
  ├─ checking=true, status="检测中"
  ├─ 遍历 LOCAL_PORTS: 55271, 55272, ..., 55279
  │   for each port:
  │     GET http://127.0.0.1:{port}/health (cache: no-store)
  │     ├─ 失败（端口不可达/超时）→ 继续下一个端口
  │     └─ 成功 → 读取响应:
  │         { ok, name: "GoodHR 5 Local Agent",
  │           version, port, machine_id, local_db }
  │         ├─ status="已连接 (端口 {port})"
  │         ├─ baseUrl="http://127.0.0.1:{port}"   ← 保存供后续任务执行使用
  │         ├─ 不绑定云端账号
  │         ├─ 不连接云端 WebSocket
  │         ├─ 不向 Local Agent 传登录 token
  │         └─ return（不再尝试后续端口）
  │
  └─ 所有端口都不可达 → status="未检测到本地程序", baseUrl=""
```

**边界**：
- 多个 Local Agent 实例在不同端口 → 只连接第一个探测到的
- user.id 为 undefined → Local Agent 绑定空 user_id
- 网络请求超时（默认浏览器超时）→ catch 后继续下一个端口

### 2.4 加载数据

```
positions.load():
  GET /api/positions → Go PositionStore.ListPositions(email)
  返回 { ok, positions: [...] }   ← 数组中每个对象含 keywords/exclude_keywords/...

tasks.load():
  GET /api/tasks → Go TaskStore.ListTasks(email)
  返回 { ok, tasks: [...] }      ← 每个任务含 platform_id/mode/status/statistics
```

---

## 3. 平台账号管理

### 3.1 账号来源链路

平台账号（profile）是整个流程的关键基础设施。用户必须先在平台上登录（有有效 cookie），才能让 Local Agent 模拟操作。

**完整链路**：

```
招聘平台（Boss/智联等）
  用户手动扫码登录 → 浏览器保存 cookie
      ↓
  用户打开 Chrome 扩展或 Local Agent 所在的浏览器
  cookie 已存在于 Chrome profile 中
      ↓
Local Agent 创建 profile 记录（管理 cookie 对应的 Chrome profile 目录）
  POST /api/v1/profiles
  { platform_id: "boss", display_name: "我的Boss账号" }
  → 写入 agent_data/profiles.json
      ↓
前端创建云端账号映射（连接本地 profile 和云端用户）
  POST /api/platform-accounts/create
  { platform_id: "boss", display_name: "我的Boss账号", local_profile_id: "<profile-uuid>" }
  → 写入 platform_accounts 表
      ↓
任务创建时前端读取账号列表
  GET /api/platform-accounts → 返回 [{id, display_name, platform_id, local_profile_id}]
      ↓
任务执行时使用 local_profile_id 找到对应的 Chrome profile 目录
  BrowserManager.start(user_data_dir: local_profile_id)
  → CloakBrowser 使用该 profile 目录，自动加载已保存的 cookie
```

### 3.2 当前缺失环节

**⚠️ 前端没有 profile 管理 UI**：
- Local Agent 有 `profiles` API（list/create/delete）
- Go 云端有 `platform-accounts` API（list/create/delete）
- 但前端 `TaskCreator.vue` 的账号下拉来自云端 API，而云端数据没有创建入口
- **结果：账号下拉永远为空，任务创建按钮永远灰色**

### 3.3 所需的完整流程（待实现）

```
前端 "账号管理" 面板：
  1. 探测到 Local Agent 后 → GET /api/v1/profiles 读取本地已有 profile
  2. 用户点击"新增" → POST /api/v1/profiles { platform_id, display_name }
     → 返回 profile { id, platform_id, display_name }
  3. 同步到云端 → POST /api/platform-accounts/create
     { platform_id, display_name, local_profile_id }
  4. 账号列表同时从本地和云端显示
```

### 3.4 平台登录状态

- CloakBrowser 使用持久化 profile 目录，cookie 自动保存
- Profile 目录对应 Chrome 的 `user_data_dir`
- 如果 cookie 过期，打开平台页面时停留在登录页
- **当前未检测**：任务执行不检查是否已登录，直接提取候选人会返回空
- goodhrpy 中有 `check_login_status()` 和 `wait_for_login()` 功能，但未迁移

---

## 4. 岗位模板管理

### 4.1 数据模型

```
Position {
  id: UUID
  user_email: string
  name: "Java高级开发"
  keywords: ["Java", "Spring", "Boot"]
  exclude_keywords: ["实习", "应届"]
  description: "岗位要求：3年以上经验..."
  greet_message: "您好，我是HR..."
  is_and_mode: false   // false=OR(任一匹配), true=AND(全部匹配)
  created_at, updated_at
}
```

### 4.2 UI 操作

```
前端 PositionManager.vue：
  ├─ 加载: onMounted → GET /api/positions
  │   └─► Go PositionService.List()
  │       └─ auth.SessionFromRequest() 校验登录
  │       └─ PositionStore.ListPositions(email)
  │           返回 [{id, name, keywords, ...}]
  │
  ├─ 创建: 填写表单 → 点击 [ 保存模板 ]
  │   └─ POST /api/positions
  │      Body: { name, keywords:[], exclude_keywords:[], description, greet_message, is_and_mode }
  │      └─► Go PositionService Collection(POST)
  │          └─ PositionStore.SavePosition()
  │              内存/PostgreSQL INSERT
  │
  ├─ 编辑: 点击 [ 编辑 ] → 回填表单 → 修改 → [ 更新模板 ]
  │   └─ POST /api/positions (相同端点，有 id 则 UPDATE)
  │
  └─ 删除: 点击 [ 删除 ]
      └─ DELETE /api/positions/{id}
```

**边界**：
- 空关键词（keywords: []）→ 筛选时按概率通过（clickFrequency 控制）
- 删除不存在的岗位 → 404

---

## 5. 任务创建

### 5.1 表单字段

```
平台:   下拉 boss / zhaopin / liepin
账号:   下拉（从 GET /api/platform-accounts 读取，按 platform 过滤）
岗位模板: 下拉（从 GET /api/positions 读取，可选）
筛选模式: keyword / ai
匹配上限: 数字输入（默认 20）
```

### 5.2 创建流程

```
点击 [ 创建任务 ]
  ├─ useTasks.create()
  │   ├─ 校验 platformAccountId 非空
  │   └─ POST /api/tasks
  │      Body: { platform_id, platform_account_id, position_id, mode, match_limit }
  │      └─► Go task.Create():
  │          ├─ toTask() 校验:
  │          │   platform_id 非空 / platform_account_id 非空 / mode 默认 "keyword"
  │          │   match_limit >= 0
  │          ├─ PlatformAccountBelongsToUser() 校验账号归属
  │          │   └─ 不匹配 → 400 "platform account not found"
  │          └─ TaskStore.CreateTask()
  │              └─ 返回 task:
  │                 { id, platform_id, mode, match_limit,
  │                   status: "created",
  │                   scanned_count:0, greeted_count:0,
  │                   skipped_count:0, failed_count:0,
  │                   local_task_id: "" }
  │
  └─► 前端: 刷新任务列表，清除表单 positionId
```

**返回的 task 对象**：
```json
{
  "id": "uuid",
  "platform_id": "boss",
  "platform_account_id": "uuid",
  "platform_account_name": "我的Boss账号",
  "position_id": "uuid (或空)",
  "position_name": "Java高级开发 (或空)",
  "mode": "keyword",
  "match_limit": 20,
  "status": "created",
  "scanned_count": 0, "greeted_count": 0,
  "skipped_count": 0, "failed_count": 0,
  "local_task_id": "",
  "created_at": "2026-05-19T..."
}
```

---

## 6. 任务执行

### 6.1 触发

```
任务列表中点击 [ 运行 ]
  ├─ useTasks.execute(taskId)
  │   └─ POST /api/tasks/{taskId}/run
  │      Body: { agent_base_url: "http://127.0.0.1:55271" }
  │      └─► Go task.Run():
  │          ├─ SessionFromRequest() → 校验登录
  │          ├─ TaskByID(email, taskId) → 校验归属
  │          ├─ agent_base_url 非空 → 400
  │          ├─ go executeTask(task, agentBaseURL) ← goroutine 异步!
  │          └─ 立即返回 { ok, status: "running" }
  │
  └─► 前端: 刷新任务列表
```

### 6.2 executeTask 内部详细流程

```
goroutine: executeTask(task, agentBaseURL)
│
├─ 写日志: "任务 {id} 开始执行"
│
├─ 读取平台配置
│   systemConfigs.Get("platform." + task.PlatformID)
│   └─► PostgreSQL/system_configs WHERE config_key='platform.boss'
│       返回 JSON:
│       { id, name, domain, pages:[{url,title}],
│         card:{container, card:[...], name, basicInfo:[...], education:[...], university, description},
│         actions:{greetBtn:[...], continueBtn:[...], ...},
│         detail:{openTarget:[...], closeBtn:[...], messageTip, messageItem},
│         extras:[{selector,label},...],
│         behavior:{needsDetailPage, supportsPaging, ...} }
│       │
│       └─ ParsePlatformConfig(json) → PlatformConfig 结构体
│
├─ 读取岗位信息（如果 task.PositionID 非空）
│   positionStore.PositionByID(email, task.PositionID)
│   └─► PostgreSQL/positions JOIN users
│       返回 Position{Name, Keywords, ExcludeKeywords, Description, GreetMessage, ...}
│   → 构建 position map: { name, keywords, exclude }
│
├─ 创建 TaskExecutor 实例
│   NewTaskExecutor(task, platformCfg, position, agentBaseURL, logFunc)
│   ├─ 判断 mode:
│   │   ├─ ai: filter=nil（由 callAI 处理）
│   │   └─ keyword: NewKeywordFilter(keywords, exclude, isAndMode, 7)
│   └─ httpClient: Timeout=120s
│
└─ executor.Run(ctx)
    │
    ├─── 步骤 1/5: 启动浏览器 ─────────────────────────────
    │   POST {agentBaseURL}/api/v1/browser/start
    │   Body: { persistent:true,
    │           user_data_dir: task.PlatformAccountID,
    │           headless:false,
    │           humanize:true }
    │   │
    │   └─► Local Agent BrowserManager.start()
    │       ├─ 已有实例在运行 → 先 stop() 再 start()
    │       ├─ persistent=true + user_data_dir
    │       │   → create_persistent_browser():
    │       │       清理 profile 锁文件（SingletonLock/Socket/Cookie）
    │       │       终止残留 Chromium 进程
    │       │       launch_persistent_context_async(CloakBrowser)
    │       │       → 返回 BrowserContext（内含已保存的 cookie）
    │       └─ 返回 { ok, status: "started" }
    │
    │   失败: executor.Run() 返回 error, 写日志 "启动浏览器失败"
    │
    ├─── 步骤 2/5: 打开平台推荐页 ──────────────────────────
    │   POST {agentBaseURL}/api/v1/page/open
    │   Body: { url: "https://www.zhipin.com/web/chat/recommend" }
    │   │
    │   └─► Local Agent:
    │       ├─ BrowserManager.new_page("default") → 创建新标签页
    │       ├─ navigate_to_page(page, url, timeout=30000)
    │       │   └─ page.goto(url, waitUntil="domcontentloaded")
    │       │       ├─ 成功 → 等待 2s → 返回 { ok, url, title }
    │       │       └─ 失败（超时）→ 500 "页面导航失败"
    │       │
    │       └─ ⚠️ 不检查页面是否在登录页！
    │           如果 cookie 过期，Boss 会跳转到登录页
    │           但 navigate_to_page 只检查 goto 是否成功
    │           （登录页也是合法页面，goto 会成功）
    │
    │   失败: executor.Run() 返回 error
    │
    ├─── 步骤 3/5: 滚动加载候选人列表 ──────────────────────
    │   POST {agentBaseURL}/api/v1/page/scroll
    │   Body: { scroll_delay_min:3, scroll_delay_max:8,
    │           max_scrolls: matchLimit/5 (最少 5) }
    │   │
    │   └─► Local Agent scroll_to_load():
    │       for i in range(max_scrolls):
    │         ├─ 随机滚动 250-450px
    │         │   └─ human_scroll(): 分 3-8 步完成
    │         ├─ 检查 stop_condition（当前无）
    │         └─ random_delay(3-8 秒)
    │       → 返回 { ok }
    │
    ├─── 步骤 4/5: 查询候选人卡片并逐卡提取 ──────────────────
    │   POST {agentBaseURL}/api/v1/page/find-elements
    │   Body: { element: { target_classes: [["candidate-card-wrap"]] },
    │           visible_only: true }
    │   │
    │   └─► Local Agent:
    │       通过统一元素定位协议定位候选人卡片：
    │         先查主页面，再查所有 iframe
    │         按 parent_classes / target_classes 交叉查询
    │         → 返回 { items:[{ref:"el_xxx", index:0}, ...], count:N }
    │
    │   POST {agentBaseURL}/api/v1/page/extract-fields
    │   Body: { element_ref: "el_xxx",
    │           fields: [{name:{...}}, {basic_info:{...}}, {education:{...}}] }
    │   │
    │   └─► Local Agent:
    │       在指定 card ref 范围内提取字段文本
    │       → 返回 { fields:{name:"张三", basic_info:"...", education:"..."} }
    │       │
    │       返回 { ok, candidates: [...], count: N }
    │
    │   失败: 返回 error，executor.Run() 返回 error
    │
    └─── 步骤 5/5: 逐候选人筛选 + 打招呼 ──────────────────
        for i, candidate := range candidates:
          ├─ 检查 ctx.Done()（context 取消检查）
          │
          ├─ 构建候选人文本: candidateText()
          │   遍历 candidate 所有字段值，用空格拼接
          │
          ├─ 根据 mode 筛选:
          │   │
          │   ├─ [keyword 模式]
          │   │   filter.Filter(text):
          │   │   ├─ 检查排除词: strings.Contains(textLower, excludeWord)
          │   │   │   命中任一 → 返回 { Passed:false, Reason:"命中排除词" }
          │   │   │   → 日志 "候选人 N 被筛选跳过: 命中排除词" → continue
          │   │   ├─ 无关键词（len(keywords)==0）:
          │   │   │   rand.Float64()*10 < clickFrequency → 通过
          │   │   │   否则 → 跳过
          │   │   ├─ 有关键词:
          │   │   │   逐关键词查找 → 匹配列表 matched[]
          │   │   │   ├─ AND 模式: 全部匹配 → 通过; 否则 → 跳过
          │   │   │   └─ OR 模式: 任一匹配 → 通过; 否则 → 跳过
          │   │   │
          │   │   └─ 通过 → "候选人 N 通过筛选: 关键词部分匹配"
          │   │
          │   └─ [AI 模式]
          │       callAI(jobDesc, candidateText):
          │       │
          │       ├─ 构建 Prompt（默认模板）:
          │       │   "你是一个资深的HR专家..." + 岗位要求 + 候选人信息
          │       ├─ POST https://ai.58it.cn/v1/chat/completions
          │       │   Body: { model:"gpt-5.1-chat",
          │       │           messages:[{role:"user",content:prompt}],
          │       │           temperature:0.3 }
          │       │   Headers: Content-Type: application/json
          │       │   ⚠️ 缺少 Authorization: Bearer {api_key}
          │       │
          │       ├─ 解析返回:
          │       │   期望 content 是纯 JSON: {"isok":true,"msg":"符合基本要求"}
          │       │   ├─ 直接 Unmarshal 成功 → 使用
          │       │   └─ 失败 → 截取 content 中第一个 { } 片段再解析
          │       │       └─ 仍失败 → 返回 error
          │       │
          │       └─ 返回 AIDecision{IsOK, Msg}
          │
          │   通过 → 继续
          │   不通过/失败 → 日志 "候选人 N AI 筛选跳过/AI 筛选失败" → continue
          │
          └─ 打招呼:
              clickGreet():
              │
              ├─ 获取平台配置的第一个 greet 按钮: platformCfg.Actions.GreetBtn[0]
              ├─ POST {agentBaseURL}/api/v1/page/click
              │   Body: { selector: ".btn.btn-greet",
              │           timeout: 10000,
              │           delay_before: 1.0 }
              │   │
              │   └─► Local Agent wait_and_click():
              │       ├─ 等待元素出现（timeout=10s）
              │       ├─ 随机延迟 0-0.5s
              │       ├─ locator.click()
              │       └─ 返回 { ok, clicked: true/false }
              │
              └─ 失败 → 不阻塞，继续下一个候选人
```

### 6.3 任务结束

```
Run() 返回:
  ├─ err != nil → WriteLog("任务执行失败: {err}")
  └─ err == nil → WriteLog("任务执行完成")

defer e.stopBrowser() → 关闭浏览器进程
```

---

## 7. 任务列表与日志

### 7.1 加载任务列表

```
前端 onMounted / 登录后 / 手动刷新:
  GET /api/tasks → Go task.List()
  └─► 返回 tasks 数组
```

### 7.2 展开日志

```
前端 TaskList.vue → 点击 [ 展开日志 ]
  ├─ useTasks.toggleLogs(taskId)
  │   ├─ 如果已展开 → 收起
  │   └─ GET /api/tasks/{taskId}/logs
  │       └─► Go TaskLogService.List()
  │           ├─ SessionFromRequest() 校验
  │           └─ TaskLogStore.ListTaskLogs(taskId)
  │               返回 [{id, level, message, created_at}, ...]
  │
  └─► 前端渲染日志列表
       ├─ level="info" → 绿色文本
       ├─ level="error" → 红色文本
       └─ level="warn" → 黄色文本
```

**日志来源**：executeTask 中每步操作调用 `WriteLog(taskID, level, message)`，写入 TaskLogStore（内存或 PostgreSQL）。

---

## 8. 候选人查看与管理

### 8.1 查看候选人

```
前端 TaskList.vue → 点击 [ 查看候选人 ]
  ├─ useTasks.toggleCandidates(task)
  │   ├─ 构造 localId = task.local_task_id || task.id
  │   ├─ loadCandidates(task, localId):
  │   │   ├─ 构造 agent = { port: 从 agentBaseUrl 解析 }
  │   │   ├─ POST /api/v1/tasks/init
  │   │   │   Body: { task_id, cloud_user_id, platform_id,
  │   │   │           platform_account_id, position_snapshot }
  │   │   │   └─► Local Agent: 创建 agent_data/tasks/{task_id}/ 目录
  │   │   │       ├─ 目录已存在 → 幂等，不覆盖
  │   │   │       └─ 目录不存在 → 创建目录 + candidates.json + screenshots/ + ocr/
  │   │   │
  │   │   └─ GET /api/v1/tasks/{taskId}/candidates
  │   │       └─► Local Agent: 读取 candidates.json
  │   │           返回 { ok, data: { items: [{id, name, raw_text, ...}], position_snapshot: {...} } }
  │   │
  │   └─ 存储候选人和岗位快照到 taskCandidates[localId]
  │
  └─► 前端渲染候选人卡片列表 + 岗位模板快照
```

### 8.2 删除候选人

```
点击候选人卡片的 [ 删除 ]
  ├─ useTasks.removeCandidate(task, candidate)
  │   └─ DELETE /api/v1/tasks/{taskId}/candidates/{candidateId}
  │       └─► Local Agent: 从 candidates.json 中移除该候选人
  │
  └─► 前端立即从本地列表中移除（乐观更新）
```

**边界**：
- 查看候选人需要 Local Agent 在线（agentBaseUrl 非空）
- Local Agent 离线时 → candidateError 显示错误信息
- position_snapshot 是任务创建时从岗位模板同步过来的快照，不因模板被修改而变化
- 候选人的详情文本（detail_text）来自 OCR 识别结果，只有执行过截图+OCR 的候选人才有

---

## 9. 边界场景分析

### 9.1 浏览器刷新

```
刷新 → Vue 挂载 → onMounted:
  ├─ loadCurrentUser()
  │   ├─ 有 token + 有效 → 恢复登录态 → 重新检测 Agent → 加载数据
  │   └─ 无 token → 登录页
  │
  └─ 正在执行的任务不受影响（Go goroutine 独立运行）
     任务执行结束后日志继续写入，用户重新展开可看到完整日志
```

### 9.2 登录过期

```
前端带着过期 token 调用任何 API:
  └─► Go SessionFromRequest() → 401
      └─► 前端:
          ├─ loadCurrentUser() 中 → logout() → 回到登录页
          └─ 其他 API 调用 → 前端显示 error 信息
```

### 9.3 Local Agent 掉线

```
任务执行中 Local Agent 进程崩溃:
  ├─ executor.post() → "请求 Local Agent 失败: connection refused"
  ├─ callAI/post 返回 error
  ├─ Run() 返回 error
  └─ WriteLog("任务执行失败: ...")

前端视角:
  ├─ AgentPanel 状态变为 "未检测到本地程序"
  ├─ 已展开的日志会保留（已写入 TaskLogStore）
  └─ 任务列表中的 status 不会更新（因为 executeTask 未更新它）
```

### 9.4 多个任务同时运行

```
用户创建任务 A（boss）→ 点击运行 → goroutine 1: executeTask(A)
用户创建任务 B（zhaopin）→ 点击运行 → goroutine 2: executeTask(B)
│
├─ 两个 goroutine 共享同一个 TaskLogStore → 日志按 task.id 隔离
├─ 每个 goroutine 调用不同的页面（不同平台）→ 不同标签页
├─ 但共用同一个 BrowserManager → 共享浏览器实例
│   ├─ 任务 A 先 start → 浏览器启动
│   ├─ 任务 B 再 start → BrowserManager.is_running=true
│   │   └─ 返回 { status: "already_running" } → 不重新启动
│   └─ 两个任务各自创建 new_page → 不同的 Page 实例
└─ 可能冲突: 两个任务同时操作不同页面，互不影响
```

### 9.5 平台网站改版/选择器失效

```
平台更新了 DOM 选择器:
  ├─ extractCandidates() → JS 中 querySelectorAll 返回空
  │   └─ 返回 { candidates: [] }
  ├─ processCandidates() → for 循环不执行
  └─ 任务"成功完成"（没有跳过错误，因为没有候选人可处理）

  ⚠️ 这是静默失败——任务显示成功但没有提取任何人
  ⚠️ 需要检查: 候选人数量为 0 时应记录 warning 日志
```

### 9.6 网络故障

```
Go 后端 ↔ Local Agent 通信:
  ├─ browser/start 失败 → Run() 返回 error
  ├─ page/open 失败 → Run() 返回 error
  ├─ page/scroll 失败 → Run() 返回 error
  ├─ page/find-elements 或 page/extract-fields 失败 → Run() 返回 error
  └─ page/click 失败 → 跳过当前候选人，继续下一个

Go 后端 ↔ AI API 通信:
  └─ AI API 不可达 → callAI 返回 error → 跳过当前候选人
      不会中断整个任务，继续处理下一个
```
