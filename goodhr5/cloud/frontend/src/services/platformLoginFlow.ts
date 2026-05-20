// 本文件负责封装创建平台账号时的本地登录检测和 cookie 导出流程。
import { currentPageURL, exportPageCookies, openPage, startBrowser } from './localAgentApi'

type PlatformAuthConfig = {
  entry_url?: string
  logged_in_url_prefix?: string
  login_url_prefixes?: string[]
}

/**
 * 执行 Boss 等招聘平台的扫码登录检测流程。
 * @param {string} agentBaseUrl - Local Agent HTTP 基础地址。
 * @param {string} platformId - 平台 ID。
 * @param {PlatformAuthConfig} auth - 平台登录配置。
 * @param {(message: string) => void} onStatus - 状态提示回调。
 * @returns {Promise<any[]>} 返回登录后的 cookies 数组。
 */
export async function runPlatformLoginFlow(agentBaseUrl: string, platformId: string, auth: PlatformAuthConfig, onStatus: (message: string) => void) {
  if (!agentBaseUrl) throw new Error('未检测到本地程序')
  const entryUrl = auth.entry_url || auth.logged_in_url_prefix
  if (!entryUrl) throw new Error('平台登录配置缺少入口地址')
  await startBrowser(agentBaseUrl, {
    persistent: true,
    user_data_dir: `platform_${platformId}`,
    headless: false,
    humanize: true,
  })
  await openPage(agentBaseUrl, { url: entryUrl })
  let sawLoginPage = false
  for (let index = 0; index < 10; index += 1) {
    await delay(1000)
    const url = await currentPageURL(agentBaseUrl)
    if (isLoginURL(url, auth)) {
      sawLoginPage = true
      onStatus('请在打开的浏览器中扫码登录')
      break
    }
    if (isLoggedInURL(url, auth)) {
      onStatus('已检测到登录状态，正在导出 cookie')
      return exportPageCookies(agentBaseUrl)
    }
  }
  if (!sawLoginPage) {
    onStatus('已检测到登录状态，正在导出 cookie')
    return exportPageCookies(agentBaseUrl)
  }
  for (let index = 0; index < 180; index += 1) {
    await delay(1000)
    const url = await currentPageURL(agentBaseUrl)
    if (isLoggedInURL(url, auth)) {
      onStatus('登录成功，正在导出 cookie')
      return exportPageCookies(agentBaseUrl)
    }
  }
  throw new Error('扫码登录超时')
}

function isLoginURL(url: string, auth: PlatformAuthConfig) {
  return (auth.login_url_prefixes || []).some(prefix => url.startsWith(prefix))
}

function isLoggedInURL(url: string, auth: PlatformAuthConfig) {
  return !!auth.logged_in_url_prefix && url.startsWith(auth.logged_in_url_prefix)
}

function delay(ms: number) {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}
