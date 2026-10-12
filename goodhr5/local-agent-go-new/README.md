<!-- 文件作用说明：向开发者介绍 local-agent-go-new 的重构目标、技术组成和文档阅读顺序。 -->

# GoodHR 新本地程序

本机联调、Windows/macOS 安装包、测试与正式环境发布的完整步骤，统一见 [运行、打包与部署说明](../DEPLOYMENT.md)。打包参数 `dev` 连接测试服务器，`build` 连接正式服务器；本机 localhost 联调使用源码启动方式。

`local-agent-go-new` 是按清晰边界重构后的 GoodHR 本地程序。它保留 Camoufox 及其反检测增强，由严格 TypeScript Worker 调用浏览器，Go 负责任务流程和平台适配。

## 技术组成

- Go：本地 HTTP 服务、任务流程、平台适配、本地数据、AI/OCR、运行组件和系统能力。
- TypeScript：Browser Worker 和强类型浏览器封装能力。
- Camoufox：浏览器运行和反检测增强。
- SQLite：保存本地任务状态、统一步骤日志、候选人动作摘要、自动回复去重摘要和下载结果。

## 唯一正式链路

```text
Go 主流程
  -> Go 平台适配
  -> Go Browser Client
  -> TypeScript 封装能力
  -> TypeScript 原子能力
  -> Camoufox
```

## 开发前阅读顺序

1. `AGENTS.md`
2. `docs/architecture.md`
3. `docs/directory-rules.md`
4. `docs/browser-worker-design.md`
5. `docs/runtime-flow.md`
6. `docs/migration-plan.md`
7. `docs/legacy-capability-matrix.md`

## 已实现主流程

```text
StartTask
  -> RunPreflightChecks
  -> 获取任务和 Profile 锁
  -> DispatchTaskFlow
      -> GreetingFlow
      -> AutoReplyFlow
  -> 保存最终状态并同步云端摘要
```

启动前检查按顺序覆盖请求、本地目录、登录、岗位、按任务需要检查会员、个人运行配置、本地平台配置、Profile、冲突、Node、Worker、Camoufox、SQLite、AI/OCR 和系统防睡眠。任务运行期间每批候选人和每轮自动回复还会重新检查登录态。

会员权限由云端统一返回：免费版可以运行关键词或 OCR 的基础打招呼任务；Plus 基础版和 Max 全能版都可以运行 AI 筛选与 AI 打招呼；只有 Max 全能版可以启动自动回复。本地程序必须同时校验 `active`、`allow_ai` 和 `allow_auto_reply`，不能只看会员名称或到期时间。

## 连接超时与闪退日志

Windows 日志目录：安装目录下的 `data\logs`，例如 `D:\GoodHRLocalAgent\data\logs`。旧版本日志在 `%APPDATA%\GoodHR\local-agent-new\logs`。

Windows 浏览器、OCR、账号登录数据、截图、下载和运行时临时文件统一放在安装目录的 `data` 中；`--data-dir` 和 `GOODHR_DATA_DIR` 仍可指定其他位置。升级首次启动时，会在旧程序和浏览器退出后复制旧 AppData 数据，逐文件校验通过才切换并移除旧副本；复制失败保留原数据，已有新数据时不会覆盖。迁移需要目标盘有足够空间，卸载保留 `data`。旧版下载到系统“下载”目录的文件不会自动搬动。Windows 安装器自身可能仍使用系统临时目录，其临时解包不属于 Agent 运行数据。

- `agent.log`：本地程序启动、Worker 进程编号、退出码、请求耗时和恢复结果。
- `worker.log`：每次请求编号、排队时间、鼠标移动/按下/松开、滚轮、截图及异常。
- `crash.log`：Go 意外崩溃时的运行时堆栈；正常运行时可以为空。分享前需检查并脱敏。
- 前两种日志会过滤凭据和 URL 查询参数，每份最多 10 MiB，各保留四份历史备份。清空页面岗位日志不会删除这些诊断文件。

遇到问题时，先记下发生时间；用相同 `request_id` 查找最后一个开始但没有完成的步骤。`WORKER_UNRESPONSIVE` 表示执行中的操作未按时返回：队列会阻止后续输入，本地程序回收 Worker，任务停止。重新开始任务时恢复浏览器，不自动重放结果未知的打招呼或消息。

Camoufox 原生鼠标资源会跳过拟人轨迹中连续重复的像素，并限制单次鼠标事件回执等待为 5 秒；修正仅作用于已识别的浏览器内部协议资源，不向招聘页面注入脚本。未知资源版本保持原文件并记录原因。原始资源备份保存在对应 `omni.ja.goodhr-original`。

## 本地接口

- `POST /api/v1/tasks/start`：启动主动打招呼或自动回复。
- `POST /api/v1/tasks/stop`：安全停止任务。
- `GET /api/v1/tasks/{task_id}`：读取任务状态。
- `GET /api/v1/runtime/status`：查看 Node 和 Worker 状态。
- `POST /api/v1/runtime/ensure`：启动 Worker。
- `POST /api/v1/runtime/install`：按云端清单异步安装 Node 22+、Camoufox 和可选 OCR，支持 SHA256、安全解压和失败回滚。
- `GET /api/v1/diagnostics`：检查目录、端口、运行组件和 Profile 锁。
- `GET|POST /api/v1/app-update/*`：读取程序更新进度并启动安装包更新。
- `POST /api/v1/page/open`：唯一浏览器打开入口，统一启动或复用 Profile、打开页面，并支持 `new_tab=true` 新增标签页和旧版 `new_page=true` 兼容字段。
- `GET /api/v1/browser/status`、`POST /api/v1/browser/stop`：读取或关闭当前浏览器，不提供第二个启动入口。
- `GET /api/v1/downloads`：查看 Worker 监听到的下载成功、失败和处理中记录。
- `GET /api/v1/downloads/history`：查看 SQLite 中已结束的下载历史；旧版 `/api/v1/local/downloads` 路径继续可用。
- `POST /api/v1/downloads/configure`：切换后续下载目录。
- `POST /api/v1/downloads/clear`：清空下载记录，不删除文件。
- `POST /api/v1/files/open|reveal`：打开下载文件或在 Finder 中定位，路径必须是绝对路径，并位于默认目录或 Worker 已成功使用过的下载目录。

Worker 的完整协议见 `contracts/browser-api.md`。

页面打开会优先复用同域名、同目标路径的已有标签页，避免刷新掉用户手动设置的筛选条件；传入 `new_tab=true` 时会始终新增并切换到一个标签页，登录页即使带有回跳参数也不会被误复用。真实滚轮使用元素位置或截图变化验证结果，不读取页面内部滚动状态，也不向招聘页面注入或执行 JavaScript。

Camoufox（Firefox 反检测内核）负责指纹伪装：同一个持久化 Profile 会在 Profile 目录维护一份稳定指纹文件（`goodhr-fingerprint.json`），同一账号指纹不漂移。启用 Camoufox 原生拟人鼠标移动（`humanize: 0.8`），由浏览器生成轨迹并等待移动完成。Worker 只发送一次目标坐标，不叠加 Playwright 的直线分步；继续保留随机安全落点、点击停顿、输入焦点检查和异常松键。

语言与时区固定为 zh-CN 和 Asia/Shanghai：用户与招聘平台均在中国大陆，指纹身份保持一致即可，不启用 Camoufox 的 geoip（按代理 IP 动态匹配时区、语言和经纬度），因此也不需要 GeoLite2 数据库、不产生额外的联网探测；启动参数中的 `geoip` 字段为旧版兼容保留，传入后不生效。

正常启动时，程序会读取 GoodHR 公开发布配置，只下载已标记完成 Mozilla 签名的 Firefox XPI，校验 SHA256、固定扩展编号和包内版本后安全解压到 `extensionsDir/goodhr-resume-assistant`。同版本同摘要会跳过下载；网络、摘要或包内容异常时保留旧版并继续启动 Agent。程序只扫描 `extensionsDir` 的一级子目录和有效 `manifest.json`，并通过 Camoufox 官方 `addons` 参数加载；自定义解压扩展仍可放在该目录的其他一级子目录中。

UBO（uBlock Origin）1.75.0 已随 Go 主程序内置，首次打开浏览器不再联网下载该扩展。启动前逐文件校验，文件缺失或损坏时从内置包离线恢复，旧目录保留为 `UBO-backup-*`，其他扩展与登录数据不受影响。Worker 明确排除默认 UBO 下载，扩展异常会记录具体目录。扩展自身的过滤规则更新保持原行为。UBO 作者为 Raymond Hill & contributors，原始扩展及其 GPL-3.0 和第三方许可证完整保留；对应源码见 https://github.com/gorhill/uBlock/tree/1.75.0 。

每个 Profile 准备时会生成只拥有书签权限的 Firefox 扩展，浏览器启动后通过自身书签接口补齐 GoodHR、BOSS直聘、猎聘猎头端、猎聘和智联招聘入口。已有 Firefox 书签会保留，旧 `Default/Bookmarks` 中的 Chromium 书签及文件夹会按原层级迁移，重复启动不重复添加，原文件不删除。书签栏默认在所有页面显示；这些入口只用于手动导航，不参与平台自动化，也不向网页注入脚本。升级后请退出并重新启动本地程序和浏览器。

书签栏兼容修正：程序会覆盖 Camoufox 主题的强制隐藏规则、把组件策略中的 `DisplayBookmarksToolbar` 改为 `always`，并将浏览器布局资源里的书签组件固定在书签栏。修改 `browser/omni.ja` 前保留 `omni.ja.goodhr-original` 原始备份，其他压缩资源原样保留；旧 Profile 的其他工具栏布局不变，只恢复误放到地址栏的书签组件，恢复保存后清除临时布局覆盖。用户仍可通过浏览器菜单显示或隐藏书签栏。

浏览器使用 Firefox 原生下载管理，已有标签页、新标签页和用户手动下载都正常进入浏览器自己的下载面板及“我的足迹 → 下载”。浏览器负责真实文件名、保存位置、重名处理、历史记录与文件打开/定位；Worker 不再调用 `download.saveAs`，只读取浏览器的本机会话状态快照。Go 每秒同步一次成功或失败终态，保存 SQLite 记录；首次成功时继续显示十秒下载提示。文件接口会检查真实路径并阻止软链接越过下载目录；切换目录必须等待浏览器确认，只影响后续下载。GoodHR 的清空记录不删除文件，也不清除浏览器原生历史。Worker 不反向调用 Go 业务接口。

运行组件会在首次启动和组件安装完成后修正 Camoufox 自身的下载通道，并保留 `omni.ja.goodhr-original` 原始备份；启动参数 `-purgecaches` 刷新浏览器代码缓存，让已有 Profile 立即使用新资源，不清除登录状态、书签或下载历史。下载观察模块不访问网页 DOM，不向招聘页面注入脚本。组件版本不匹配时明确报错，不强行修改未知资源。安装新包后先退出本地程序和浏览器，再启动生效；旧版由 GoodHR 保存的文件仍保留在原目录，其历史仍可在 GoodHR 中查看，不会补写成 Firefox 原生下载历史。

四个平台都按 `entry.go`、`position.go`、`candidate.go`、`detail.go`、`greet.go`、`followup.go`、`reply.go` 和 `runtime.go` 分责。每个平台目录中的 `config.json` 是带中文属性说明的本地运行配置和能力核对表，通过 `go:embed` 随 Go 程序一起编译，也是平台 URL、行为和选择器的唯一来源；本地程序不会向云端读取或合并平台配置。配置中的 `pending_selectors` 表示旧版也没有可确认的配置，自动回复缺少真实页面地址或选择器时会在启动前明确拦截。

本地程序启动后会打开前端控制台并附加实际 `local_port`。源码开发默认打开 `http://localhost:5173/admin/`，`build` 包打开 `https://goodhr.58it.cn/admin/`，`dev` 包打开 `https://goodhr-test.ai.58it.cn/admin/`；云端 API 地址和前端地址互不混用。新版不托管、不下载第二份本地静态控制台。
如果固定端口上已经是一个健康的 GoodHR 本地程序，新进程只会复用该实例、打开现有控制台后退出；不会结束不明端口占用者。
本地 Go 服务默认监听 `127.0.0.1:43129`，启动参数传入其他端口时，控制台仍以 URL 中的实际 `local_port` 为准；前端没有收到端口时也默认探测 `43129`。内部 Browser Worker 继续使用 `39881`。

AI 客户端支持普通 JSON 和 SSE 流式响应，遇到 429、5xx 或临时网络错误最多重试三次；`detail_mode=ai` 会把真实滚轮生成的分段截图交给多模态模型。OCR 使用常驻 JSON 行协议，运行组件压缩包多一层目录时也会递归找到可执行文件。

Go 与 Worker 步骤日志统一写入岗位日志，每个岗位只保留最近 1000 条。主程序和 Worker 文件日志按 10MB 轮转，各保留 3 份历史文件。本地任务、候选人、会话、下载和步骤摘要保留 90 天；OCR/AI 临时截图读取后立即删除，启动时还会清理崩溃遗留截图、组件压缩包和旧更新包。

程序启动时会把上次异常退出遗留的 `running` 任务改成明确失败状态。任务运行期间每 30 秒检测一次时间断层，电脑休眠后恢复且断层超过 2 分钟时会停止当前任务，避免在页面状态已经变化后继续点击。

## 构建与运行

依赖镜像确认后执行：

```bash
./scripts/prepare-runtime.sh
./scripts/build.sh
./bin/goodhr-local-agent
```

`prepare-runtime.sh` 会安装锁定的 Worker 依赖，并通过官方 `npx camoufox-js fetch` 下载 Camoufox 反检测浏览器（开发环境走 GitHub Releases；生产环境由 Go 安装器按云端清单从自建 OSS 下载并做 SHA256 校验）。Go 不会改为普通浏览器，也不会绕过 Camoufox。

开发环境可以执行：

```bash
./scripts/run-dev.sh
```

本地程序版本号默认是 `6`。需要临时覆盖时执行：

```bash
./scripts/run-dev.sh --version 6.1
```

下载同步在 Browser Worker 第一次启动前会安静等待，不会把正常的未启动状态打印成错误；Worker 曾经连接成功后如果意外断开，仍会记录提醒。

macOS 正式包只在 Mac 上生成：

```bash
./scripts/package-release.sh 6 build
```

Windows x64 正式包在 Windows 上生成，不能和 macOS 包混用：

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
.\scripts\package-windows.ps1 -Version 6 build
```

该脚本使用带 BOM 的 UTF-8 编码，兼容 Windows 自带的 Windows PowerShell 5.1。命令行也可以使用 `scripts\package-windows.bat 6 build`。版本号换成实际发布版本即可。打包机需要 Go、Node.js 22+ 和 Inno Setup 6；脚本默认使用 npm 国内镜像 `https://registry.npmmirror.com` 和 Go 国内镜像 `https://goproxy.cn,direct`。

中文界面首次启动：程序内置匹配 Firefox 152 的中文语言包。新建浏览器配置会先隐藏启动并正常关闭一次，保存语言包注册结果后才显示窗口；已有配置已启用中文时跳过这一步。初始化不打开招聘网站，也不会循环启动。首次启动会多等待几秒。

浏览器窗口交互：本地程序启动和组件下载完成后，自动在 Camoufox 的 `chrome.css` 末尾补齐标签点击修正，避免标签栏被当成窗口拖动区。Windows 浏览器使用原生标题栏（`browser.tabs.inTitlebar=0`），绕过字体伪装导致的窗口按钮方块；网页字体白名单和已有账号指纹保持原样。安装新包后退出并重新启动本地程序及浏览器，修正才会生效。

浏览器宽高自动管理：页面始终使用 `viewport: null`，旧接口传入的 `viewport_width`、`viewport_height` 不再生效。启动时只清除 Camoufox 配置中的窗口和页面尺寸覆盖，账号原始指纹文件不改写，其他指纹与插件保留。浏览器自行决定窗口大小，已有配置可沿用浏览器保存的窗口状态，手动调整窗口时页面随之变化。

Windows 脚本会独立编译 `windows/amd64` GUI 主程序，打包 Worker 生产依赖，同时生成以下文件：

- `release\goodhr-local-agent-v6-windows-x64.zip`
- `release\GoodHR-Local-Agent-Setup-v6-windows-x64.exe`

每次构建会先删除本版本的旧安装器，避免遗留文件与 Windows 图标缓存混淆。安装器首次安装默认按 `D → B → E → C` 选择存在的磁盘，优先安装到 `D:\GoodHRLocalAgent`；C 盘兜底使用当前用户的 `%LOCALAPPDATA%\Programs\GoodHRLocalAgent`。升级时保留用户上一次选定的目录。

升级前安装器会结束旧主程序，以及命令行指向当前安装目录的残留 Node Worker，避免 `better-sqlite3` 原生文件被占用导致安装回滚；不会结束其他项目的 Node 进程。

Logo 源文件统一保存在 `assets\goodhr-logo-transparent-512.png`。打包脚本会自动调用 `scripts\generate-windows-icon.ps1`，从这个 PNG 生成 `assets\goodhr-logo.ico`，用于安装器文件图标、卸载项和快捷方式；安装向导直接使用源 PNG。以后修改 Logo，只需替换该 PNG 再执行打包命令。

Windows 运行时使用各平台对应的 Node、Camoufox、PowerShell 提示音、资源管理器和 `SetThreadExecutionState` 防睡眠；macOS 则使用对应的 Node、Camoufox、`afplay`、Finder 和 `caffeinate`。

两种发布包都会包含带版本号的 Go 主程序、Worker 编译产物和 Worker 生产依赖，输出到已忽略提交的 `release/` 目录。运行组件和本地程序更新只接受 HTTPS 地址与完整 SHA256，校验不通过不会安装。

## 猎聘开聊弹层恢复

猎聘会保留已经隐藏的推广弹框节点。推广识别限定当前可见弹框，并兼容 IM 推广层的实际结构，避免隐藏旧节点导致漏检或匹配多个关闭按钮。

点击“立即沟通”后没有出现职位弹框时，程序会留出两秒观察延迟推广层，依次清理推广弹框、职位弹框、候选人聊天框及右侧联系人列表，再重新定位同一候选人，最多重试一次。重试前出现“继续沟通”则直接停止重复点击；发送确认按钮不在重试范围内。发送成功后同样观察两秒，推广层优先按 Esc，验证仍未关闭时再尝试关闭按钮。失败清理使用独立限时上下文，停止任务后也会尝试关闭遗留弹层。

此修复的 Windows 安装包构建命令：

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
.\scripts\package-windows.ps1 -Version 6-hliepin-dialog build
```

产物为 `release\GoodHR-Local-Agent-Setup-v6-hliepin-dialog-windows-x64.exe`。平台配置编译在 Go 程序中，需安装新包并重新启动本地程序后生效。

## 窄窗口滚动与点击验证修复

浏览器继续自动管理窗口宽高，不强制放大窗口。公共滚动每一步重新读取目标位置，先调整纵向、再用真实横向滚轮把右侧按钮带入窗口；已经到位的方向不再滚动。部分可见的候选人卡片按相交区域计算距离；点击只要求有安全落点，避免贴边联系人行无法完整进入窗口时一直滚动。最后一步缩短距离以避免过冲。连续三次无进展或达到次数上限会明确停止，不把“执行过滚轮”当成“已到位”。

点击后的隐藏验证同时接受“元素已移除”和“元素仍在 DOM 中但已隐藏”，避免职位弹框正常关闭后反而报发送失败。

猎聘索要电话、微信、简历会等待可见确认按钮，并验证确认框消失；确认失败时停止后续索要和消息输入，清理确认弹框再收尾。Worker 的页面操作串行执行，点击和输入使用 `finally` 松开鼠标，定位移动前只释放本程序已按下的遗留左键，避免无效松键造成浏览器等待。点击和输入前等待目标连续三次位置稳定；输入前使用标准 `:focus` 查询确认焦点，未聚焦时禁止发送全选和输入按键。全程不向招聘页面注入脚本。

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
.\scripts\package-windows.ps1 -Version 6-natural-speed build
```

对应安装器：`release\GoodHR-Local-Agent-Setup-v6-natural-speed-windows-x64.exe`。本版本启用原生拟人鼠标轨迹，更新后必须重新启动浏览器会话才能生效；不固定窗口宽高。原生轨迹最长时间调整为约 0.8 秒；输入使用 Playwright 标准键盘能力，默认字符间隔随机 50～100 毫秒，词间保留额外 50～120 毫秒短暂停顿。轨迹和用时会随距离及随机路径变化。

## 稳定性诊断版本打包

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
.\scripts\package-windows.ps1 -Version 6-stability build
```

安装器输出到 `release\GoodHR-Local-Agent-Setup-v6-stability-windows-x64.exe`。打包会重新安装 Worker 依赖，请先结束本机正在执行的招聘任务。安装后重新启动本地程序和浏览器，才能加载新的 Go 流程、Worker 与浏览器原生鼠标修正。

已发送的招呼会在后续索要资料、发送补充消息和关闭弹层之前记账，避免收尾失败导致成功次数丢失。聊天弹层清理有独立的 45 秒预算；浏览器会话失效时立即结束本次任务，不自动重放发送操作。原生鼠标事件回执最多等待 5 秒，超时会记录 `GOODHR_MOUSE_ACK_TIMEOUT`，便于区分原生鼠标异常和整个 Worker 无响应。

原生鼠标移动回执超时后，在预算足够且请求未取消时，只重新读取目标并补试一次移动，不重放点击或发送。部分可见的元素至少露出 8 像素（小元素需完整露出），窗口底部只有一条边的目标会继续用真实滚轮移入安全区域。

## 核心原则

- 不整体复制旧目录。
- 先固定边界和协议，再迁移能力。
- Node 原子能力不对外暴露。
- Go 只调用 TypeScript 封装能力。
- TypeScript Worker 不包含任何招聘平台逻辑。
- 招聘页面完全禁止 JavaScript 注入，只使用标准 Locator、鼠标、键盘、真实滚轮和截图。
- 所有选择器操作使用统一选择器类型。
- 主流程使用平铺步骤，不隐藏深层调用链。

## 正式和测试环境打包（6.1.1 起）

打包命令必须以 `build`（正式）或 `dev`（测试）指定环境，不再使用旧的无环境命令。

```powershell
.\scripts\package-windows.ps1 -Version 6.1.1 build
.\scripts\package-windows.ps1 -Version 6.1.1 dev
.\scripts\package-windows.bat 6.1.1 build
```

正式域名为 `https://goodhr.58it.cn`，测试域名为 `https://goodhr-test.ai.58it.cn`。测试包文件名额外包含 `-test`，包内 `build-info.json` 可核对环境。macOS 使用 `zsh scripts/package-release.sh 6.1.1 build`，测试版末尾改为 `dev`。

前后端 Docker 部署、数据库备份及 OSS 发布说明见 [分环境部署文档](../DEPLOYMENT.md)。
