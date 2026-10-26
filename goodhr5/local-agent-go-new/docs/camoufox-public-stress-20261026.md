# Camoufox 公开网站复现与升级验证

本文记录 2026-10-26 经用户确认的浏览器故障复现、版本对照和发布门槛，不使用招聘网站或招聘账号。

## 执行计划

1. 使用独立浏览器副本、全新 Profile 和公开网站测试；原有浏览器、Profile 和生产配置保持隔离。
2. 对照现场 152.0.4-beta.30 与官方候选 158.0-beta.40，每版至少三个公开页面、三次全新启动、每页三十轮（合计 270 轮）；观察前台、遮挡和复用情形，保留真实滚轮、点击、单步超时、耗时和截图证据。
3. 只使用 Playwright 标准 Page、Locator、鼠标和键盘，不注入网页脚本。加载失败与输入超时分别统计，不把偶发的网络失败当成引擎故障。
4. 分开检查官方浏览器稳定性与 GoodHR 的输入补丁、中文、字体、下载、书签和插件兼容。不能把纯浏览器 demo 通过当成客户端集成通过。
5. Mac Apple Silicon 本机实测；Intel 如使用 Rosetta，要明确标记。未取得 Windows 原生实测环境前，不更新 Windows 生产配置。
6. 只有对应平台稳定性和现有客户端兼容均通过后才上传 oss2。发布前备份配置，校验本地、服务器和公开下载 SHA-256，并保留可回退版本。
7. 基线未自然复现时，明确报告“本轮未复现”，不得宣称候选已修复现场故障。若兼容或稳定性不通过，保留原组件配置并记录证据。

## 当前状态

- 已确认 Windows 缺少可用的原生测试环境；本次 Windows 配置保持原值。
- 158.0-beta.40 Mac ARM64 官方包已下载，SHA-256 与 GitHub 资产摘要一致：`b8aba26d86f761627651b8e7f0d90ade022a32a3d750e0b68ff5830ebefd3aab`。
- 152 隔离副本已安装正式 v12，`PageHandler.js` 资源 SHA-256 为 `3be1a33d51ce5c83052f8c59bf43664959c108fa7075844b95129581637b0f2a`，与用户诊断中记录一致；该摘要不是整个 `omni.ja` 的摘要。
- 152 已累计完成 270 轮普通有窗口操作、540 次滚轮，没有滚轮超时。另有前台激活夹具失败的两次会话，未计入成功轮次，也不计作浏览器输入死锁。
- 158 的第一次启动 19,514ms，超过早期 demo 的 18 秒总预算；已把冷启动和初始化标签预算分开到 60 秒，真正鼠标与滚轮仍为 18 秒。不能把这次启动慢算作滚轮故障。
- 158 已完成同等 270 轮普通测试、540 次滚轮，没有输入超时；其现有客户端集成仍失败，因此没有把生产内核直接切到 158。

## 结果

### 现有客户端兼容

真实隔离归档探针逐项调用现有 Go 资源安装方法，152 全部通过。158 的鼠标资源、下载观察器及 152 中文包不兼容；没有通过伪造版本标记绕过检查。

正式 Worker 探针已准备独立 Profile、内置 UBO 和下载目录，并把启动与导航预算校准为 60 秒：152 在 11,238ms 成功；158 在 19,307ms 返回明确原因“浏览器原生下载通道没有准备好”，不是测试外层超时。

因此不能只把全局浏览器配置切到 158。当前配置没有按客户端版本分发能力，旧客户端也会拿到同一份新组件。

### 遮挡复现与修复

AppKit 隐藏/激活接口没有确认生效的测试明确标记为夹具失败，不计作卡死复现。改用按已验证测试 PID 限定、自动关闭的独立不透明遮挡窗口，不激活其他应用、不拦截鼠标、不读取网页或窗口标题。

旧版三次独立遮挡测试都出现 `mouse_wheel` 等待 18,002–18,003ms 后超时。正式 Worker 也记录到帧等待约 0.5、1、2、4、8、16 秒逐级增长，最终出现 `WORKER_UNRESPONSIVE`。全程没有 AI 请求，没有访问招聘页面。

修复采用 [Camoufox PR #861](https://github.com/daijro/camoufox/pull/861) 的同一浏览器内部设置：在 Chrome 窗口初始化时设置 `domWindow.browsingContext.forceAppWindowActive = true`，保持窗口被完全遮挡后的刷新。没有向网页注入脚本；测试滚动和点击仅使用标准鼠标、键盘能力。

精确单行 A/B 使用同一现场基线：原版启动与未遮挡滚动成功，遮挡后正式滚动超过 45 秒并需要回收测试进程；仅加入该设置的副本在同样遮挡后 2,396ms 完成，正常关闭。

### 正式候选验收

候选来自当前线上两个官方原始 ZIP，先验证原始 SHA-256，再只修改上述资源并重新打包。随后从候选 ZIP 重新解压，用现有 Go 方法安装资源、UBO 和语言包，使用这些实际安装副本运行长测。

| 验证对象 | 全新会话 | 公开页面 | 成功轮次 | 真实滚轮 | 滚轮 p95 / 最大 |
| --- | ---: | ---: | ---: | ---: | --- |
| 旧版普通测试 | 3 | 3 | 270 | 540 | 无输入超时 |
| 官方 158 普通测试 | 3 | 3 | 270 | 540 | 无输入超时 |
| Mac ARM64 r1，含每会话首站 60 秒遮挡 | 3 | 3 | 270 | 540 | 17ms / 35ms |
| Mac Intel r1，含每会话首站 60 秒遮挡 | 3 | 3 | 270 | 540 | 15ms / 90ms |

每次滚轮均用标准 Locator 边界变化确认正文实际移动，不把调用返回当作滚动成功。校验会话结束与子进程正常退出，关闭失败或缺少退出记录时不得显示通过。

ARM64 是本机原生运行。Intel 浏览器在同一 Apple Silicon Mac 上经 Rosetta 运行，驱动 Node 为 arm64；不冒充 Intel 真机或 x64 Node 整套安装验收。Windows 未取得原生环境，没有更新 Windows 配置。

两个候选均通过 Go 全部资源检查和正式 Worker 启动。实际发布包安装后又分别完成正式 Worker 的“未遮挡滚动→真实遮挡→再次滚动→正常清理”：ARM64 遮挡后 2,307ms 完成，Intel 经 Rosetta 为 2,970ms。已验证原始包与候选包各 397 个外层成员仅 `omni.ja` 改变；内层各 2436 个成员仅 `TargetRegistry.js` 改变，实际 diff 只有该设置和中文说明、来源注释。每架构 19 个 Mach-O 文件内容完全不变，没有夹入 Profile、备份或额外版本文件。

现场 legacy 升级路径的 v12 `PageHandler.js` 摘要为 `3be1a33d...`；从官方包全新安装 v12 后为 `af4494ef...`。两种路径分别保留证据，没有混用成同一资源。正式候选的窗口修复在现有 Go 资源安装后仍存在且只有一处。

其他校验：Go runtime 测试通过；Windows amd64、Mac amd64 编译通过；Node Worker 216 项通过、1 项跳过；统计闭环、进程身份、补丁幂等、双层 ZIP 内容/元数据保留及拒绝覆盖自检通过。最终脚本版本另外执行三站真实冒烟通过，确认新的监护与收尾路径有效。

### 发布记录

内核版本保持真实值 `152.0.4-beta.30`，修补版本通过独立文件名、新 SHA-256 和配置 note 标记为 GoodHR 窗口刷新修补 r1。已有用户需刷新后台并手动更新运行组件，再完全退出 Camoufox 后重开，已有窗口不会自动重新加载磁盘模块。

| 平台 | 新文件 | 字节数 | SHA-256 |
| --- | --- | ---: | --- |
| Mac Apple Silicon | `camoufox-152.0.4-beta.30-goodhr-window-r1-mac.arm64.zip` | 322472555 | `45b09f22dcd9666cd4e5347967ef9980845f571ea8e836bd6a1cb122c3bc43fa` |
| Mac Intel | `camoufox-152.0.4-beta.30-goodhr-window-r1-mac.x86_64.zip` | 330079159 | `2c075ea66c7322f2e1446452a84152de456f971898f58d1a43197c2c4d0bf3e7` |

文件已通过宝塔 MCP 上传到 oss2 根目录，服务器源文件和独立临时文件完整校验后原子发布。两次完整公网 GET 均返回 HTTP 200、字节数及 SHA-256 一致；旧文件保留。

生产配置于北京时间 2026-10-26 19:27:04 事务提交，仅修改 `runtime_components.camoufox.mac` 和 `darwin-x64` 的 URL、SHA-256、note，版本保持 152。剔除这两个资产后，前后 JSON 完全相等；Windows、Node、OCR、主程序版本及其他配置保持。

数据库整行并发保护标识从 `1026c3fc830354030acd8e0a51f1e9ec` 变为 `1c09753e86fd87fbf888333700c4b71b`。完整前后配置、中文带整行并发保护的 `rollback.private.sql` 和发布证据保存在服务器 `/opt/goodhr-release/camoufox-window-r1-20261026`，目录 700、私有文件 600，不在网站目录。

提交后重新完整核验新两包和旧 Mac ARM、Intel、Windows 三包的 SHA-256 均正确。仅精确删除本次两个 Upload 暂存源，保留工具索引、旧包和回退材料；没有重启服务。`/www` 可用空间从发布前 2,880,532,480 字节到收尾后 2,226,061,312 字节；服务器最终证据为该目录的 `release-final-summary.json`。

### 复现入口

- `worker/test/camoufox-public-stress.mjs`：标准 SDK 公开文档循环、动作 18 秒限时、启动/建页 60 秒限时、增量日志与截图。
- `worker/test/camoufox-window-cover.swift`：仅覆盖已验证测试 PID 的可见窗口，指定时长后退出。
- `worker/test/camoufox-worker-start-probe.mjs`：现有 ActionService 的启动和可选正式遮挡滚轮验证。
- `internal/runtime/component_upgrade_probe_test.go`：显式启用的隔离资源兼容检查，普通测试默认跳过。
- `scripts/patch-camoufox-window-rendering.py`：严格 152 版本/唯一锚点检查、原件备份、原子替换、可审计候选打包。

示例（工作目录为本地程序根目录，`CAMOUFOX_TEST_ROOT` 须为包含 `camoufox-upgrade` 路径组件的隔离目录；浏览器根目录版本文件按正式安装器准备）：

```sh
xcrun swiftc worker/test/camoufox-window-cover.swift -o "$CAMOUFOX_TEST_ROOT/camoufox-window-cover"
node worker/test/camoufox-public-stress.mjs \
  --install-dir "$CAMOUFOX_TEST_ROOT/browser" \
  --binary "$CAMOUFOX_TEST_ROOT/browser/Camoufox.app/Contents/MacOS/camoufox" \
  --output-dir "$CAMOUFOX_TEST_ROOT/results" --label compare \
  --sessions 3 --rounds 30 --site heroku --site playwright --site mdn \
  --cover-before-wheel-round 1 --cover-seconds 60 \
  --cover-binary "$CAMOUFOX_TEST_ROOT/camoufox-window-cover"
```

测试只使用 Heroku 公开长页面、Playwright 官方文档和 MDN 文档；没有招聘任务、候选人、真实消息或 AI 服务验收。公开站点复现与修复通过不冒充招聘业务实用验收。

完整原始输出保留在当前工作树 `output/playwright/camoufox-upgrade/results/`，其中失败的测试准备记录也保留，不覆盖为成功。
