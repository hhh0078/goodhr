// 文件作用说明：定义 Go 可调用的 Browser Worker 封装能力请求和返回类型。

import type { Cookie } from "playwright-core";
import type { JsonObject, JsonValue } from "./common.js";
import type { ElementView, FindResult, SelectorSpec } from "./selector.js";

/** ProxyConfig 表示浏览器代理配置。 */
export interface ProxyConfig {
  server: string;
  bypass?: string;
  username?: string;
  password?: string;
}

/** BrowserStartRequest 表示启动或复用 CloakBrowser 的参数。 */
export interface BrowserStartRequest {
  user_data_dir?: string;
  downloads_path?: string;
  headless?: boolean;
  humanize?: boolean;
  geoip?: boolean;
  url?: string;
  wait_until?: "load" | "domcontentloaded" | "networkidle" | "commit";
  timeout_ms?: number;
  new_tab?: boolean;
  locale?: string;
  timezone?: string;
  user_agent?: string;
  viewport_width?: number;
  viewport_height?: number;
  proxy?: string | ProxyConfig;
  args?: string[];
}

/** BrowserStatusResult 表示浏览器运行状态。 */
export interface BrowserStatusResult extends JsonObject {
  running: boolean;
  persistent: boolean;
  reused: boolean;
  user_data_dir: string;
  downloads_path: string;
  current_url: string;
}

/** WorkerRuntimeStatus 表示 CloakBrowser 增强浏览器二进制安装状态。 */
export interface WorkerRuntimeStatus extends JsonObject {
  cloakbrowser_version: string;
  platform: string;
  binary_path: string;
  installed: boolean;
}

/** PageOpenRequest 表示打开页面的参数。 */
export interface PageOpenRequest {
  url: string;
  wait_until?: "load" | "domcontentloaded" | "networkidle" | "commit";
  timeout_ms?: number;
  new_tab?: boolean;
}

/** PageInfo 表示一个浏览器标签页。 */
export interface PageInfo extends JsonObject {
  page_id: string;
  url: string;
  title: string;
  current: boolean;
}

/** PageListResult 表示当前全部浏览器标签页。 */
export interface PageListResult extends JsonObject {
  pages: PageInfo[];
  count: number;
}

/** PageUseRequest 表示切换当前标签页的参数。 */
export interface PageUseRequest {
  page_id: string;
}

/** ElementFindRequest 表示查找一个元素的参数。 */
export interface ElementFindRequest {
  selector: SelectorSpec;
}

/** ElementFindAllRequest 表示查找多个元素并读取字段的参数。 */
export interface ElementFindAllRequest {
  selector: SelectorSpec;
  max_items?: number;
  fields?: Record<string, SelectorSpec>;
  expected_missing?: boolean;
}

/** ElementReadRequest 表示读取元素文本、HTML 或属性的参数。 */
export interface ElementReadRequest {
  selector: SelectorSpec;
  property?: "text" | "html";
  attribute?: string;
}

/** ElementClickRequest 表示执行完整封装点击的参数。 */
export interface ElementClickRequest {
  selector: SelectorSpec;
  button?: "left" | "right" | "middle";
  click_count?: number;
  viewport_margin?: number;
  wait_for_new_page?: boolean;
  new_page_timeout_ms?: number;
  verify?: ClickVerification;
}

/** ClickVerification 表示点击后可选的结果验证。 */
export interface ClickVerification {
  url_contains?: string;
  target_hidden?: SelectorSpec;
  target_visible?: SelectorSpec;
  timeout_ms?: number;
}

/** ElementInputRequest 表示执行完整封装输入的参数。 */
export interface ElementInputRequest {
  selector: SelectorSpec;
  text: string;
  clear?: boolean;
  verify?: boolean;
  min_delay_ms?: number;
  max_delay_ms?: number;
}

/** KeyboardPressRequest 表示按键盘按键的参数。 */
export interface KeyboardPressRequest {
  key: string;
  delay_ms?: number;
}

/** ScrollRequest 表示页面或元素真实滚轮滚动参数。 */
export interface ScrollRequest {
  target?: SelectorSpec;
  wheel_anchor?: SelectorSpec;
  distance: number;
  max_attempts?: number;
  wait_ms?: number;
  require_full?: boolean;
  viewport_margin?: number;
}

/** ScreenshotRequest 表示页面或元素截图参数。 */
export interface ScreenshotRequest {
  target?: SelectorSpec;
  directory: string;
  filename: string;
  full_page?: boolean;
}

/** ScreenshotResult 表示截图保存结果。 */
export interface ScreenshotResult extends JsonObject {
  path: string;
  filename: string;
  size: number;
}

/** LongScreenshotRequest 表示使用真实鼠标滚轮分段截取长元素的参数。 */
export interface LongScreenshotRequest {
  target: SelectorSpec;
  wheel_anchor?: SelectorSpec;
  directory: string;
  filename: string;
  distance?: number;
  max_parts?: number;
  wait_ms?: number;
}

/** ScreenshotPart 表示长截图中的一个本地 PNG 分段。 */
export interface ScreenshotPart extends JsonObject {
  path: string;
  filename: string;
  size: number;
  index: number;
  scroll_position: number;
}

/** LongScreenshotResult 表示长元素分段截图结果。 */
export interface LongScreenshotResult extends JsonObject {
  parts: ScreenshotPart[];
  count: number;
  complete: boolean;
}

/** CookieListResult 表示当前浏览器 Cookie。 */
export interface CookieListResult {
  cookies: Cookie[];
  count: number;
}

/** CookieSetRequest 表示需要导入的 Cookie。 */
export interface CookieSetRequest {
  cookies: Cookie[];
}

/** DownloadRecord 表示 Worker 保存的一条浏览器下载记录。 */
export interface DownloadRecord extends JsonObject {
  id: string;
  filename: string;
  file_name: string;
  file_path: string;
  path: string;
  suggested_filename: string;
  url: string;
  page_url: string;
  size: number;
  status: "pending" | "saved" | "failed";
  error: string;
  created_at: string;
}

/** DownloadListResult 表示当前浏览器会话的下载记录。 */
export interface DownloadListResult extends JsonObject {
  downloads: DownloadRecord[];
  count: number;
  pending: number;
  directory: string;
}

/** DownloadConfigureRequest 表示切换后续下载保存目录的参数。 */
export interface DownloadConfigureRequest {
  directory: string;
}

/** ElementActionResult 表示元素封装能力的通用结果。 */
export interface ElementActionResult {
  success: boolean;
  find: FindResult;
  view: ElementView;
  details: JsonValue;
}
