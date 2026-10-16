/** 本文件负责新版后台的平台登录入口打开和登录状态轮询判断。 */
"use client";

import { currentLocalPageURL, openLocalPage } from "./admin-api";

export type PlatformPageRule = {
  url?: string;
  title?: string;
  match?: "contains" | "prefix" | "exact";
  code?: string;
  entry?: boolean;
};

export type PlatformAuthConfig = {
  pages: PlatformPageRule[];
  public_pages: PlatformPageRule[];
};

const URL_CHECK_INTERVAL_MS = 3000;
const URL_FIRST_CHECK_DELAY_MS = 5000;
const LOGIN_SUCCESS_CONFIRM_TIMES = 3;
const POSITION_LOGIN_CHECK_TIMES = 3;

/** pickPlatformAuthConfig 从平台配置列表中取出指定平台登录规则。 */
export function pickPlatformAuthConfig(configs: any[], platformID: string) {
  const key = `platform.${platformID}`;
  const item = (configs || []).find((config) => {
    const configKey = String(config?.config_key || config?.key || "");
    const currentID = String(
      config?.platform_id || config?.id || "",
    ).toLowerCase();
    return configKey === key || currentID === platformID;
  });
  if (!item) throw new Error(`平台 ${platformID} 缺少配置`);
  return parsePlatformAuthConfig(
    item.config_value || item.value || item,
    platformID,
  );
}

/** parsePlatformAuthConfig 解析现有已登录规则，并兼容旧版明确配置的登录地址前缀，不推断新的成功条件。 */
export function parsePlatformAuthConfig(
  value: unknown,
  platformID: string,
): PlatformAuthConfig {
  let parsed: any = value;
  if (typeof value === "string") {
    try {
      parsed = JSON.parse(value);
    } catch {
      throw new Error(`平台 ${platformID} 配置不是合法 JSON`);
    }
  }
  const authPages = parsed?.auth?.pages;
  const publicPages: PlatformPageRule[] = Array.isArray(parsed?.public?.pages)
    ? parsed.public.pages.filter((page: PlatformPageRule | null) => page && typeof page.url === "string" && page.url.trim())
    : [];
  if (!Array.isArray(authPages) || authPages.length === 0) {
    throw new Error(`平台 ${platformID} 配置缺少 auth.pages`);
  }
  if (!publicPages.some((page) => page.code === "login")) {
    publicPages.push(...legacyLoginPageRules(parsed?.auth?.login_url_prefixes));
  }
  if (publicPages.length === 0) {
    throw new Error(`平台 ${platformID} 配置缺少 public.pages 登录规则或 auth.login_url_prefixes`);
  }
  return { pages: authPages, public_pages: publicPages };
}

/** legacyLoginPageRules 只把旧配置中有效的完整 HTTP(S) 登录前缀映射为公开规则，不补充平台地址。 */
function legacyLoginPageRules(value: unknown): PlatformPageRule[] {
  if (!Array.isArray(value)) return [];
  const prefixes = value.filter((item): item is string => {
    if (typeof item !== "string" || !item.trim()) return false;
    try {
      const parsed = new URL(item.trim());
      return ["http:", "https:"].includes(parsed.protocol) && Boolean(parsed.hostname) && !parsed.username && !parsed.password;
    } catch { return false; }
  });
  return Array.from(new Set(prefixes.map((prefix) => prefix.trim()))).map((url) => ({ url, match: "prefix", code: "login" }));
}

/** pickAuthEntryURL 从平台登录规则中选择默认打开页面。 */
export function pickAuthEntryURL(auth: PlatformAuthConfig) {
  const pages = auth.pages || [];
  const page =
    pages.find((item) => item.entry && item.url) ||
    pages.find((item) => item.url);
  return String(page?.url || "");
}

/** pickLoginEntryURL 从公开页面规则中选择登录页。 */
export function pickLoginEntryURL(auth: PlatformAuthConfig) {
  const pages = auth.public_pages || [];
  const page =
    pages.find((item) => item.code === "login" && item.url) ||
    pages.find((item) => item.url);
  return String(page?.url || "");
}

/** openPlatformBrowser 打开平台账号对应的本地浏览器并导航到入口页。 */
export async function openPlatformBrowser(
  agentBase: string,
  account: any,
  auth: PlatformAuthConfig,
) {
  const targetURL = pickAuthEntryURL(auth);
  if (!targetURL) throw new Error("平台配置缺少入口页面地址");
  const browserPayload = {
    persistent: true,
    platform_account_id: account.id,
    user_data_dir: account.id,
    headless: false,
    humanize: true,
  };
  await openLocalPage(agentBase, { ...browserPayload, url: targetURL });
}

/** openPlatformLoginBrowser 打开平台账号对应的本地浏览器并直接导航到登录页。 */
export async function openPlatformLoginBrowser(
  agentBase: string,
  account: any,
  auth: PlatformAuthConfig,
) {
  const targetURL = pickLoginEntryURL(auth);
  if (!targetURL) throw new Error("平台配置缺少登录页面地址");
  const browserPayload = {
    persistent: true,
    platform_account_id: account.id,
    user_data_dir: account.id,
    headless: false,
    humanize: true,
  };
  await openLocalPage(agentBase, { ...browserPayload, url: targetURL });
}

/** openPlatformPositionBrowser 使用默认本地浏览器资料目录打开岗位运行平台入口页。 */
export async function openPlatformPositionBrowser(
  agentBase: string,
  platformID: string,
  auth: PlatformAuthConfig,
) {
  const targetURL = pickAuthEntryURL(auth);
  if (!targetURL) throw new Error("平台配置缺少入口页面地址");
  await openLocalPage(agentBase, {
    persistent: true,
    platform_id: platformID,
    user_data_dir: "default",
    headless: false,
    humanize: true,
    url: targetURL,
  });
}

/** confirmPlatformLoggedInForPosition 快速确认岗位运行平台是否已登录。 */
export async function confirmPlatformLoggedInForPosition(
  agentBase: string,
  auth: PlatformAuthConfig,
  onStatus: (message: string) => void,
) {
  let loggedInHits = 0;
  for (let index = 0; index < POSITION_LOGIN_CHECK_TIMES; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS);
    const url = await currentLocalPageURL(agentBase);
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1;
      onStatus(`正在确认平台登录状态 ${loggedInHits}/${POSITION_LOGIN_CHECK_TIMES}`);
      continue;
    }
    loggedInHits = 0;
    onStatus(
      isLoginURL(url, auth)
        ? "招聘平台还停在登录页"
        : `招聘平台还没确认登录：${shortURL(url)}`,
    );
  }
  if (loggedInHits < POSITION_LOGIN_CHECK_TIMES) {
    throw new Error("招聘平台还没登录，请先打开浏览器完成登录，再回来开始岗位运行。");
  }
}

/** waitForPlatformLoggedIn 连续确认当前页面命中已登录规则后返回。 */
export async function waitForPlatformLoggedIn(
  agentBase: string,
  auth: PlatformAuthConfig,
  onStatus: (message: string) => void,
  signal?: AbortSignal,
) {
  let loggedInHits = 0;
  onStatus("正在确认招聘平台页面；没看到窗口时，请重新打开本次招聘登录窗口。");
  await delay(URL_FIRST_CHECK_DELAY_MS, signal);
  for (let index = 0; index < 180; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS, signal);
    const url = await currentLocalPageURL(agentBase);
    signal?.throwIfAborted();
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1;
      onStatus(
        `正在确认登录状态 ${loggedInHits}/${LOGIN_SUCCESS_CONFIRM_TIMES}`,
      );
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) return;
      continue;
    }
    loggedInHits = 0;
    if (isLoginURL(url, auth)) {
      onStatus("请在招聘浏览器中完成扫码或验证码登录");
    } else if (isPlatformPageHost(url, auth)) {
      onStatus("招聘平台页面正在跳转；请在招聘浏览器中完成登录，我会继续确认。");
    } else {
      onStatus("暂未找到所选招聘平台页面。请重新打开本次招聘登录窗口，完成账号登录。");
    }
  }
  throw new Error("这次还没确认到平台登录。请重新打开本次招聘登录窗口，完成登录后再回来");
}

/** isPlatformPageHost 区分平台内正常跳转与空白、其他网站，只用于提示，不作为登录成功依据。 */
function isPlatformPageHost(url: string, auth: PlatformAuthConfig) {
  try {
    const current = new URL(url);
    if (!current.hostname) return false;
    return [...(auth.pages || []), ...(auth.public_pages || [])].some((page) => {
      try {
        const target = String(page?.url || "");
        const configured = new URL(target.startsWith("http") ? target : `https://${target}`);
        return current.hostname === configured.hostname;
      } catch {
        return false;
      }
    });
  } catch {
    return false;
  }
}

/** isLoggedInURL 先排除公开规则及明确登录路径，再按原有已登录规则判断，避免泛域名或回跳参数误报成功。 */
export function isLoggedInURL(url: string, auth: PlatformAuthConfig) {
  if (isLoginURL(url, auth)) return false;
  return (auth.pages || []).some((page) => matchPageURL(url, page));
}

/** isLoginURL 优先沿用平台公开规则，再保守排除完整登录路径，不把查询参数里的登录地址当页面路径。 */
export function isLoginURL(url: string, auth: PlatformAuthConfig) {
  return (auth.public_pages || []).some((page) => matchPageURL(url, page)) || hasExplicitLoginPath(url);
}

/** hasExplicitLoginPath 只识别URL路径中的完整登录段，兼容常见页后缀和编码，不猜测任何登录成功地址。 */
function hasExplicitLoginPath(url: string): boolean {
  try {
    for (const rawSegment of new URL(url).pathname.split("/")) {
      let segment = rawSegment;
      try { segment = decodeURIComponent(rawSegment); } catch { /* 某段编码损坏时保留原值，不能跳过其他明确的登录路径段。 */ }
      if (segment.split("/").some((part) => /^(?:log[-_]?in|sign[-_]?in|logon|signon)(?:\.(?:html?|aspx?|jsp|php))?$/i.test(part))) return true;
    }
    return false;
  } catch {
    return false;
  }
}

/** matchPageURL 按平台配置规则匹配页面地址。 */
function matchPageURL(currentURL: string, page: PlatformPageRule) {
  const target = String(page?.url || "").trim();
  if (!target || !currentURL) return false;
  const match =
    page.match || (target.startsWith("http") ? "prefix" : "contains");
  if (match === "exact") return currentURL === target;
  if (match === "contains")
    return currentURL.includes(target.replace(/^https?:\/\//, ""));
  return currentURL.startsWith(target);
}

/** delay 等待指定毫秒数。 */
function delay(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(new DOMException("登录等待已暂停", "AbortError")); return; }
    const timer = window.setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, ms);
    /** abort 在教程暂停或离开页面时释放等待，避免旧登录轮询继续占用操作。 */
    function abort() { window.clearTimeout(timer); reject(new DOMException("登录等待已暂停", "AbortError")); }
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/** shortURL 缩短页面地址用于状态提示。 */
function shortURL(url: string) {
  if (!url) return "空地址";
  return url.length > 72 ? `${url.slice(0, 72)}...` : url;
}
