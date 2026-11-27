/** 文件作用：核对月季年日均价、优惠说明、1.5元底线及空配置的安全展示。 */
import assert from "node:assert/strict";
import test from "node:test";
import { timePlanPricing } from "./ai-time-plan-pricing.ts";

const plans = [
  { id: "monthly", price_cents: 6900, duration_days: 30 },
  { id: "quarterly", price_cents: 17900, duration_days: 90 },
  { id: "yearly", price_cents: 54900, duration_days: 365 },
];

test("按天比较月季年优惠，全年日均实际不低于1.5元", () => {
  assert.deepEqual(timePlanPricing(plans[0], plans), { description: "额度无限 · 约￥2.30/天", bestValue: false });
  assert.deepEqual(timePlanPricing(plans[1], plans), { description: "额度无限 · 约￥1.99/天 · 比包月省约13.5%", bestValue: false });
  assert.deepEqual(timePlanPricing(plans[2], plans), { description: "额度无限 · 约￥1.50/天 · 比包月省约34.6%", bestValue: true });
  assert.ok(plans.every(plan => plan.price_cents >= 150 * plan.duration_days));
});

test("后台调价后不再使用写死的优惠，不把更贵的年套餐称为最划算", () => {
  const changed = plans.map(plan => plan.id === "yearly" ? { ...plan, price_cents: 99900 } : plan);
  assert.deepEqual(timePlanPricing(changed[2], changed), { description: "额度无限 · 约￥2.74/天", bestValue: false });
  assert.equal(timePlanPricing(changed[1], changed).bestValue, true);
});

test("套餐缺失、零天数及同价比较不产生虚假优惠或NaN", () => {
  assert.deepEqual(timePlanPricing(null, []), { description: "额度无限", bestValue: false });
  assert.deepEqual(timePlanPricing({ ...plans[0], duration_days: 0 }, plans), { description: "额度无限", bestValue: false });
  assert.equal(timePlanPricing(plans[2], []).bestValue, false);
  const equal = [{ id: "monthly", price_cents: 6000, duration_days: 30 }, { id: "quarterly", price_cents: 18000, duration_days: 90 }];
  assert.doesNotMatch(timePlanPricing(equal[1], equal).description, /省/);
});
