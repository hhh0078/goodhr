# CloakBrowser → Camoufox 迁移改动方案

> 本文档作用：基于代码库现状调研与 Camoufox 能力调研，给出 GoodHR 浏览器内核从 CloakBrowser（魔改 Chromium）替换为 Camoufox（魔改 Firefox）的完整改动方案，供评审确认后实施。
>
> 配套文档：《camoufox-功能介绍.md》《camoufox-使用指南.md》
>
> 编写时间：2026-09-16。分支：feature/camoufox-migration

---

## 一、背景与目标

### 为什么要换

1. **授权风险**：CloakBrowser 为闭源二进制，再分发存在 OEM 授权隐患（原 CLOAKBROWSER.md 已记录）。Camoufox 开源（camoufox-js 为 MPL-2.0），无再分发授权风险。
2. **反检测强度**：Camoufox 在 C++ 实现层拦截指纹，自动化运行在 Juggler 沙箱，配合 BrowserForge 真实分布指纹生成，理论隐身能力强于现有方案，且与本项目"零 JS 注入"红线天然一致。
3. **能力增强**：协议级 WebRTC IP 伪装、按 IP 自动匹配时区/语言/经纬度、字体指纹防护、真实指纹预设库，都是现有方案没有或较弱的。

### 目标

- 本地程序（local-agent-go-new）的浏览器内核替换为 Camoufox，Go 主流程与 Worker 的 HTTP 协议**保持不变**。
- 全平台流程（Boss直聘/猎聘/智联）在新内核上完成回归。
- 浏览器二进制的下载分发切换到 Camoufox（OSS 自建镜像）。

---

## 二、两代内核核心差异（改造依据）

| 差异点 | CloakBrowser 现状 | Camoufox 方案 | 改造动作 |
|--------|-------------------|---------------|----------|
| 内核/协议 | Chromium + CDP | Firefox + Juggler | Worker 内启动调用方式重写；页面操作 API 不变 |
| npm 依赖 | `cloakbrowser@0.5.2` | `camoufox-js@0.12.0`（Node>=22） | 换依赖 |
| playwright-core | 1.61.1 | **必须 < 1.61.0**（camoufox-js peer 依赖） | 降级到 1.60.x |
| 指纹指定 | 启动参数 `--fingerprint=sha256种子` | `fingerprint` 选项传完整 BrowserForge 指纹对象 | 指纹策略重写（见决策点 D4） |
| 持久化登录 | `launchPersistentContext({userDataDir})` | `Camoufox({ user_data_dir })` 返回 BrowserContext | 启动封装重写 |
| humanize | npm 包内置（鼠标/键盘/滚动） | 仅内置鼠标移动 humanize | 先关掉，沿用 Worker 自研类人原语（决策点 D3） |
| 二进制定位 | `CLOAKBROWSER_BINARY_PATH` + 目录约定（Chromium.app/chrome.exe） | `executable_path` 启动选项 / `CAMOUFOX_INSTALL_DIR` | Go 探测逻辑重写 |
| 分发渠道 | 自建 OSS（oss.58it.cn）zip + SHA256 | 官方走 GitHub Releases（国内慢） | 保留自建 OSS 通道，换成 Camoufox 镜像包 |
| Profile 数据 | Chromium profile | Firefox profile（**互不兼容**） | 用户需重新登录平台（决策点 D6） |

---

## 三、改动范围总览

改动集中在**本地程序链路**（属于主流程通用组件，不是某个招聘平台的平台逻辑）：

```
云端 backend（下载清单配置）          ← 改配置数据
云端 frontend（管理端展示/文案）      ← 改展示
本地 Go（运行时安装/定位/预检）       ← 改安装与探测
Worker TS（浏览器启动封装/指纹）      ← 改动最重
平台流程 Go（boss/liepin/zhaopin）   ← 原则上不改，只做回归验证
```

旧版链路（goodhr5/local-agent-go + worker-node）**本期不迁移**，见决策点 D1。

---

## 四、详细改动清单

### 第 1 层：Worker（TypeScript，改动最重）

位置：`goodhr5/local-agent-go-new/worker/`

| # | 文件 | 改动内容 |
|---|------|----------|
| 1 | `package.json` | 删除 `cloakbrowser@0.5.2`，新增 `camoufox-js@0.12.0`；`playwright-core` 从 1.61.1 **降级到 ~1.60.0**；新增 `fingerprint-generator`（camoufox-js 的 peer 相关依赖，用于生成/恢复指纹） |
| 2 | `src/browser/session/browser-session.ts` | 重写启动封装：`launchPersistentContext({userDataDir,...})` → `Camoufox({ user_data_dir, ... })`（返回 BrowserContext）；`launch(options)` + `newContext` → `Camoufox(options)`（返回 Browser）。参数映射：`humanize`（初版固定 false）、`geoip`（有代理时 true）、`proxy`、`locale`、`headless`、`executable_path`（指向 Go 下发的 Camoufox 二进制）、`fingerprint`（从账号指纹文件读入）。删除 `extensionPaths` 类 CloakBrowser 专有参数 |
| 3 | `src/browser/session/fingerprint.ts` | 策略重写：现为 `sha256(userDataDir)` → `--fingerprint=种子`。改为「指纹文件」方案：账号 profile 目录下保存 `fingerprint.json`（BrowserForge 指纹对象）；不存在则用 `fingerprint-generator` 生成一次并落盘；每次启动读文件传入 `fingerprint` 选项。文件损坏/缺失时重新生成（不报错阻断，符合边界处理要求） |
| 4 | `src/browser/actions/action-service.ts` | `binaryInfo()`（来自 cloakbrowser 包）与 `runtimeStatus` 中 `CLOAKBROWSER_BINARY_PATH` 的读取：改为读取 `CAMOUFOX_BINARY_PATH`（Go 注入）或按 `CAMOUFOX_INSTALL_DIR` 约定探测；返回的二进制版本信息来源改为解析 Camoufox 目录/版本文件 |
| 5 | 启动参数清理 | 全局搜索 `--fingerprint`、`--test-type` 等 Chromium 专有参数并移除；`downloadsPath` 移到 `launchOptions()` 摊开后的 Playwright 原生选项里 |

**不变的部分（重要）**：HTTP 协议（contracts/browser-api.md）、24 个 `/api/v1/*` 路由、鼠标/键盘/滚轮/截图等自研原语（`primitives/mouse.ts`、`actions/click.ts`、`input.ts`、`scroll.ts`、`screenshot.ts`）全部保持 Playwright 标准 API，Firefox 上继续可用，零改动。

### 第 2 层：本地 Go（运行时安装与探测）

位置：`goodhr5/local-agent-go-new/internal/`

| # | 文件 | 改动内容 |
|---|------|----------|
| 6 | `runtime/types.go` | Manifest 组件：`cloakbrowser` → `camoufox`（版本/URL/SHA256 结构不变） |
| 7 | `runtime/manager.go` | `CloakBrowserPath()` → `CamoufoxPath()`：探测路径改为 Camoufox 的目录结构（macOS `Camoufox.app/Contents/MacOS/camoufox`、Windows `camoufox/camoufox.exe`、Linux `camoufox/camoufox`，**具体结构以 PoC 实测为准**）；`node_modules/cloakbrowser` 存在性检查改为 `node_modules/camoufox-js`；环境变量注入 `CLOAKBROWSER_BINARY_PATH` → `CAMOUFOX_BINARY_PATH` |
| 8 | `runtime/installer.go` | 下载源切换：按新 Manifest 从 OSS 镜像下载 Camoufox 压缩包，SHA256 校验 + 解压逻辑复用；需处理 Firefox 系解压后的目录结构差异 |
| 9 | `config/config.go` | 环境变量重命名与默认值（`CAMOUFOX_BINARY_PATH`、安装目录 `runtime/camoufox/`），保留旧变量读取一个过渡版本以便平滑升级（读到旧的也能工作，但优先新的） |
| 10 | `flow/preflight/preflight.go` | `check_cloakbrowser` 步骤改名 `check_camoufox`，检测逻辑对接新 `CamoufoxPath()`；错误文案按 GoodHR 文案风格改写，例如："Camoufox 还没装好，我这就带你去下载页，大概一两分钟" |
| 11 | `contracts/browser-api.md` | 协议文档补充：二进制路径环境变量、runtimeStatus 字段语义变化（对 Go 侧字段名保持兼容） |

### 第 3 层：云端

| # | 文件 | 改动内容 |
|---|------|----------|
| 12 | backend `internal/httpapi/system_config_store.go` | `system.onboarding_config` 下载清单：cloakbrowser 条目 → camoufox 条目（新 URL/版本/SHA256）；保留读取旧字段兼容，使已装旧组件的存量用户能被引导升级 |
| 13 | 新增数据库迁移 | 新的组件清单默认值迁移 SQL（如 0075_camoufox_runtime_components.sql，每个字段带中文备注） |
| 14 | frontend `lib/admin-runtime.ts` | 组件别名映射 `cloakbrowser` → `camoufox`（旧别名保留映射展示，兼容存量数据） |
| 15 | frontend `app/admin/agent-download/page.tsx` | 下载页文案与组件名替换（GoodHR 文案风格） |

### 第 4 层：杂项与文档

| # | 位置 | 改动内容 |
|---|------|----------|
| 16 | `scripts/prepare-runtime.sh` | `npm exec -- cloakbrowser install` → `npx camoufox-js fetch`（开发机流程；生产仍走 Go 安装器 + OSS） |
| 17 | 旧版 `worker-node/src/profile-process.js`（如仍维护） | Windows 残留进程清理的进程名匹配增加 `camoufox`；本期不动旧版则记录到待办 |
| 18 | `internal/browser/CLOAKBROWSER.md`（旧版目录） | 归档说明：内核已切换，保留作历史参考 |
| 19 | 根 `AGENTS.md` 浏览器规则 | "只能使用 CloakBrowser/Playwright 标准能力" → "只能使用 Camoufox/Playwright 标准能力"，零注入红线原文保留 |
| 20 | 实验性 GoController（旧版 go_*.go 全套） | 依赖 CDP，Firefox 无 CDP，**直接标记弃用**（决策点 D7） |

---

## 五、关键决策点（需要你确认）

| # | 决策点 | 我的建议 | 备选 |
|---|--------|----------|------|
| D1 | 迁移范围 | 只迁移新版 `local-agent-go-new` 链路；旧版 worker-node 不动，等新版稳定后整体下线 | 旧版同步迁移（工作量翻倍，不建议） |
| D2 | playwright-core 降级 | 降到 1.60.x（camoufox-js 要求 <1.61.0） | 无（硬约束，只能降级） |
| D3 | humanize 开关 | 初版关闭（`humanize: false`），沿用 Worker 自研类人原语，行为可控；稳定后再 A/B 对比开启 | 直接开启（鼠标移动会多一层人类化，轨迹耗时不可控） |
| D4 | 指纹策略 | 每个账号 profile 下持久化 `fingerprint.json`（BrowserForge 指纹），实现"一号一指纹"，语义对齐现在的"一号一种子" | 沿用数字种子（Camoufox 不支持，不可行） |
| D5 | 分发方案 | Go 安装器继续从自建 OSS 下载 Camoufox 镜像 zip（官方包转存 + SHA256），Worker 用 `executable_path` 指定路径；不走官方 `npx camoufox-js fetch`（GitHub 国内不可控） | 让用户端直连 GitHub（国内体验差，不建议） |
| D6 | 登录态迁移 | Chromium profile 无法迁移到 Firefox，升级后用户需重新登录招聘平台；云端已有 Cookie 存储，可在首次启动时尝试用 Playwright `context.addCookies()` 重注入（localStorage 无法迁移，部分平台可能仍需重新登录）。需在升级提示里讲清楚 | 只提示重新登录（最简单，体验略差） |
| D7 | 实验性 GoController | 标记弃用/删除（Firefox 无 CDP，维护无意义） | 改造为 launchServer + firefox.connect（投入产出比低） |

---

## 六、实施阶段

> 每阶段独立可验证、可提交。阶段 0 是闸门：PoC 不通过则整个方案重议。

### 阶段 0：PoC 技术验证（不进主代码）

1. 开发机安装 `camoufox-js@0.12.0` + `playwright-core@1.60.x`，`npx camoufox-js fetch` 下载浏览器；
2. 跑通：持久化启动（`user_data_dir`）→ 打开 Boss直聘登录页 → 标准点击/输入/滚轮滚动/截图 → 关闭重开验证登录态保留；
3. 记录：macOS/Windows 解压后的真实目录结构（供 Go 探测逻辑）、`executable_path` 自定义路径是否生效、GeoIP 数据库在 JS 版的获取方式；
4. 用 browserscan/creepjs 检测隐身效果；
5. 验证 `fingerprint-generator` 生成的指纹对象可以持久化复用（二次启动指纹一致）。

### 阶段 1：Worker 切换内核

改动清单 #1-#5。验收：Worker 本地手测启动/页面操作/截图/下载全通过；协议响应格式与现版本完全一致。

### 阶段 2：本地 Go 适配

改动清单 #6-#11。验收：全新机器走完"下载 Node → 下载 Camoufox → 预检 → 跑任务"全流程。

### 阶段 3：云端配置与管理端

改动清单 #12-#15。验收：管理端可见 camoufox 组件与版本；存量 agent 能收到新组件清单。

### 阶段 4：平台流程回归（工作量集中区）

- Boss直聘：登录/搜索职位/打招呼/查详情/自动回复 全流程；
- 猎聘：登录/手动筛选/详情/沟通 全流程；
- 智联：登录/继续沟通/候选人定位 全流程；
- 重点回归：滚动安全边距（截图拼接依赖 viewport 行为，Firefox 下需复测）、下载简历监控、声音提示、窗口聚焦逻辑（Go 侧聚焦 Chromium 窗口的代码要适配 Firefox 窗口标题/进程名）。

### 阶段 5：发布

1. Camoufox 镜像包上传 OSS，配置清单入库存档；
2. 发布带开关的本地程序新版本（云端清单控制新用户直接装 Camoufox，存量用户升级引导）；
3. 小范围灰度（内部 + 少量用户）→ 全量。

---

## 七、风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| 平台页面在 Firefox 下渲染/行为差异，选择器失效 | 平台流程失败率高 | 阶段 4 全量回归；选择器全部走平台配置表（数据库定位器），失效时可热更新，不用重发版 |
| camoufox-js 是 Experimental | 边缘功能可能有 bug；升级节奏受上游约束 | 锁定精确版本；封装层隔离（浏览器启动只动 browser-session.ts 一个文件面）；有问题可读源码（开源）或给 Apify 提 issue |
| 用户升级后登录态丢失 | 体验受损，客服压力 | 决策点 D6 的 Cookie 重注入 + 升级提示文案讲清楚 + 平台登录页引导 |
| 安装包大（GB 级）下载慢 | 用户安装等待时间长 | OSS 镜像 + 断点续传（安装器已支持校验重试）+ 下载进度文案 |
| playwright-core 降级引入回归 | Worker 其他功能受影响 | 降级后先跑一遍现有全部单测与冒烟；1.60 与 1.61 差异小，风险可控 |
| better-sqlite3（camoufox-js 依赖）原生模块 | npmmirror 安装/打包可能踩坑 | 阶段 0 一并验证；必要时用 `--build-from-source` 或预编译镜像 |
| 双内核并存期的组件冲突 | 用户机器上 Chromium/Firefox 残留 | 安装器提供旧组件清理逻辑；Windows 进程清理名单补充 camoufox |

---

## 八、回滚方案

- 云端下载清单保留 cloakbrowser 组件条目直至全量稳定；出现严重问题时，云端把清单切回旧组件即可让新装用户回退，无需发版。
- Worker 内启动封装集中在 browser-session.ts，必要时可做一个 `BROWSER_DRIVER=cloakbrowser|camoufox` 的环境变量开关（初版可以不做，靠 Git 分支回滚即可）。

---

## 九、工作量与顺序小结

| 顺序 | 内容 | 相对工作量 |
|------|------|-----------|
| 0 | PoC 验证（含目录结构/GeoIP/指纹持久化实测） | 小，但是闸门 |
| 1 | Worker 切换（browser-session/fingerprint/action-service/依赖） | 中 |
| 2 | 本地 Go（runtime 安装/探测/预检/环境变量） | 中 |
| 3 | 云端（清单迁移 + 管理端展示文案） | 小 |
| 4 | 三平台流程回归（问题大概率集中在这） | 大（主要是验证与修选择器） |
| 5 | OSS 镜像 + 灰度发布 | 小 |

---

## 附：本次调研的信息来源

- Camoufox 官方：camoufox.com（features / installation / usage / geoip / remote-server / fingerprint 各页）
- camoufox-js@0.12.0：npmjs.com/package/camoufox-js、github.com/apify/camoufox-js、dist/*.d.ts 类型定义
- 代码库现状：本仓库 local-agent-go-new（worker + internal）与 local-agent-go（旧版链路）调研记录
