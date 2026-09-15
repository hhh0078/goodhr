# Camoufox 功能介绍

> 本文档作用：全面介绍 Camoufox 反检测浏览器的定位、核心能力和技术原理，作为 GoodHR 浏览器内核从 CloakBrowser 迁移到 Camoufox 的决策依据之一。
>
> 调研时间：2026-09-16，信息来源：Camoufox 官方文档（camoufox.com）、GitHub 仓库（daijro/camoufox、apify/camoufox-js）、npm 包页面。

---

## 一、Camoufox 是什么

Camoufox 是一个**基于 Firefox 深度定制的开源反检测浏览器**（anti-detect browser），专为爬虫、自动化和 AI Agent 设计。

一句话理解它的核心思路：

> 指纹伪装不是靠往页面里"注脚本"改数据，而是在浏览器 **C++ 源码层面**把指纹数据拦截改掉。网站用 JS 去查任何指纹信息，拿到的都是"原生样子"的假数据，查不出破绽。

这一点和 GoodHR 现有的"零脚本注入"红线天然契合：Camoufox 本身就不依赖 JS 注入来实现隐身。

### 与 CloakBrowser 的定位对比

| 维度 | CloakBrowser（现用） | Camoufox（目标） |
|------|---------------------|-----------------|
| 内核 | Chromium（魔改） | Firefox（魔改，基于 Tor Browser 反指纹研究） |
| 指纹伪装层级 | 浏览器编译层 | C++ 实现层（拦截后暴露给页面） |
| 自动化协议 | CDP（Chrome DevTools Protocol） | Juggler（Playwright 的 Firefox 协议，经过改造） |
| 指纹生成方式 | `--fingerprint=种子` 数字种子 | BrowserForge 按真实设备分布统计生成，或传入自定义指纹 |
| 开源情况 | 闭源，二进制再分发有 OEM 授权风险 | 开源（camoufox-js 为 MPL-2.0），无再分发授权风险 |
| 官方接口 | npm 包（Playwright 风格） | Python 包最成熟；JS 包 `camoufox-js` 由 Apify 维护 |
| 去膨胀 | - | 大量裁剪 Mozilla 服务，内存约 200MB，比原版 Firefox 快 |

---

## 二、核心能力清单（来自官方 Features List）

### 1. 指纹伪装（Fingerprint spoofing）

- Navigator 属性伪装（设备、浏览器、语言环境 locale 等）
- 屏幕尺寸、分辨率、颜色深度等模拟
- WebGL 参数伪装：vendor/renderer、支持的扩展、context attributes、shader 精度格式
- 窗口内部/外部视口尺寸（inner/outer window）伪装
- AudioContext 伪装：采样率、输出延迟、最大声道数
- 设备语音（voices）与播放速率伪装
- 麦克风、摄像头、扬声器数量伪装
- 网络请求头伪装：Accept-Languages、User-Agent 与 navigator 属性保持一致
- **WebRTC IP 在协议层伪装**（不是 JS 层，查不出来）
- 地理位置、时区、语言环境伪装
- Battery API（电池）伪装

### 2. 隐身补丁（Stealth patches）

- 避免主世界执行泄露：所有页面自动化脚本运行在沙箱世界，页面探测不到
- 避免帧执行上下文泄露
- 修复 `navigator.webdriver` 检测
- 修复 Firefox 无头模式通过指针类型（pointer type）被检测的问题
- 使用非默认的屏幕与窗口尺寸组合
- 重新启用 fission 内容隔离、PDF.js
- 内置类人鼠标移动（human-like cursor movement）

### 3. 反字体指纹（Anti font fingerprinting）

- 按 User-Agent 自动匹配正确的系统字体
- 内置 Windows、Mac、Linux 三套系统字体
- 通过随机偏移字间距（letter spacing）防止字体度量指纹

### 4. Playwright 支持

- 使用 Playwright 定制的最新版 Firefox + 改造过的 Juggler 协议
- 附带多种反机器人检测的配置补丁
- **与现有 Playwright 代码完全兼容**，只需改浏览器初始化那一行

### 5. 精简与性能（Debloat）

- 裁剪/禁用了大量 Mozilla 服务，比原版 Firefox 更快、更省内存（约 200MB）
- 融合 LibreWolf、Ghostery、BetterFox、PeskyFox、FastFox 的去遥测与提速配置
- 移除全部 CSS 动画（对 AI Agent 还有额外好处：DOM 更干净）
- 极简主题

### 6. 插件（Addons）

- 通过传路径列表加载 Firefox 插件，无需调试服务器
- 内置 uBlock Origin（带自定义隐私过滤规则，可修 DNS 泄露）
- 插件不允许打开新标签页、自动在隐私模式启用、自动固定到工具栏

### 7. 人类化鼠标移动（Humanize）

- 算法源自 HumanCursor 项目，用 C++ 重写，轨迹随距离自适应
- 参数：`humanize`（开关/最大时长）、`humanize:maxTime`（默认 1.5 秒）、`humanize:minTime`、`showcursor`（光标高亮，仅在浏览器层显示，页面看不到、不会泄露）

### 8. 智能 GeoIP（配合代理使用）

- 传入 `geoip=True` 或目标 IP，自动按出口 IP 计算并伪装：经纬度、时区、国家、语言、WebRTC IP
- 还会按目标地区的"语言使用者分布"生成浏览器语言，避免"IP 在美国、语言却是 zh-CN"这种穿帮
- 官方建议配合住宅代理（residential proxy）使用

### 9. 指纹生成与预设

- 默认用 BrowserForge 生成指纹，**模拟真实世界设备统计分布**（比如 Linux 用户占 5%、其中 2560x1440 分辨率占 9.5%），避免"指纹虽然合理但整体分布异常"被检测
- 支持 `fingerprint_preset` 使用真实指纹预设（官方收录了 312 个来自真实 Firefox 流量的预设：180 个 Windows、67 个 macOS、65 个 Linux）
- 支持传入自定义指纹对象（JS 版对应 `fingerprint` 参数），可实现"一个账号一个固定指纹"

---

## 三、几个必须知道的技术特性

### 1. Juggler 协议沙箱（自动化隔离原理）

与 Chromium 系的 CDP 不同，Camoufox 用的是 Firefox 的 Juggler 协议并做了深度改造：Playwright 操作和读取的是页面的一个"独立副本"，真实页面完全不受影响。所以：

- 页面探测不到 `window.__playwright__` 之类的注入痕迹
- 自动化输入走 Firefox 原生输入通道，与真人操作无异

### 2. 只支持 Firefox 指纹，不支持 Chromium 指纹

官方明确：**Camoufox 不支持注入 Chromium 指纹**。因为任何有水平的 WAF 都会检测 V8 独有的 JS 行为，Firefox 内核伪装成 Chrome 必然穿帮。

→ 对 GoodHR 的影响：迁到 Camoufox 后，浏览器对外身份是 Firefox，User-Agent、页面渲染行为、部分选择器表现都会和 Chromium 有差异，平台流程需要回归验证。

### 3. 可配置项非常细

指纹配置按类别划分（传入 JSON/对象即可覆盖默认生成值）：
`navigator` / `cursor-movement` / `fonts` / `screen` / `window` / `document` / `headers` / `geolocation` / `webrtc` / `webgl` / `media-audio` / `voices` / `addons` / `miscellaneous`

### 4. 实用开关

| 开关 | 作用 |
|------|------|
| `block_images` | 屏蔽所有图片请求，省代理流量 |
| `block_webrtc` | 彻底禁用 WebRTC |
| `block_webgl` | 禁用 WebGL（仅特殊场景，否则可能反而泄露） |
| `disable_coop` | 禁用跨源开放策略，让跨域 iframe 里的元素（如 Cloudflare Turnstile 勾选框）可点击 |
| `enable_cache` | 启用页面缓存（默认关，省内存；关掉时不能用 go_back/go_forward） |

### 5. 版本管理

支持多版本共存与频道管理（`official/stable`、`official/prerelease`），CLI 一条命令切换、锁定、安装指定版本，便于灰度和回滚。

---

## 四、生态与活跃度（2026-09 现状）

| 项目 | 说明 |
|------|------|
| 主仓库 | github.com/daijro/camoufox（Python 生态，文档与浏览器构建发布处） |
| JS 客户端 | github.com/apify/camoufox-js，npm 包名 `camoufox-js`，v0.12.0（2026-08 发布），周下载约 8.3 万，Apify 官方维护 |
| JS 版定位 | README 自述 "Experimental JS port"，是 Python 包装器的移植版，不依赖 Python 运行 |
| JS 版要求 | Node >= 22；peer 依赖 `playwright-core < 1.61.0` |
| 浏览器分发 | `npx camoufox-js fetch` 从 GitHub Releases 下载，安装目录可用 `CAMOUFOX_INSTALL_DIR` 自定义 |

> 注意：JS 版虽然标注 Experimental，但由 Apify 维护、更新活跃、下载量大，属于"可用但有心理预期"的状态。Python 版是功能最全的参考实现。

---

## 五、对 GoodHR 场景的能力映射

| GoodHR 需求 | Camoufox 对应能力 | 结论 |
|-------------|-------------------|------|
| 反检测（Boss/猎聘/智联风控） | C++ 级指纹伪装 + 沙箱自动化 + 指纹统计分布生成 | 覆盖，且理论强度高于 JS 注入方案 |
| 一个账号一个稳定指纹 | `fingerprint` 参数可传入自定义 BrowserForge 指纹（持久化保存） | 覆盖（需替换现在的 `--fingerprint=种子` 方案） |
| 代理 + GeoIP 匹配 | `geoip` 参数原生支持，自动算时区/语言/经纬度 | 覆盖，甚至比 CloakBrowser 更细（WebRTC IP 协议级伪装） |
| 人类化鼠标/滚轮 | 内置 humanize 光标移动；滚轮仍走 Playwright `page.mouse.wheel` | 覆盖；现有 Worker 自研的类人操作原语可保留 |
| 登录态保持 | `user_data_dir` 持久化上下文（返回 BrowserContext） | 覆盖 |
| 多平台适配 | Playwright API 完全兼容 | 页面操作层基本不动，平台选择器需回归 |
| 零 JS 注入红线 | Camoufox 不靠 JS 注入实现隐身；`mw:` 主世界执行能力我们不用即可 | 兼容红线 |
| 离线分发到用户电脑 | 官方走 GitHub Releases，国内直连慢 | 需自建镜像（OSS）+ Go 安装器适配，方案见迁移文档 |

---

## 六、主要限制与风险提示

1. **Chromium → Firefox 的差异**：现有页面渲染、滚动行为、部分 CSS/DOM 表现在 Firefox 上可能不同，三个招聘平台的流程与选择器需要完整回归。
2. **旧 Profile 不能复用**：Chromium 的用户数据目录（含 localStorage 等登录态）无法迁移到 Firefox。用户升级后需要重新登录招聘平台，或用云端 Cookie 存储重新注入。
3. **playwright-core 版本锁定**：`camoufox-js@0.12.0` 要求 `playwright-core < 1.61.0`，与 Worker 现在的 1.61.1 冲突，需要降级。
4. **JS 版为 Experimental**：边缘功能可能不如 Python 版完整，遇到问题可能需要读源码或提 issue。
5. **安装包体积较大**：官方文档示例浏览器目录约 1.2GB（含多版本时更大），自建镜像的带宽与用户下载时长需要规划。
6. **Firefox 无 CDP**：依赖 `--remote-debugging-port` 直连 CDP 的代码（实验性 GoController）在 Camoufox 上不可用，需弃用或改走 WebSocket 服务器模式。

---

## 七、参考链接

- 官网：https://camoufox.com/
- 功能清单：https://camoufox.com/features/
- 指纹注入：https://camoufox.com/fingerprint/
- Python 用法：https://camoufox.com/python/usage/
- 安装与版本管理：https://camoufox.com/python/installation/
- GeoIP：https://camoufox.com/python/geoip/
- 远程服务器：https://camoufox.com/python/remote-server/
- JS 客户端：https://github.com/apify/camoufox-js 、 https://www.npmjs.com/package/camoufox-js
- 主仓库：https://github.com/daijro/camoufox
