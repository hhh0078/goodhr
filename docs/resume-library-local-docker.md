<!-- 文件作用：记录简历库功能分支已运行的本地 Docker 验收入口、账号、插件、隔离方式与未完成的本地程序验收。 -->
# 简历库本地 Docker 测试

2026-10-10 已实际启动，功能分支为 `codex/resume-library-extension`。

## 直接开始测试

- 简历库：<http://127.0.0.1:5173/admin/resumes>
- 插件下载：<http://127.0.0.1:5173/download#browser-extension>
- 后台 API：<http://127.0.0.1:8084/health>
- 本地测试账号：`goodhr-resume-test@qq.com`
- 本地测试密码：`GoodHR-Test-2026`

登录页切换到“密码登录”。这是独立测试库的超级管理员账号，不是生产账号。首次打开可能出现公告、会员奖励和“请先安装本地程序”提示，关闭提示后可直接使用简历库。

已准备 1 份合成简历、2 个测试岗位、2 条独立备注和 1 条推荐记录。点击“演示候选人（本地测试）”查看头像、履历、备注和推荐；备注有发生时间、保存时间和作者，推荐有阶段、备注和推进历史。测试岗位关闭打招呼，也没有启动招聘任务。

可立即测试搜索、筛选、手动添加、重复提醒、更新、备注、岗位关联、推荐状态和插件下载。AI 批量解析、AI 摘要与插件自动配对需要独立测试 Agent。

## 浏览器插件

本地下载页的 Chrome 和 Firefox 都是 **1.0.2 本地测试包**。网页和 API 地址固定为上述本地地址，插件业务写入测试库；普通包默认连接生产，测试请使用名称含“本地测试”的包。

- Chrome 已解压目录：`D:\work\goodhr-plugin-local-test\chrome`
- Firefox 已解压目录：`D:\work\goodhr-plugin-local-test\firefox`
- ZIP、Firefox 临时 XPI 和 `checksums.json` 也在 `D:\work\goodhr-plugin-local-test`。

Chrome 在扩展管理页开启开发者模式，加载 Chrome 目录；Firefox 在 `about:debugging#/runtime/this-firefox` 临时加载 Firefox 目录中的 `manifest.json`。Firefox 未签名，重启浏览器后需重新临时加载。两种包均未发布商店。

使用同一个本地测试账号登录插件。未连接测试 Agent 时，插件手动登录、只读采集、查重、补手机号、添加/更新、备注和推荐仍可使用。招聘页面仍需浏览器登录相应平台；本次没有通过浏览器工具安装插件或写入真实候选人资料。

安装与操作说明见 [本地插件说明](../goodhr5/browser-extension/LOCAL-INSTALL.md)。本地后台下载配置已设为本地 ZIP URL 和对应 SHA256；没有覆盖生产数据库配置或正式插件资源。

| 本地下载文件 | 字节 | SHA256 |
| --- | ---: | --- |
| `goodhr-extension-chrome-local-1.0.2.zip` | 51071 | `a16f8e87615605a22d1c27384e5f217ae79b47f8b87f330d763047e920f0ec06` |
| `goodhr-extension-firefox-local-1.0.2.zip` | 51201 | `7dd71205ef7ec7e79eccb7f608947c5afe42b89741b193ab755feef462953534` |

## Docker 与数据

配置文件为 [docker-compose.resume-test.yml](../goodhr5/docker-compose.resume-test.yml)，项目名 `goodhr-resume-test`，含前端、后端、PostgreSQL 16 和 Redis 7。网页 5173、API 8084 仅监听本机；数据库和 Redis 未映射主机端口。现有 ERP 容器继续运行。

数据库、Redis、头像上传和日志使用独立命名卷，重新创建前后端容器不会丢失测试数据。本地新库实际执行 90 个迁移，最新为 `0086_candidate_notes.sql`。没有导入生产数据、SMTP 或支付凭据，邮件定时任务关闭。

如以后停止再启动，在功能工作目录执行：

```powershell
Set-Location 'C:\Users\Administrator\.codex\worktrees\resume-library\good-hr-ai-js\goodhr5'
docker compose -f docker-compose.resume-test.yml up -d --no-build
```

首次在另一个检出目录构建时，先 `npm --prefix browser-extension run build:local` 生成挂载的测试 ZIP，再 `docker compose -f docker-compose.resume-test.yml up -d --build`。已有依赖使用国内镜像构建，不需要为了启动另装依赖。

暂停这套环境使用 `docker compose -f docker-compose.resume-test.yml down`。不要追加 `-v`，它会删除测试数据卷。

## 测试 Agent 尚未启动

电脑上原有本地程序继续使用 43129，本地测试网页固定探测 **43139**，不会回退绑定原程序。测试插件默认也使用 43139，测试 Worker 使用 39891，测试数据目录独立。

测试程序已编译到 `D:\work\goodhr-resume-local-test\goodhr-resume-local-test.exe`。自动审批拒绝了启动独立测试 Agent 的命令，只返回 `blocked by policy`，未提供具体原因。因此没有绕过启动；43139 当前未监听。AI 解析、摘要、自动登录和配对尚未在这套环境中实测，AI/OCR 也需在测试程序中单独配置。

如需手动启动，在单独的 PowerShell 窗口执行以下命令，关闭该窗口即可结束这份测试程序；不要替换原程序：

```powershell
$env:GOODHR_CLOUD_API_BASE = 'http://127.0.0.1:8084'
$env:GOODHR_CONSOLE_URL = 'http://127.0.0.1:5173'
$env:GOODHR_AUTO_OPEN_CONSOLE = 'false'
$env:GOODHR_WORKER_PORT = '39891'
& 'D:\work\goodhr-resume-local-test\goodhr-resume-local-test.exe' -host '127.0.0.1' -port 43139 -data-dir 'C:\Users\Administrator\.codex\worktrees\resume-library\good-hr-ai-js\goodhr5\local-agent-go-new\agent_data\docker-test'
```

程序连上后，在本地网页登录测试账号，再验证配对。AI 请求会交给自行配置的服务商并可能计费；只验收简历库和插件入库时可以跳过 AI。

## 本轮运行证据

- 四个 Docker 服务实际运行，PostgreSQL/Redis 健康；后端 `/health` 和前端 `/download` 返回 200。
- 用真实本地 HTTP 接口创建测试账号、岗位、合成简历、两条独立备注和推荐；插件密码登录接口返回 200。
- 重建前后端容器后，数据库仍有 1 份简历、2 条备注、1 条推荐。
- 本地下载配置和两个 ZIP 的 HTTP 下载字节数、SHA256 一致。
- 实际网页完成密码登录、打开手动添加表单、查看示例详情及两条备注；截图在前端 `artifacts/docker-runtime-list.jpg` 和 `artifacts/docker-runtime-notes.jpg`，均为合成资料。
- 离线 UI 回归通过：搜索、手动新增、备注和推荐可用；AI 按钮拒绝离线提交，打开浏览器显示连接提示，模拟检查没有 Agent POST。
- 插件 63 项回归通过；新增的本地包验证生产源码/正式包字节未变、测试登录/保存地址、配对地址和权限隔离。
- 前端全量 19 项单测（含固定测试端口 4 项）、TypeScript 与 Docker 内生产构建通过。

运行镜像：

- 后端 `sha256:eecdc6e970cdf467bda472783aff933f8974d708cb5bb26e4ff1e2afc1b85c31`
- 前端 `sha256:40b08c3d61fe8ffc3c2be0372899fe46ed50e06a7fc60614f7808da1fa135b86`（已包含候选人属性统一换行、中文性别、创建时间及最新备注样式修正）

## 简历库排版调整

按用户验收反馈参考 HiMatch 列表：简历库正文、表头、按钮和姓名使用13px，基础信息等辅助文字12px，常规控件高度32px。该设置限定在简历库路由，列表、详情和弹框共用，继续继承会员配色。

“求职意向”不再独立占列，期望职位和“地区 · 薪资 · 求职状态”放入候选人第一列。手机号与姓名同行，标签、头像和完整资料入口保留；长文本省略并能查看完整提示。表格六列合计1290px，减少横向滚动。

此次 TypeScript、Docker 生产构建及已有头像/查询/备注/推荐/窄屏两项 UI 回归通过；已刷新真实 Docker 页面，实测表头13px、按钮32px，第一列包含原求职意向字段。截图在前端 `artifacts/resume-compact-runtime.jpg`。

后续反馈“最新备注仍太大”：实际读取发现按钮承载的正文和作者时间均为13px、600字重。已单独覆盖该列及内部Typography为12px、400字重、17.4px行高，保留两行摘要和完整内容提示。生产构建通过并更新前端容器；刷新实际页面确认两者都已生效，截图为 `artifacts/resume-note-small-runtime.jpg`。

后续反馈“鼠标放上去备注有阴影”：实际悬停时是按钮的灰色底色，已仅覆盖最新备注的悬停和按下样式为透明、无阴影，保留键盘焦点。Docker生产构建通过并更新前端；真实页面确认悬停状态下背景为透明、阴影为none、字号12px且普通字重；点击仍打开两条独立备注。截图为 `artifacts/resume-note-no-hover-runtime.jpg`。

候选人列再次调整：姓名下的基础信息、期望职位/地区/薪资/状态和标签统一为12px、400字重、相同辅助文字色；每个属性独立随空间换行，长属性可内部折行，取消原分行及标签底色。英文性别在列表和详情显示为中文，裸数字工作年限补“年经验”，不修改原存储值。“来源 / 入库”替换为只展示日期和时分的“创建时间”，来源及创建人保留在详情/筛选，表格总宽1270px。

此次中文性别、空值、未知值、零工作年限及坏日期检查通过；Docker生产构建和原头像/查询/详情备注推荐/窄屏两项UI回归通过。实际Docker页面核对全部10个属性为12px、400字重、同色，按列宽自动排为3行，并显示“男”“8年经验”和创建时分。截图为 `artifacts/resume-candidate-compact-runtime.jpg`、`artifacts/resume-created-time-runtime.jpg`。

真实招聘页面、插件实际安装、Firefox 正式签名、AI 实际服务商和生产部署的验收范围仍见 [完整验收记录](resume-library-upgrade-acceptance.md)。
