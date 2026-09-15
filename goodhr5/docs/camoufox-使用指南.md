# Camoufox 使用指南

> 本文档作用：整理 Camoufox 的安装、启动、常用参数和 Node.js（camoufox-js）的完整用法，作为 GoodHR Worker 改造时的技术参考手册。
>
> 重点偏向 Node.js/TypeScript 用法（我们的 Worker 是 Node 环境），Python 用法作为对照。
>
> 调研时间：2026-09-16，来源：camoufox.com 官方文档与 camoufox-js@0.12.0 的 TypeScript 类型定义（以类型定义为准，最权威）。

---

## 一、安装

### 1. Node.js 版（camoufox-js）

```bash
# 安装 JS 客户端（同时需要项目里已有 playwright-core < 1.61.0）
npm install camoufox-js

# 下载 Camoufox 浏览器本体（默认装到用户缓存目录，如 macOS: ~/Library/Caches/camoufox）
npx camoufox-js fetch

# 自定义安装目录（类似 Playwright 的 PLAYWRIGHT_BROWSERS_PATH）
CAMOUFOX_INSTALL_DIR=/opt/camoufox npx camoufox-js fetch
```

CLI 命令：

| 命令 | 作用 |
|------|------|
| `npx camoufox-js fetch` | 下载/更新浏览器 |
| `npx camoufox-js remove` | 删除已下载的浏览器 |
| `npx camoufox-js test [url]` | 打开浏览器访问某 URL 测试 |

### 2. Python 版（对照参考，功能最全）

```bash
pip install -U "camoufox[geoip]"   # geoip 是可选增强，强烈建议配合代理使用
python -m camoufox fetch           # 下载浏览器
```

Python 版 CLI 更丰富：`sync`（刷新版本列表）、`set`（选择/锁定版本）、`active`、`fetch`、`list`、`remove`、`server`（起远程 WebSocket 服务器）、`test`、`path`、`version`、`gui`。

版本管理支持 `official/stable`、`official/prerelease` 频道跟随，或锁定到具体版本（如 `official/stable/134.0.2-beta.20`），便于灰度与回滚。

### 3. 环境变量

| 变量 | 作用 |
|------|------|
| `CAMOUFOX_INSTALL_DIR` | 自定义浏览器安装/查找目录 |
| `CAMOU_CONFIG` / `CAMOU_CONFIG_<n>` | 浏览器进程间传递指纹配置（包装器自动处理，无需手动设置） |

### 4. 版本要求

- Node >= 22
- `playwright-core` peer 依赖 **< 1.61.0**（注意：GoodHR Worker 当前是 1.61.1，迁移时必须降级，例如 1.60.x）

---

## 二、Node.js 三种启动方式

### 方式 1：`Camoufox()` 一把梭（推荐日常用）

```typescript
import { Camoufox } from 'camoufox-js';

// 普通启动：返回 Playwright Browser
const browser = await Camoufox({ headless: false });
const page = await browser.newPage();
await page.goto('https://example.com');
await browser.close();
```

```typescript
// 持久化启动（保留登录态）：传 user_data_dir，返回的是 Playwright BrowserContext！
const context = await Camoufox({
  user_data_dir: '/path/to/profile-dir',
});
const page = context.pages()[0] ?? await context.newPage();
// Cookie、localStorage 等都会保存在该目录
await context.close();
```

> 关键差异：传了 `user_data_dir` 返回 **BrowserContext**（持久上下文），没传返回 **Browser**。类型定义里通过泛型精确区分，TS 会自动推导。

### 方式 2：`launchOptions()` + 原生 `firefox.launch`（需要精细控制时用）

```typescript
import { launchOptions } from 'camoufox-js';
import { firefox } from 'playwright-core';

const browser = await firefox.launch({
  ...(await launchOptions({ /* Camoufox 选项 */ })),
  // 这里可以继续写 Playwright 原生选项，会覆盖 Camoufox 的同名项
});
```

适合场景：需要额外传 Playwright 启动参数（如 `downloadsPath`、`timeout`）时。

### 方式 3：`launchServer()` 远程 WebSocket 服务器（多语言/多进程场景）

```typescript
import { launchServer } from 'camoufox-js';
import { firefox } from 'playwright-core';

const server = await launchServer({ port: 8888, ws_path: '/camoufox' });
const browser = await firefox.connect(server.wsEndpoint());
const page = await browser.newPage();
// ... 正常使用
await browser.close();
await server.close();
```

> 官方提醒：远程服务器模式下**只有一个浏览器实例，指纹不会随会话轮换**；且该功能标注为实验性。按账号隔离指纹的场景（如 GoodHR）不适合共用一个服务器实例，建议每账号独立启动。

---

## 三、LaunchOptions 完整参数表（camoufox-js@0.12.0 类型定义）

### 指纹相关

| 参数 | 类型 | 说明 |
|------|------|------|
| `os` | `string \| string[]` | 指纹的目标系统：`"windows"` / `"macos"` / `"linux"` 或数组随机。默认三系统随机 |
| `fingerprint` | `Fingerprint`（fingerprint-generator 包） | 传入自定义 BrowserForge 指纹；不传则按 `os`/`screen` 随机生成。**实现"一号一指纹"的关键参数** |
| `screen` | `Screen` | 约束生成指纹的屏幕尺寸（min/max width/height） |
| `window` | `[number, number]` | 固定窗口大小（官方警告：固定窗口易被指纹识别，调试用） |
| `webgl_config` | `[string, string]` | 指定 WebGL vendor/renderer 组合，必须与目标 os 匹配，否则泄露 |
| `ff_version` | `number` | 伪装的 Firefox 版本号（仅特殊场景用） |
| `fonts` | `string[]` | 额外加载的字体（在目标 os 默认字体之外） |
| `custom_fonts_only` | `boolean` | 只用传入字体，不加载系统字体 |

### 行为相关

| 参数 | 类型 | 说明 |
|------|------|------|
| `humanize` | `boolean \| number` | 类人鼠标移动。`true` 或最大时长秒数（如 `1.5`，光标通常最多 1.5 秒移过窗口） |
| `locale` | `string \| string[]` | 语言环境，第一个用于 Intl API |
| `addons` | `string[]` | Firefox 插件路径列表（需解压后的目录） |
| `exclude_addons` | `string[]` | 排除的内置默认插件 |

### 网络相关

| 参数 | 类型 | 说明 |
|------|------|------|
| `proxy` | `string \| PlaywrightProxy` | 代理。字符串如 `"http://host:8080"`，或 `{ server, username, password }` |
| `geoip` | `string \| boolean` | 按出口 IP（或指定 IP）自动计算并伪装经纬度、时区、国家、语言、WebRTC IP。`true` 为自动探测 |
| `block_images` | `boolean` | 屏蔽图片请求，省流量 |
| `block_webrtc` | `boolean` | 彻底禁用 WebRTC |
| `block_webgl` | `boolean` | 禁用 WebGL（仅特殊场景） |
| `disable_coop` | `boolean` | 禁用 COOP，使跨域 iframe 内元素（如 Turnstile 勾选框）可点击 |

### 启动相关

| 参数 | 类型 | 说明 |
|------|------|------|
| `headless` | `boolean \| "virtual"` | 无头模式。`"virtual"` 仅 Linux（用 Xvfb 虚拟显示跑有头模式） |
| `executable_path` | `string` | 自定义浏览器可执行文件路径。**GoodHR 自建分发必须用这个** |
| `user_data_dir` | `string` | 持久化用户数据目录（传入后返回 BrowserContext） |
| `firefox_user_prefs` | `Record<string, any>` | Firefox 原生 about:config 偏好设置 |
| `args` | `string[]` | 传给浏览器的启动参数 |
| `env` | `object` | 浏览器进程环境变量 |
| `main_world_eval` | `boolean` | 允许 `mw:` 前缀脚本在主世界执行。**GoodHR 零注入红线，禁止开启使用** |
| `enable_cache` | `boolean` | 页面缓存（默认关；关着不能用 go_back/go_forward） |
| `debug` | `boolean` | 打印发送给浏览器的配置，调试用 |
| `i_know_what_im_doing` | `boolean` | 跳过"配置可能导致泄露"的警告 |

> 补充：包装器默认把 `newPage()` 的 viewport 设为 `null`（不强制 1280x720），让 Juggler 直接测量真实窗口，保证窗口尺寸指纹一致。调用方显式传 viewport 时以调用方为准。

---

## 四、典型场景示例（Node.js/TypeScript）

### 场景 1：带代理 + GeoIP 的持久化登录会话（对标 GoodHR 平台账号）

```typescript
import { Camoufox } from 'camoufox-js';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';

const profileDir = `/data/profiles/account-123`;
const fingerprintPath = `/data/profiles/account-123/fingerprint.json`;

// 首次生成指纹并保存，之后同一账号永远用同一个指纹
let fingerprint: Fingerprint;
if (existsSync(fingerprintPath)) {
  fingerprint = JSON.parse(readFileSync(fingerprintPath, 'utf-8'));
} else {
  // 用 fingerprint-generator 生成，或首次从 Camoufox 启动结果中导出后保存
  fingerprint = generateFingerprint({ browsers: ['firefox'], os: 'windows' });
  writeFileSync(fingerprintPath, JSON.stringify(fingerprint));
}

const context = await Camoufox({
  user_data_dir: profileDir,
  fingerprint,                       // 一号一指纹
  os: 'windows',
  humanize: 1.5,                     // 类人鼠标移动，最大 1.5 秒
  geoip: true,                       // 按代理出口 IP 自动匹配时区/语言/经纬度
  proxy: {
    server: 'http://proxy.example.com:8080',
    username: 'user',
    password: 'pass',
  },
});

const page = context.pages()[0] ?? await context.newPage();
await page.goto('https://www.zhipin.com/');
```

### 场景 2：完全用 Playwright 原生写法（迁移期最小改动）

```typescript
import { launchOptions } from 'camoufox-js';
import { firefox } from 'playwright-core';

const browser = await firefox.launch({
  ...(await launchOptions({
    os: 'macos',
    humanize: true,
    geoip: true,
    executable_path: '/opt/camoufox/浏览器可执行文件路径',
    proxy: 'http://user:pass@proxy:8080',
  })),
  headless: false,
});
```

### 场景 3：指纹稳定性策略说明

Camoufox 与 CloakBrowser 的指纹机制差异：

| | CloakBrowser（现用） | Camoufox |
|---|---|---|
| 指定方式 | 启动参数 `--fingerprint=10000~99999` 数字种子 | 启动选项 `fingerprint`（完整指纹对象） |
| 稳定性来源 | 种子相同 → 指纹相同 | 对象相同 → 指纹相同 |
| GoodHR 适配 | 按 `sha256(userDataDir)` 生成种子 | **按账号生成 BrowserForge 指纹并持久化为 JSON**，启动时读入传入 |

即：把现在的"种子算法"换成"指纹文件存储"，稳定性的语义不变。

---

## 五、指纹配置项（进阶，按类别覆盖）

默认情况下不需要手动配置（BrowserForge 自动生成完整一致的指纹）。确有需要时，可通过各语言的 `config` 参数按类别覆盖，完整类别：

`navigator`、`cursor-movement`、`fonts`、`screen`、`window`、`document`、`headers`、`geolocation`、`webrtc`、`webgl`、`media-audio`、`voices`、`addons`、`miscellaneous`

示例（Python 写法，JS 通过 `launchOptions({ config: {...} })` 等价传递）：

```python
with Camoufox(config={
    "webrtc:ipAddress": "203.0.113.0",
    "webgl:vendor": "Intel",
    "webgl:renderer": "Intel Iris OpenGL Engine",
}) as browser:
    ...
```

> 官方建议：这类配置属于高级功能，包装器会帮你把大部分属性填成自洽的值，手动覆盖前先确认不会造成"指纹内部不一致"。

---

## 六、GeoIP 使用要点

1. 参数：`geoip: true`（自动探测出口 IP）或 `geoip: "1.2.3.4"`（指定目标 IP）。
2. 需配合 `proxy` 使用；若 `geoip` 为 `true`，探测请求会走代理发出。
3. 自动生成并伪装：经纬度、时区、国家、语言环境（按目标地区语言分布）、WebRTC IP。
4. 需要本地 GeoIP 数据库（官方用 MaxMind GeoLite2，约 40MB；JS 版依赖 `maxmind` 包）。
5. 官方建议配合住宅代理使用效果最好。

---

## 七、检测自验方法

改完内核后建议用下列公开检测站点验证（模拟真实用户手动访问即可，符合零注入规则）：

- https://www.browserscan.net （综合指纹评分，官方文档用它做 GeoIP 演示）
- https://abrahamjuliot.github.io/creepjs （CreepJS，Camoufox 官方致谢的测试站）
- https://browserleaks.com （单项指纹详情）
- https://www.browserscan.net 之外还可用 bot.sannysoft.com 看自动化痕迹

重点核对项：UA 与 navigator 一致、时区/语言与代理 IP 一致、WebRTC 不泄露真实 IP、`navigator.webdriver` 为 false、Canvas/WebGL/字体指纹无异常。

---

## 八、GoodHR 使用注意（结合项目红线）

1. **零脚本注入红线**：Camoufox 的 `main_world_eval`（`mw:` 前缀主世界执行）能力**禁止启用**；页面操作继续走 Playwright 标准 Page/Locator/鼠标/键盘/滚轮。
2. **humanize 取舍**：Camoufox 内置的 humanize 只作用于鼠标移动。Worker 已自研类人点击/输入/滚轮原语（多段贝塞尔移动 + 真实 wheel），建议迁移初期 `humanize: false`，避免"双重人类化"导致移动轨迹和耗时不可控；后期可 A/B 对比再决定。
3. **viewport 注意**：包装器默认不强制 viewport（测窗口真实值）。Worker 里依赖固定 viewport 的截图拼接、滚动安全边距逻辑需要回归确认。
4. **headless**：本地程序默认有头（用户可见浏览器窗口），与现状一致即可；`"virtual"` 只在 Linux 服务器上用。
