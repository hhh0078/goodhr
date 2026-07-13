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
const TASK_LOGIN_CHECK_TIMES = 3;

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

/** parsePlatformAuthConfig 解析平台配置中的已登录页面和登录页面规则。 */
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
  const publicPages = parsed?.public?.pages;
  if (!Array.isArray(authPages) || authPages.length === 0) {
    throw new Error(`平台 ${platformID} 配置缺少 auth.pages`);
  }
  if (!Array.isArray(publicPages)) {
    throw new Error(`平台 ${platformID} 配置缺少 public.pages`);
  }
  return { pages: authPages, public_pages: publicPages };
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

/** openPlatformTaskBrowser 使用默认本地浏览器资料目录打开任务平台入口页。 */
export async function openPlatformTaskBrowser(
  agentBase: string,
  platformID: string,
  auth: PlatformAuthConfig,
) {
  const targetURL = pickAuthEntryURL(auth);
  if (!targetURL) throw new Error("平台配置缺少入口页面地址");
  await openLocalPage(agentBase, {
    persistent: true,
    platform_id: platformID,
    user_data_dir: `platform-${platformID || "default"}`,
    headless: false,
    humanize: true,
    url: targetURL,
  });
}

/** confirmPlatformLoggedInForTask 快速确认任务平台是否已登录。 */
export async function confirmPlatformLoggedInForTask(
  agentBase: string,
  auth: PlatformAuthConfig,
  onStatus: (message: string) => void,
) {
  let loggedInHits = 0;
  for (let index = 0; index < TASK_LOGIN_CHECK_TIMES; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS);
    const url = await currentLocalPageURL(agentBase);
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1;
      onStatus(`正在确认平台登录状态 ${loggedInHits}/${TASK_LOGIN_CHECK_TIMES}`);
      continue;
    }
    loggedInHits = 0;
    onStatus(
      isLoginURL(url, auth)
        ? "招聘平台还停在登录页"
        : `招聘平台还没确认登录：${shortURL(url)}`,
    );
  }
  if (loggedInHits < TASK_LOGIN_CHECK_TIMES) {
    throw new Error("招聘平台还没登录，请先打开浏览器完成登录，再回来开始任务。");
  }
}

/** waitForPlatformLoggedIn 连续确认当前页面命中已登录规则后返回。 */
export async function waitForPlatformLoggedIn(
  agentBase: string,
  auth: PlatformAuthConfig,
  onStatus: (message: string) => void,
) {
  let loggedInHits = 0;
  onStatus("登录页面加载中，请稍等...");
  await delay(URL_FIRST_CHECK_DELAY_MS);
  for (let index = 0; index < 180; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS);
    const url = await currentLocalPageURL(agentBase);
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1;
      onStatus(
        `正在确认登录状态 ${loggedInHits}/${LOGIN_SUCCESS_CONFIRM_TIMES}`,
      );
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) return;
      continue;
    }
    loggedInHits = 0;
    onStatus(
      isLoginURL(url, auth)
        ? "请在浏览器中完成扫码或验证码登录"
        : `等待登录页面跳转：${shortURL(url)}`,
    );
  }
  throw new Error("登录确认超时，请确认浏览器仍处于登录状态");
}

/** isLoggedInURL 判断当前地址是否命中平台已登录页面。 */
export function isLoggedInURL(url: string, auth: PlatformAuthConfig) {
  return (auth.pages || []).some((page) => matchPageURL(url, page));
}

/** isLoginURL 判断当前地址是否命中平台公开登录页面。 */
export function isLoginURL(url: string, auth: PlatformAuthConfig) {
  return (auth.public_pages || []).some((page) => matchPageURL(url, page));
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
function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

/** shortURL 缩短页面地址用于状态提示。 */
function shortURL(url: string) {
  if (!url) return "空地址";
  return url.length > 72 ? `${url.slice(0, 72)}...` : url;
}
