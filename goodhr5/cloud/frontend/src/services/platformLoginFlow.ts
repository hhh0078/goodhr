// 本文件负责封装创建平台账号时的本地登录检测和 cookie 导出流程。
import { currentPageURL, exportPageCookies, openPage, startBrowser } from './localAgentApi'

export type PlatformPageRule = {
  url: string
  title?: string
  match?: 'contains' | 'prefix' | 'exact'
  entry?: boolean
}

export type PlatformAuthConfig = {
  pages: PlatformPageRule[]
  public_pages: PlatformPageRule[]
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
  const entryUrl = pickAuthEntryURL(auth)
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
    const loginURL = isLoginURL(url, auth)
    const loggedInURL = isLoggedInURL(url, auth)
    if (loginURL) {
      sawLoginPage = true
      loggedInHits = 0
      onStatus('请在打开的浏览器中扫码登录')
      break
    }
    if (loggedInURL) {
      loggedInHits += 1
      onStatus(`正在确认登录状态 ${loggedInHits}/${LOGIN_SUCCESS_CONFIRM_TIMES}`)
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
        return exportCookiesAfterLogin(agentBaseUrl, onStatus, '已检测到登录状态')
      }
      continue
    }
    onStatus(`等待页面跳转到登录页或已登录页面：${shortURL(url)}`)
    loggedInHits = 0
  }
  if (!sawLoginPage) {
    if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
      return exportCookiesAfterLogin(agentBaseUrl, onStatus, '已检测到登录状态')
    }
    throw new Error('未确认登录状态，请重试')
  }
  loggedInHits = 0
  for (let index = 0; index < 180; index += 1) {
    await delay(URL_CHECK_INTERVAL_MS)
    const url = await currentPageURL(agentBaseUrl)
    if (isLoggedInURL(url, auth)) {
      loggedInHits += 1
      onStatus(`扫码后正在确认登录状态 ${loggedInHits}/${LOGIN_SUCCESS_CONFIRM_TIMES}`)
      if (loggedInHits >= LOGIN_SUCCESS_CONFIRM_TIMES) {
        return exportCookiesAfterLogin(agentBaseUrl, onStatus, '登录成功')
      }
      continue
    }
    onStatus(`等待扫码登录完成：${shortURL(url)}`)
    loggedInHits = 0
  }
  throw new Error('扫码登录超时')
}

function isLoginURL(url: string, auth: PlatformAuthConfig) {
  return (auth.public_pages || []).some(page => matchPageURL(url, page))
}

function isLoggedInURL(url: string, auth: PlatformAuthConfig) {
  return (auth.pages || []).some(page => matchPageURL(url, page))
}

function delay(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}

/**
 * 缩短 URL 用于界面状态展示。
 * @param {string} url - 完整页面 URL。
 * @returns {string} 缩短后的 URL。
 */
function shortURL(url: string) {
  if (!url) return '空地址'
  return url.length > 72 ? `${url.slice(0, 72)}...` : url
}

/**
 * 从 auth.pages 中选择默认打开的登录后页面。
 * @param {PlatformAuthConfig} auth - 平台登录配置。
 * @returns {string} 可导航页面 URL。
 */
export function pickAuthEntryURL(auth: PlatformAuthConfig) {
  const pages = auth.pages || []
  const page = pages.find(item => item.entry && item.url) || pages.find(item => item.url)
  return page?.url || ''
}

/**
 * 按页面规则判断当前 URL 是否命中。
 * @param {string} currentURL - 当前浏览器 URL。
 * @param {PlatformPageRule} page - 平台页面规则。
 * @returns {boolean} 是否命中。
 */
function matchPageURL(currentURL: string, page: PlatformPageRule) {
  const target = String(page?.url || '').trim()
  if (!target || !currentURL) return false
  const match = page.match || (target.startsWith('http') ? 'prefix' : 'contains')
  if (match === 'exact') return currentURL === target
  if (match === 'contains') return currentURL.includes(target.replace(/^https?:\/\//, ''))
  return currentURL.startsWith(target)
}

/**
 * 登录确认后调用本地程序导出 cookie，并输出可见状态。
 * @param {string} agentBaseUrl - Local Agent HTTP 基础地址。
 * @param {(message: string) => void} onStatus - 状态提示回调。
 * @param {string} reason - 触发导出的登录判断原因。
 * @returns {Promise<any[]>} 返回导出的 cookies。
 */
async function exportCookiesAfterLogin(agentBaseUrl: string, onStatus: (message: string) => void, reason: string) {
  onStatus(`${reason}，正在请求本地程序导出 cookie`)
  const cookies = await exportPageCookies(agentBaseUrl)
  onStatus(`本地程序已导出 ${cookies.length} 条 cookie`)
  if (!Array.isArray(cookies) || cookies.length === 0) {
    throw new Error('本地程序没有导出 cookie，请确认浏览器仍处于登录状态')
  }
  return cookies
}
