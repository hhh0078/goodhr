/** 本文件负责前端统一解析会员状态、功能权限和 Plus 升级 Max 的预计抵扣金额。 */

export type SubscriptionStatus = {
  active: boolean;
  member_type: string;
  member_name: string;
  expires_at: string;
  remaining_days: number;
  remaining_seconds: number;
  allow_ai: boolean;
  allow_auto_reply: boolean;
  features: string[];
};

export type SubscriptionPlan = {
  id: string;
  name: string;
  member_type: string;
  duration_days: number;
  original_price: number;
  discount_amount: number;
  allow_auto_reply: boolean;
  features: string[];
  description: string;
  recommended?: boolean;
};

export type SubscriptionUpgradeQuote = {
  amountCents: number;
  creditCents: number;
  upgrade: boolean;
};

export const EMPTY_SUBSCRIPTION: SubscriptionStatus = {
  active: false,
  member_type: "free",
  member_name: "免费版",
  expires_at: "",
  remaining_days: 0,
  remaining_seconds: 0,
  allow_ai: false,
  allow_auto_reply: false,
  features: [],
};

/** normalizeSubscription 把接口中的未知会员数据转换为安全的强类型状态。 */
export function normalizeSubscription(value: unknown): SubscriptionStatus {
  const source = recordValue(value);
  const memberType = String(source.member_type || "free").trim().toLowerCase();
  const active = Boolean(source.active);
  return {
    active,
    member_type: memberType,
    member_name:
      String(source.member_name || "").trim() || membershipName(memberType),
    expires_at: String(source.expires_at || ""),
    remaining_days: nonNegativeNumber(source.remaining_days),
    remaining_seconds: nonNegativeNumber(source.remaining_seconds),
    allow_ai: active && source.allow_ai !== false,
    allow_auto_reply: active && Boolean(source.allow_auto_reply),
    features: stringList(source.features),
  };
}

/** normalizeSubscriptionPlans 把套餐接口转换为安全的强类型数组。 */
export function normalizeSubscriptionPlans(value: unknown): SubscriptionPlan[] {
  if (!Array.isArray(value)) return [];
  return value
    .map((item) => {
      const source = recordValue(item);
      return {
        id: String(source.id || "").trim(),
        name: String(source.name || "会员套餐").trim(),
        member_type: String(source.member_type || "free")
          .trim()
          .toLowerCase(),
        duration_days: nonNegativeNumber(source.duration_days),
        original_price: nonNegativeNumber(source.original_price),
        discount_amount: nonNegativeNumber(source.discount_amount),
        allow_auto_reply: Boolean(source.allow_auto_reply),
        features: stringList(source.features),
        description: String(source.description || "").trim(),
        recommended: Boolean(source.recommended),
      };
    })
    .filter((plan) => plan.id);
}

/** canUseAI 判断当前会员是否允许使用 AI 岗位能力。 */
export function canUseAI(subscription: SubscriptionStatus) {
  return subscription.active && subscription.allow_ai;
}

/** canUseAutoReply 判断当前会员是否允许使用自动回复。 */
export function canUseAutoReply(subscription: SubscriptionStatus) {
  return subscription.active && subscription.allow_auto_reply;
}

/** membershipName 返回会员类型对应的中文名称。 */
export function membershipName(memberType: unknown) {
  switch (String(memberType || "").trim().toLowerCase()) {
    case "max":
      return "Max 全能版";
    case "plus":
      return "Plus 基础版";
    default:
      return "免费版";
  }
}

/** estimateSubscriptionQuote 预计普通购买或 Plus 升级 Max 的实付与抵扣金额。 */
export function estimateSubscriptionQuote(
  subscription: SubscriptionStatus,
  plans: SubscriptionPlan[],
  target: SubscriptionPlan,
): SubscriptionUpgradeQuote {
  const targetCents = planPriceCents(target);
  if (
    !subscription.active ||
    subscription.member_type !== "plus" ||
    target.member_type !== "max"
  ) {
    return { amountCents: targetCents, creditCents: 0, upgrade: false };
  }
  const plus = plans.find((plan) => plan.member_type === "plus");
  if (!plus || plus.duration_days <= 0) {
    return { amountCents: targetCents, creditCents: 0, upgrade: true };
  }
  const plusCents = planPriceCents(plus);
  const periodSeconds = plus.duration_days * 24 * 60 * 60;
  const creditCents = Math.min(
    targetCents,
    Math.max(
      0,
      Math.floor(
        (plusCents * subscription.remaining_seconds) / periodSeconds,
      ),
    ),
  );
  return {
    amountCents: targetCents - creditCents,
    creditCents,
    upgrade: true,
  };
}

/** isPlanDowngradeBlocked 判断有效 Max 是否正在尝试购买 Plus。 */
export function isPlanDowngradeBlocked(
  subscription: SubscriptionStatus,
  plan: SubscriptionPlan,
) {
  return (
    subscription.active &&
    subscription.member_type === "max" &&
    plan.member_type === "plus"
  );
}

/** planPriceCents 返回套餐优惠后的分金额。 */
export function planPriceCents(plan: SubscriptionPlan) {
  return Math.max(
    0,
    Math.round((plan.original_price - plan.discount_amount) * 100),
  );
}

/** recordValue 把未知值安全转换为普通对象。 */
function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object"
    ? (value as Record<string, unknown>)
    : {};
}

/** nonNegativeNumber 把未知数值转换为非负有限数字。 */
function nonNegativeNumber(value: unknown) {
  const parsed = Number(value || 0);
  return Number.isFinite(parsed) ? Math.max(0, parsed) : 0;
}

/** stringList 把未知数组转换为无空项字符串数组。 */
function stringList(value: unknown) {
  return Array.isArray(value)
    ? value.map((item) => String(item).trim()).filter(Boolean)
    : [];
}
