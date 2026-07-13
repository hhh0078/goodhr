/** 本文件负责提供会员状态和免费版限制的前端判断工具。 */

const DEFAULT_FREE_DAILY_GREET_LIMIT = 100;

/**
 * 判断当前订阅是否有效。
 * @param {any} subscription - 会员订阅状态。
 * @returns {boolean} 有效会员返回 true。
 */
export function isSubscriptionActive(subscription: any) {
  if (!subscription || subscription.active === false) return false;
  const expiresAt = new Date(subscription.expires_at);
  if (Number.isNaN(expiresAt.getTime())) return Boolean(subscription.active);
  return Date.now() < expiresAt.getTime();
}

/**
 * 读取免费版每日打招呼上限。
 * @param {any} appConfig - 系统应用配置。
 * @returns {number} 免费版每日上限，默认 100。
 */
export function freeDailyGreetLimit(appConfig: any) {
  const value = Number(
    appConfig?.free_daily_greet_limit ??
      appConfig?.free_daily_greeting_limit ??
      appConfig?.free_daily_greet_count ??
      0,
  );
  if (!Number.isFinite(value) || value <= 0) return DEFAULT_FREE_DAILY_GREET_LIMIT;
  return Math.floor(value);
}
