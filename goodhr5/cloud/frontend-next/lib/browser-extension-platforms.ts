/** 本文件统一维护浏览器简历插件已验证支持的平台名称，供后台和官网下载引导共用。 */

export const BROWSER_EXTENSION_SUPPORTED_PLATFORMS = [
  "BOSS直聘",
  "猎聘企业端",
  "猎聘猎头端",
  "智联招聘",
  "前程无忧",
] as const;

export const BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT =
  BROWSER_EXTENSION_SUPPORTED_PLATFORMS.join("、");
