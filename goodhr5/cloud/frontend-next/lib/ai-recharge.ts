/** 本文件负责 AI 充值活动的金额解析、赠送预估和局部系统配置编辑。 */
export type AIRechargePromotion = {
  amount_cents: number;
  bonus_cents: number;
};

export const AI_RECHARGE_POLICY =
  "充值后可随时退款，赠送的余额不可退款，赠送的金额将在最后使用。";
export const AI_RECHARGE_REFUND_POLICY =
  "退款会收回该笔充值剩余的赠送余额。";
export const AI_RECHARGE_CONFIG_KEY = "system.ai_recharge_promotions";
export const MIN_AI_RECHARGE_CENTS = 100;
export const MAX_AI_RECHARGE_CENTS = 100000;
export const MAX_AI_RECHARGE_PROMOTIONS = 50;

/** rechargeAmountCents 将最多两位小数的元金额准确转换为分，无效金额返回空值。 */
export function rechargeAmountCents(value: string | number | null): number | null {
  const text = String(value ?? "").trim();
  if (!/^\d+(?:\.\d{1,2})?$/.test(text)) return null;
  const [yuan, decimal = ""] = text.split(".");
  const cents = Number(yuan) * 100 + Number(decimal.padEnd(2, "0"));
  return Number.isSafeInteger(cents) && cents > 0 ? cents : null;
}

/** normalizeAIRechargePromotions 仅展示有效活动，并按充值门槛从低到高排序。 */
export function normalizeAIRechargePromotions(value: unknown): AIRechargePromotion[] {
  if (!Array.isArray(value)) return [];
  return value
    .filter((item): item is AIRechargePromotion =>
      Boolean(item) &&
      Number.isSafeInteger(item.amount_cents) && item.amount_cents >= MIN_AI_RECHARGE_CENTS && item.amount_cents <= MAX_AI_RECHARGE_CENTS &&
      Number.isSafeInteger(item.bonus_cents) && item.bonus_cents > 0 && item.bonus_cents <= MAX_AI_RECHARGE_CENTS,
    )
    .map((item) => ({ amount_cents: item.amount_cents, bonus_cents: item.bonus_cents }))
    .sort((a, b) => a.amount_cents - b.amount_cents);
}

/** rechargeBonusCents 按最高已达到的充值门槛预估赠送，多个活动不叠加。 */
export function rechargeBonusCents(amount: string, promotions: AIRechargePromotion[]): number {
  const cents = rechargeAmountCents(amount);
  if (cents === null) return 0;
  let matched: AIRechargePromotion | undefined;
  for (const promotion of promotions) {
    if (promotion.amount_cents <= cents &&
      (!matched || promotion.amount_cents > matched.amount_cents)) matched = promotion;
  }
  return matched?.bonus_cents || 0;
}

/** rechargeYuanLabel 将分金额转换为适合按钮和活动选项的简短元金额。 */
export function rechargeYuanLabel(cents: number): string {
  return (cents / 100).toFixed(2).replace(/\.00$/, "").replace(/(\.\d)0$/, "$1");
}

/** parseAIRechargeConfig 读取系统 JSON 对象，无效内容返回空值以防覆盖其他配置。 */
export function parseAIRechargeConfig(value: string): Record<string, unknown> | null {
  try {
    const config = JSON.parse(value);
    return config && typeof config === "object" && !Array.isArray(config) ? config : null;
  } catch {
    return null;
  }
}

/** updateAIRechargePromotions 仅替换充值活动字段，完整保留其他系统配置。 */
export function updateAIRechargePromotions(value: string, promotions: unknown[]): string | null {
  const config = parseAIRechargeConfig(value);
  if (!config) return null;
  return JSON.stringify({ ...config, ai_recharge_promotions: promotions }, null, 2);
}

/** validateAIRechargePromotions 校验活动金额及重复门槛，避免保存有歧义的充值配置。 */
export function validateAIRechargePromotions(value: string): string {
  const config = parseAIRechargeConfig(value);
  if (!config) return "充值活动配置需要是 JSON 对象。";
  const promotions = config.ai_recharge_promotions;
  if (promotions === undefined) return "";
  if (!Array.isArray(promotions)) return "充值活动需要是数组，停用全部活动请填空数组。";
  if (promotions.length > MAX_AI_RECHARGE_PROMOTIONS) return "充值活动最多配置 50 个。";
  const thresholds = new Set<number>();
  for (const [index, item] of promotions.entries()) {
    if (!item || !Number.isSafeInteger(item.amount_cents) || item.amount_cents <= 0 ||
      !Number.isSafeInteger(item.bonus_cents) || item.bonus_cents <= 0)
      return `第 ${index + 1} 个活动的充值和赠送金额都需要大于 0，最多保留两位小数。`;
    if (item.amount_cents < MIN_AI_RECHARGE_CENTS || item.amount_cents > MAX_AI_RECHARGE_CENTS || item.bonus_cents > MAX_AI_RECHARGE_CENTS)
      return `第 ${index + 1} 个活动的充值门槛需在 1–1000 元之间，赠送金额不能超过 1000 元。`;
    if (thresholds.has(item.amount_cents)) return `第 ${index + 1} 个活动的充值门槛重复了。`;
    thresholds.add(item.amount_cents);
  }
  return "";
}
