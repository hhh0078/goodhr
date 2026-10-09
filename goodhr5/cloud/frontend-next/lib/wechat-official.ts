/** 本文件定义公众号只读状态、绑定码及私有配置界面共用的基础转换。 */
export type WechatOfficialStatus = {
  enabled: boolean;
  configured: boolean;
  account_name: string;
  original_id: string;
  qr_image_url: string;
  reward_days: number;
  bound: boolean;
  followed: boolean;
  gift_granted: boolean;
};

/** normalizeWechatStatus 将接口字段转为稳定界面状态，缺失配置时不自动显示关注提示。 */
export function normalizeWechatStatus(value: unknown): WechatOfficialStatus {
  const source =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : {};
  return {
    enabled: source.enabled === true,
    configured: source.configured === true,
    account_name: String(source.account_name || "GoodHR"),
    original_id: String(source.original_id || ""),
    qr_image_url: String(source.qr_image_url || ""),
    reward_days: Number(source.reward_days) || 5,
    bound: source.bound === true,
    followed: source.followed === true,
    gift_granted: source.gift_granted === true,
  };
}

/** officialAccountQRURL 为已配置原始 ID 生成官方关注二维码图片地址，不重新编码图片链接。 */
export function officialAccountQRURL(originalID: string) {
  const normalized = originalID.trim();
  return normalized
    ? `https://open.weixin.qq.com/qr/code?username=${encodeURIComponent(normalized)}`
    : "";
}

/** readableImageURL 只允许 HTTPS 或站内图片，避免把非图片协议作为二维码加载。 */
export function readableImageURL(value: string) {
  const normalized = value.trim();
  return /^https:\/\//i.test(normalized) || /^\/(?!\/)/.test(normalized)
    ? normalized
    : "";
}
