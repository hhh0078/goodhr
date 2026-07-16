# GoodHR 用户流程邮件提醒：完整使用说明

本文档说明如何通过公共接口，按照用户当前停留的招聘流程节点，自动发送对应的教程邮件。

接口会读取 `users.flow_state`，按当前流程节点分组。不同节点使用不同邮件标题、教程内容和操作入口，所有邮件底部统一显示：

- 作者微信：`17607080935`
- GoodHR 官网：[https://goodhr5.58it.cn](https://goodhr5.58it.cn)

## 一、接口地址

```text
GET  /api/public/email-jobs/flow-reminder
POST /api/public/email-jobs/flow-reminder
```

线上完整地址：

```text
https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder
```

GET 适合直接配置到宝塔计划岗位运行、Linux Cron、Windows 计划岗位运行或其他定时岗位运行平台。

POST 适合参数比较多，或者由自己的程序调用。

## 二、接口鉴权

服务端必须配置环境变量：

```env
GOODHR_EMAIL_JOB_TOKEN=换成一段足够长的随机字符串
GOODHR_PUBLIC_BASE_URL=https://goodhr5.58it.cn
```

调用时建议通过请求头传递令牌：

```http
Authorization: Bearer YOUR_EMAIL_JOB_TOKEN
```

接口也兼容 URL 参数 `token`，但 URL 容易出现在访问日志里，不建议在线上使用：

```text
?token=YOUR_EMAIL_JOB_TOKEN
```

## 三、所有可用 flows

`flows` 表示只提醒当前停留在指定流程节点的用户。如果不传，默认处理所有尚未跑通核心流程的用户。

| flow 参数 | 用户当前停留位置 | 系统发送的教程 | 邮件主要入口 |
| --- | --- | --- | --- |
| `agent_detected` | 尚未检测到本地程序 | 下载、安装并启动 GoodHR 本地程序 | `/download` |
| `runtime_ready` | 本地程序已连接，但运行组件未准备好 | 安装 Node、浏览器等必要组件 | `/admin/agent-download` |
| `position_created` | 运行环境已就绪，但尚未创建岗位 | 创建岗位、填写要求和招呼语 | `/admin/positions` |
| `platform_login_verified` | 已有岗位运行，但招聘平台尚未确认登录 | 打开招聘平台并完成登录 | `/admin/positions` |
| `position_started` | 平台已经登录，但岗位运行未成功启动 | 检查组件、会员、AI 配置和岗位运行日志 | `/admin/positions` |
| `first_resume_processed` | 岗位运行启动过，但尚未成功处理第一份简历 | 检查候选人列表、筛选条件和扫描日志 | `/admin/positions` |
| `first_greet_success` | 已处理简历，但尚未成功打出第一次招呼 | 检查筛选分数、阈值、平台额度和失败日志 | `/admin/positions` |

已经完成全部流程的用户，其节点为 `completed`。该值不能传入 `flows`，系统也不会向这类用户发送流程教程邮件。

### 常用 flows 组合

只提醒还没完成本地环境准备的用户：

```text
agent_detected,runtime_ready
```

只提醒还没配置招聘业务的用户：

```text
position_created
```

只提醒岗位运行启动前卡住的用户：

```text
platform_login_verified,position_started
```

只提醒岗位运行已经跑过、但还没有结果的用户：

```text
first_resume_processed,first_greet_success
```

## 四、接口参数

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `flows` | 字符串或字符串数组 | 全部未完成节点 | GET 使用逗号分隔；POST 可以使用 JSON 数组 |
| `stalled_hours` | 整数 | `24` | 用户在当前流程至少停留多少小时，范围 `1` 到 `8760` |
| `created_day` | 日期字符串 | 不限制 | 只处理指定日期注册的用户，格式必须是 `YYYY-MM-DD` |
| `limit` | 整数 | `1000` | 本次允许匹配的最大用户数，范围 `1` 到 `5000` |
| `dry_run` | 布尔值 | `false` | 为 `true` 时只统计人数，不创建批次、不发送邮件 |

筛选条件之间是“并且”的关系。例如：

```text
flows=agent_detected&stalled_hours=48&created_day=2026-07-15
```

表示：只选择 `2026-07-15` 注册、当前仍停留在“未检测到本地程序”、并且已经停留至少 48 小时的用户。

### limit 的安全规则

如果实际匹配 1200 人，但 `limit=1000`，接口会拒绝整次发送，不会只发前 1000 人。

这是为了避免参数写错后误发大量邮件。建议每次正式发送前先执行 `dry_run=true`。

## 五、推荐使用流程

### 第一步：预览全部节点人数

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000&dry_run=true"
```

预览不会创建邮件批次，也不会发送邮件。

### 第二步：确认人数后正式发送

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000"
```

系统会按照用户当前节点拆成多个邮件批次。例如有三类用户，就会创建三个批次，每个批次使用自己的标题和教程。

## 六、GET 调用示例

### 1. 提醒所有停留超过 24 小时的未完成人员

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000"
```

### 2. 提醒还没安装本地程序的用户

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?flows=agent_detected&stalled_hours=12&limit=500"
```

### 3. 提醒本地程序或运行组件未准备好的用户

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?flows=agent_detected,runtime_ready&stalled_hours=12&limit=500"
```

### 4. 只处理指定日期注册的用户

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?created_day=2026-07-15&stalled_hours=24&limit=500"
```

### 5. 只预览启动岗位运行前卡住的人数

```bash
curl -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?flows=platform_login_verified,position_started&stalled_hours=6&limit=1000&dry_run=true"
```

## 七、POST 调用示例

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "flows": ["agent_detected", "runtime_ready"],
    "stalled_hours": 24,
    "created_day": "2026-07-15",
    "limit": 500,
    "dry_run": true
  }' \
  "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder"
```

POST JSON 和 URL 查询参数可以同时使用。相同参数同时出现时，URL 查询参数优先。

## 八、Windows PowerShell 示例

### 预览

```powershell
$headers = @{ Authorization = "Bearer YOUR_EMAIL_JOB_TOKEN" }
$url = "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000&dry_run=true"
Invoke-RestMethod -Method Get -Uri $url -Headers $headers
```

### 正式发送

```powershell
$headers = @{ Authorization = "Bearer YOUR_EMAIL_JOB_TOKEN" }
$url = "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000"
Invoke-RestMethod -Method Get -Uri $url -Headers $headers
```

## 九、返回结果

### dry_run 预览结果

```json
{
  "ok": true,
  "result": {
    "job": "flow-reminder",
    "batches": [],
    "skipped": [],
    "preview": {
      "agent_detected": 12,
      "runtime_ready": 5,
      "position_started": 3
    }
  }
}
```

`preview` 的键就是用户当前流程节点，值是匹配人数。

### 正式发送结果

```json
{
  "ok": true,
  "result": {
    "job": "flow-reminder",
    "batches": [
      {
        "id": "邮件批次ID",
        "subject": "GoodHR 本地程序还没启动",
        "target_summary": "流程提醒：agent_detected：停留至少24小时",
        "source_key": "flow-reminder:2026-07-15:agent_detected",
        "total_count": 12,
        "sent_count": 0,
        "failed_count": 0,
        "opened_count": 0
      }
    ],
    "skipped": [],
    "preview": {
      "agent_detected": 12
    }
  }
}
```

接口返回表示邮件批次已经创建。邮件会在后台异步发送，因此刚返回时 `sent_count` 通常还是 `0`。

可以在 GoodHR 超级管理员后台的邮件管理页面查看发送成功数、失败数和打开数。

## 十、重复发送规则

同一个自然日、同一个流程节点只允许创建一个自动提醒批次。

幂等键格式：

```text
flow-reminder:YYYY-MM-DD:flow
```

例如：

```text
flow-reminder:2026-07-15:agent_detected
```

如果当天再次触发相同节点，接口会把它放进 `skipped`，不会重复发送。

因此建议正式发送岗位运行每天运行一次，例如每天上午 9 点运行。`dry_run` 不创建批次，不受这条规则影响。

## 十一、定时岗位运行配置

### Linux Cron：每天上午 9 点发送

```cron
0 9 * * * curl -sS -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000" >> /var/log/goodhr-flow-reminder.log 2>&1
```

### Linux Cron：每天上午 8:50 预览，9:00 正式发送

```cron
50 8 * * * curl -sS -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000&dry_run=true" >> /var/log/goodhr-flow-reminder-preview.log 2>&1
0 9 * * * curl -sS -H "Authorization: Bearer YOUR_EMAIL_JOB_TOKEN" "https://goodhr5.58it.cn/api/public/email-jobs/flow-reminder?stalled_hours=24&limit=1000" >> /var/log/goodhr-flow-reminder.log 2>&1
```

Windows 可以将前面的 PowerShell 正式发送脚本保存为 `.ps1`，再通过“岗位运行计划程序”每天执行一次。

## 十二、邮件模板位置

八个流程教程模板位于：

```text
goodhr5/cloud/backend/templates/automatic_emails/
```

对应关系：

```text
agent_detected.html
runtime_ready.html
position_created.html
platform_login_verified.html
position_started.html
first_resume_processed.html
first_greet_success.html
```

统一作者信息模板：

```text
footer.html
```

邮件系统配置保存在：

```text
system_configs.config_key = system.email_recovery
```

数据库中如果为某个节点配置了自定义模板，会优先使用数据库模板；没有配置时使用代码目录中的默认模板。

## 十三、常见错误

### 401：token 不对，我先不敢发邮件

可能原因：

- 服务端没有配置 `GOODHR_EMAIL_JOB_TOKEN`
- 请求没有携带 `Authorization` 请求头
- 请求令牌和环境变量不一致

### unsupported flow

`flows` 中存在不支持的名称。请从本文“所有可用 flows”表格中复制，不要传中文名称或 `completed`。

### stalled_hours must be between 1 and 8760

`stalled_hours` 必须是 1 到 8760 之间的整数。

### created_day must use YYYY-MM-DD

注册日期格式不正确，正确示例：

```text
2026-07-15
```

### matched users exceeds limit

匹配人数超过安全上限。请先使用 `dry_run=true` 查看各节点人数，确认无误后再适当提高 `limit`。

### skipped 中出现 flow-reminder 日期键

说明当天该流程节点已经创建过提醒批次，系统为了避免重复打扰用户，跳过了本次发送。

## 十四、推荐的正式配置

日常建议每天上午 9 点执行一次：

```text
stalled_hours=24
limit=1000
flows 不传
dry_run=false
```

如果希望更温和，可以将 `stalled_hours` 调整为 `48` 或 `72`。这样只有在同一个流程停留两到三天的用户才会收到提醒。
