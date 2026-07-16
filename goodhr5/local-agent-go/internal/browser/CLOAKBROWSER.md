# CloakBrowser 能力与 GoodHR 接入说明

> 文件作用：记录 CloakBrowser 的官方能力、限制、GoodHR 当前接入方式和后续开发注意事项，避免维护过程中把浏览器能力、Node 控制层和实验性 Go 控制层混为一谈。
>
> 核对日期：2026-07-15。CloakBrowser 更新较快，升级前应重新检查官网、官方仓库和许可证。

## 1. CloakBrowser 是什么

CloakBrowser 是面向浏览器自动化的定制 Chromium。它不是仅靠启动参数或页面 JavaScript 注入隐藏自动化，而是在 Chromium 源码层修改浏览器指纹和自动化特征，并提供兼容 Playwright、Puppeteer 的包装器。

官方定位是“用于合法自动化的隐身 Chromium”。它的目标是降低普通 Playwright、Puppeteer 或无头 Chromium 被反机器人系统识别的概率，但不能保证所有网站、所有时间都不被识别。

官方资料：

- 官网：[cloakbrowser.dev](https://cloakbrowser.dev/)
- 官方 GitHub：[CloakHQ/CloakBrowser](https://github.com/CloakHQ/CloakBrowser)
- 官方 npm 包：[cloakbrowser](https://www.npmjs.com/package/cloakbrowser)
- 官方更新记录：[CHANGELOG.md](https://github.com/CloakHQ/CloakBrowser/blob/main/CHANGELOG.md)

## 2. 官方提供的主要能力

### 2.1 Chromium 源码级指纹处理

官方说明其修改直接编译进 Chromium 二进制，而不是运行后向页面注入脚本。覆盖的信号包括：

- Canvas、WebGL、WebGPU、GPU 厂商和渲染器；
- Audio、字体枚举、Client Rects；
- 屏幕尺寸、设备内存、CPU 并发数等硬件信息；
- User-Agent、`navigator.webdriver`、插件列表和 `window.chrome` 等浏览器特征；
- WebRTC、网络时序、TLS/CDP/自动化相关信号；
- 浏览器输入行为的一致性。

源码级修改的价值是：页面脚本读取到的多个相关属性更容易保持一致，不只是单独伪造某一个字段。

### 2.2 Playwright、Puppeteer 和其他框架兼容

官方 JavaScript 包可作为 Playwright 或 Puppeteer 的近似替代入口。官方也列出了 Selenium、browser-use、Crawl4AI、Stagehand、LangChain 等集成方式。

对 GoodHR 来说，最合适的常规接入仍是 Playwright 兼容接口，因为现有 Node Worker 已经使用 Playwright 风格的页面、定位器和持久化上下文。

### 2.3 人类化交互

官方包装器的 `humanize` 能力包括：

- 贝塞尔曲线鼠标移动；
- 更自然的键盘输入节奏和停顿；
- 更接近真人的滚动行为。

这类行为层能力与 Chromium 二进制的指纹补丁是两层不同能力。只启动 CloakBrowser 可执行文件，并不表示调用方自动获得官方包装器的全部人类化逻辑。

### 2.4 持久化浏览器资料

通过 persistent context 和固定的 `userDataDir`，可以保留：

- 登录 Cookie；
- LocalStorage 等站点存储；
- 浏览器缓存；
- 网站会话状态。

招聘平台要求稳定登录，因此 GoodHR 应继续为同一个本地用户复用固定资料目录，不能在每次岗位运行中临时生成新资料目录。

### 2.5 代理和地理信息一致性

官方支持 HTTP、SOCKS5 代理，并能根据代理出口 IP 自动匹配时区、语言和区域。也可以显式指定 timezone、locale、平台、屏幕和硬件参数。

需要注意：CloakBrowser 不内置代理池，也不自动轮换代理。代理质量、出口信誉和账号使用环境仍由调用方负责。

### 2.6 二进制管理和多平台

官方包装器支持首次运行自动下载、缓存、检查更新、清理缓存和固定指定 Chromium 版本。官方当前覆盖 Windows x64、Linux x64/ARM64、macOS Intel/Apple Silicon；具体免费版和 Pro 版版本号以官网为准。

官方还提供 Docker、持久 CDP 服务和多 fingerprint seed 等运行方式，但 GoodHR 当前本地程序不使用 Docker。

## 3. 它不负责什么

CloakBrowser 不是万能的验证码解决器，也不能保证绕过所有风控：

- 它不会代替用户破解或自动识别验证码；
- 它不提供代理轮换服务；
- IP 信誉、账号行为、操作频率和业务异常仍可能触发风控；
- 无头模式、数据中心代理、时区与 IP 不一致仍可能暴露自动化；
- 旧版浏览器的隐身效果会随网站检测策略变化而下降；
- 过快、固定间隔、瞬间填值等机械操作仍可能被行为检测识别。

官方明确说明，它主要用于减少不必要的挑战出现，不等同于 CAPTCHA solving 服务。

## 4. GoodHR 当前的接入结构

### 4.1 当前生产链路：Node Worker

当前正式运行链路仍然是：

```text
本地程序主流程
  -> 平台 Runtime
  -> Browser Executor
  -> Node Worker
  -> cloakbrowser npm 包
  -> Playwright 风格 API
  -> CloakBrowser Chromium
```

当前事实：

- `internal/app/server.go` 创建的是 `browser.NewWorkerManager`；
- `worker-node/src/index.js` 动态导入 `cloakbrowser`；
- Node Worker 使用 `launchPersistentContext` 保存登录资料；
- Node Worker 当前默认启用 `humanize`，除非调用参数明确传入 `false`；
- `worker-node/package.json` 当前依赖范围是 `cloakbrowser ^0.3.27`。

因此，现阶段打包和运行仍依赖 Node Worker。实验性 Go 控制器的存在不会自动改变生产链路。

### 4.2 实验链路：GoController

Go 控制器入口位于：

- `NewGoController()`；
- `NewGoControllerWithOptions(GoControllerOptions)`。

它直接启动 CloakBrowser 可执行文件并连接 Chrome DevTools Protocol，目前已拆分为：

- `go_session.go`：启动、停止、页面和会话；
- `go_element.go`：元素查找、属性、文本、引用和视口信息；
- `go_input.go`：真实鼠标点击、键盘输入和快捷键；
- `go_scroll.go`：真实鼠标滚轮滚动；
- `go_screenshot.go`：页面及元素截图；
- `go_download.go`：Cookie 和下载目录；
- `go_cdp.go`：CDP/WebSocket 通信；
- `go_actions.go`：通用组合动作及临时兼容路由；
- `go_controller.go`：统一入口、类型和路由分发。

当前已经通过真实浏览器验证：启动、输入、点击、滚轮滚动和确保元素进入视口。

### 4.3 GoController 当前能力边界

GoController 使用 CloakBrowser 二进制，因此可以继承二进制本身的 Chromium 源码级补丁。但是它直接使用原始 CDP，不经过官方 JavaScript 包的完整驱动层，所以必须注意：

- 不能假设 GoController 自动拥有官方 `humanize` 的全部实现；
- 不能假设原始 CDP 与官方推荐的 Playwright 驱动具有完全相同的自动化特征；
- 当前真实鼠标、键盘和滚轮事件只解决了“不要用 DOM 直接赋值或 JS 滚动”的基础问题，不等同于完整的人类行为模型；
- 在替换 Node Worker 前，必须对 BOSS、猎聘、智联等真实站点分别进行登录稳定性、风控、指纹和长时间运行测试；
- 未完成同等能力验证前，GoController 只能保持实验状态。

## 5. 后续开发规则

1. 平台业务逻辑放在 `internal/platforms/{platform}`，不要重新堆进浏览器控制器。
2. 浏览器目录只放通用原子操作和可复用组合操作。
3. 页面滚动使用真实鼠标滚轮事件，不使用 `window.scrollBy`、`element.scrollBy` 或注入 `scrollIntoView`。
4. 输入优先使用真实键盘事件或官方人类化输入，不直接修改 DOM 的 `value`。
5. 候选人点击前重新确认元素仍存在且位于当前视口，避免使用过期元素引用。
6. 浏览器版本、资料目录、时区、语言、代理和账号环境应保持一致。
7. 升级 CloakBrowser 包或二进制时，要记录包装器版本和 Chromium 版本，并执行招聘平台回归测试。
8. 不要仅依据官网测试成绩承诺“百分之百不会被检测”；这些成绩属于官方在特定版本和环境下的测试结果。

## 6. 版本与打包注意事项

- GoodHR 本地程序版本 `5.3.5` 与 CloakBrowser/npm/Chromium 版本是三套不同版本，不能混用。
- npm 包使用 `^0.3.27` 时，重新安装依赖可能解析到同一主版本下更新的包。正式打包应使用锁文件或明确版本，避免不同电脑安装出不同依赖。
- 浏览器二进制更新可能改变网站兼容性。稳定发布时应固定并记录经过验证的二进制版本。
- 官方许可证说明：包装器代码与浏览器二进制的授权条件不完全相同。官方当前说明二进制不能随意重新分发、转售或重新打包；把二进制嵌入发给第三方的产品可能需要 OEM/SaaS 授权。
- GoodHR 在发布安装包前，应再次核对官方最新的 [BINARY-LICENSE.md](https://github.com/CloakHQ/CloakBrowser/blob/main/BINARY-LICENSE.md) 和官网 FAQ。内部使用、让最终用户从官方渠道下载、把二进制直接打进安装包，这三种方式的授权含义不同。

## 7. 安全边界

CDP 端口可以读取页面、执行脚本并控制浏览器，权限很高：

- 只能绑定本机 `127.0.0.1`；
- 不得直接暴露到公网或局域网；
- 不得在日志里输出 Cookie、代理密码、登录凭证或完整 CDP WebSocket 地址；
- 仅用于用户已授权的招聘账号、用户自己的数据和合法业务流程；
- 遵守招聘平台条款、访问频率限制和所在地法律。

## 8. 升级前检查清单

- [ ] 官网、GitHub、npm 的最新版本和变更记录是否一致；
- [ ] Node 包版本和实际下载的 Chromium 版本是否已固定；
- [ ] Windows 新电脑是否能正确找到并启动浏览器；
- [ ] 固定资料目录能否保留登录状态；
- [ ] 点击、输入、滚动是否仍产生真实输入事件；
- [ ] BOSS 岗位切换、候选人列表、详情页和打招呼是否通过；
- [ ] 长时间运行后是否存在页面引用过期、内存增长或浏览器残留进程；
- [ ] 是否检查了最新二进制分发许可证；
- [ ] GoController 若要替换 Node，是否完成逐项能力对照和真实平台回归测试。
