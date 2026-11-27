/** 文件作用：按后台真实套餐价格和天数计算日均价及相对包月优惠，不修改AI额度与扣费。 */
export type AITimePlanPrice = { id: string; price_cents: number; duration_days: number };

/** validPrice 排除空值、非法价格及零天数，避免套餐展示NaN或误报优惠。 */
function validPrice(plan: AITimePlanPrice | null | undefined): plan is AITimePlanPrice {
  return Boolean(plan && Number.isSafeInteger(plan.price_cents) && plan.price_cents > 0
    && Number.isSafeInteger(plan.duration_days) && plan.duration_days > 0);
}

/** timePlanPricing 按相同天数比较真实价格，给更长且更便宜的套餐标明省钱比例。 */
export function timePlanPricing(plan: AITimePlanPrice, plans: AITimePlanPrice[]) {
  if (!validPrice(plan)) return { description: "额度无限", bestValue: false };
  const valid = (Array.isArray(plans) ? plans : []).filter(validPrice);
  const monthly = valid.find(item => item.id === "monthly") || valid.find(item => item.duration_days === 30);
  const dailyPrice = plan.price_cents / plan.duration_days;
  const saving = monthly && plan.duration_days > monthly.duration_days
    ? 100 * (1 - plan.price_cents * monthly.duration_days / (monthly.price_cents * plan.duration_days)) : 0;
  const bestValue = saving > 0 && valid.every(item => dailyPrice <= item.price_cents / item.duration_days);
  const parts = ["额度无限", `约￥${(dailyPrice / 100).toFixed(2)}/天`];
  if (saving >= 0.1) parts.push(`比包月省约${saving.toFixed(1).replace(/\.0$/, "")}%`);
  return { description: parts.join(" · "), bestValue };
}
