// 文件作用说明：把 Camoufox 返回的弱类型启动参数转换为 Playwright 的强类型配置。

import type { LaunchOptions } from "playwright-core";
import { launchOptions as camoufoxLaunchOptions } from "camoufox-js";
import type { BrowserStartRequest } from "../../contracts/actions.js";
import type { ActionContext } from "../../contracts/common.js";
import type { loadStableProfileFingerprint } from "./fingerprint.js";
import { offlineAddonOptions } from "./bundled-addon.js";
import { useAutomaticWindowSize } from "./window-size.js";

/** toPlaywrightLaunchOptions 校验外部启动配置，不把未知字段直接传入浏览器。 */
export function toPlaywrightLaunchOptions(prepared: Record<string, unknown>): LaunchOptions {
  const options: LaunchOptions = {};
  const executablePath = prepared["executablePath"];
  if (typeof executablePath === "string" && executablePath) options.executablePath = executablePath;
  const headless = prepared["headless"];
  if (typeof headless === "boolean") options.headless = headless;
  const args = prepared["args"];
  if (Array.isArray(args)) options.args = args.filter((item): item is string => typeof item === "string");
  const env = prepared["env"];
  if (env && typeof env === "object" && !Array.isArray(env)) {
    const stringEnv: Record<string, string> = {};
    for (const [key, value] of Object.entries(env)) {
      if (["string", "number", "boolean"].includes(typeof value)) stringEnv[key] = String(value);
    }
    options.env = stringEnv;
  }
  const userPrefs = prepared["firefoxUserPrefs"];
  if (isPrimitiveRecord(userPrefs)) options.firefoxUserPrefs = userPrefs;
  const proxy = prepared["proxy"];
  if (isProxySettings(proxy)) options.proxy = proxy;
  return options;
}

/** CamoufoxProxySettings 与 Playwright 代理配置保持结构一致。 */
interface CamoufoxProxySettings { server: string; bypass?: string; username?: string; password?: string; }

/** isProxySettings 校验并收敛外部返回的代理配置。 */
function isProxySettings(value: unknown): value is CamoufoxProxySettings {
  if (!value || typeof value !== "object") return false;
  const candidate = value as { server?: unknown };
  return typeof candidate.server === "string" && candidate.server.length > 0;
}

/** isPrimitiveRecord 判断记录是否只包含基础类型，拒绝复杂对象或数组。 */
function isPrimitiveRecord(value: unknown): value is Record<string, string | number | boolean> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return Object.values(value).every(item => typeof item === "string" || typeof item === "number" || typeof item === "boolean");
}

/** buildLaunchOptions 把启动请求转换为 Camoufox + Playwright 启动参数。 */
export async function buildLaunchOptions(
  request: BrowserStartRequest,
  actionContext: ActionContext,
  fingerprint?: Awaited<ReturnType<typeof loadStableProfileFingerprint>>,
): Promise<LaunchOptions> {
  // 决策：用户与招聘平台均在中国大陆，语言与时区统一固定为 zh-CN / Asia/Shanghai，
  // 无需按代理 IP 动态匹配地理位置，因此不启用 Camoufox 的 geoip（协议字段保留但已停用）。
  const prepared = await camoufoxLaunchOptions({
    headless: request.headless ?? false,
    // 由浏览器生成完整拟人轨迹，最长约 0.8 秒；Worker 只发送目标坐标并等待移动结束。
    humanize: 0.8,
    // 运行组件会修正浏览器资源，启动时刷新代码缓存，使已有 Profile 立即使用新资源。
    args: ["-purgecaches"],
    locale: request.locale ?? "zh-CN",
    proxy: request.proxy,
    // Go 从内置包准备 UBO；显式关闭默认下载，首次启动也不等待外网。
    ...await offlineAddonOptions(request.extension_paths, actionContext),
    // 不传 executable_path：macOS 下 camoufox-js 会到错误目录找 properties.json；
    // 通过 CAMOUFOX_INSTALL_DIR 环境变量（Go 注入）让 camoufox-js 按官方逻辑解析启动文件。
    config: { timezone: request.timezone ?? "Asia/Shanghai" },
    // 界面语言：Camoufox 只打包英文界面资源，中文界面依赖 Go 预装的 zh-CN 语言包；
    // 这里请求简体中文，语言包缺失时 Firefox 会自动回退英文，不影响启动。
    firefox_user_prefs: {
      "intl.locale.requested": "zh-CN",
      // Windows 使用系统标题栏绘制窗口按钮，避免字体伪装把按钮图标变成方块。
      ...(process.platform === "win32" ? { "browser.tabs.inTitlebar": 0 } : {}),
    },
    ...(fingerprint ? { fingerprint } : {}),
  });
  return useAutomaticWindowSize(toPlaywrightLaunchOptions(prepared));
}
