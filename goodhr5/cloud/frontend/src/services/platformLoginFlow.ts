// 本文件负责封装创建平台账号时的本地登录检测和 cookie 导出流程。
import { currentPageURL, exportPageCookies, openPage, startBrowser } from './localAgentApi'

type PlatformAuthConfig = {
  entry_url?: string
  logged_in_url_prefix?: string
  logged_in_url_contains?: string[]
  login_url_prefixes?: string[]
}

type PlatformLoginFlowOptions = {
  userDataDir?: string
  cookieSync?: {
    cookie_id?: string
    platform_id: string
    display_name: string
    cloud_api_base: string
  }
}

const URL_CHECK_INTERVAL_MS = 3000
const LOGIN_SUCCESS_CONFIRM_TIMES = 3

/**
 * 执行 Boss 等招聘平台的扫码登录检测流程。
 * @param {string} agentBaseUrl - Local Agent HTTP 基础地址。
 * @param {string} platformId - 平台 ID。
 * @param {PlatformAuthConfig} auth - 平台登录配置。
 * @param {(message: string) => void} onStatus - 状态提示回调。
 * @param {PlatformLoginFlowOptions} options - 可选流程参数，用于指定本地浏览器目录。
 * @returns {Promise<any[]>} 返回登录后的 cookies 数组。
 */
export async function runPlatformLoginFlow(agentBaseUrl: string, platformId: string, auth: PlatformAuthConfig, onStatus: (message: string) => void, options: PlatformLoginFlowOptions = {}) {
  if (!agentBaseUrl) throw new Error('未检测到本地程序')
  const entryUrl = auth.entry_url || auth.logged_in_url_prefix
  if (!entryUrl) throw new Error('平台登录配置缺少入口地址')
  await startBrowser(agentBaseUrl, {
    persistent: true,
    user_data_dir: options.userDataDir || `platform_${platformId}`,
    headless: false,
    humanize: true,
    cookie_sync: options.cookieSync,
  })
  await openPage(agentBaseUrl, { url: entryUrl, cookie_sync: options.cookieSync })
  let sawLoginPage = false
  let loggedInHits = 0
  for (let index = 0; index < 10; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS)
    const url = await currentPageURL(agentBaseUrl)
    if (isLoginURL(url, auth)) {
      sawLoginPage = true
      loggedInHits = 0
      onStatus('请在打开的浏览器中扫码登录')
      break
    }
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
        onStatus('已检测到登录状态，正在导出 cookie')
        return exportPageCookies(agentBaseUrl)
      }
      continue
    }
    loggedInHits = 0
  }
  if (!sawLoginPage) {
    if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
      onStatus('已检测到登录状态，正在导出 cookie')
      return exportPageCookies(agentBaseUrl)
    }
    throw new Error('未确认登录状态，请重试')
  }
  loggedInHits = 0
  for (let index = 0; index < 180; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS)
    const url = await currentPageURL(agentBaseUrl)
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
        onStatus('登录成功，正在导出 cookie')
        return exportPageCookies(agentBaseUrl)
      }
      continue
    }
    loggedInHits = 0
  }
  throw new Error('扫码登录超时')
}

function isLoginURL(url: string, auth: PlatformAuthConfig) {
  return (auth.login_url_prefixes || []).some(prefix => url.startsWith(prefix))
}

function isLoggedInURL(url: string, auth: PlatformAuthConfig) {
  const contains = auth.logged_in_url_contains || []
  if (contains.some(keyword => keyword && url.includes(keyword))) return true
  return !!auth.logged_in_url_prefix && url.startsWith(auth.logged_in_url_prefix)
}

function delay(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}
