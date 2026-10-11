/** 本文件验证充值门槛、金额精度和活动配置保存对原有系统设置的保护。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  normalizeAIRechargePromotions,
  rechargeAmountCents,
  rechargeBonusCents,
  updateAIRechargePromotions,
  validateAIRechargePromotions,
} from "./ai-recharge.ts";

const promotions = normalizeAIRechargePromotions([
  { amount_cents: 20000, bonus_cents: 5000 },
  { amount_cents: 5000, bonus_cents: 500 },
  { amount_cents: 10000, bonus_cents: 2000 },
]);

test("150 元使用 100 元档赠送 20 元，达到更高门槛时替换而不叠加", () => {
  assert.equal(rechargeBonusCents("49.99", promotions), 0);
  assert.equal(rechargeBonusCents("50", promotions), 500);
  assert.equal(rechargeBonusCents("99.99", promotions), 500);
  assert.equal(rechargeBonusCents("100", promotions), 2000);
  assert.equal(rechargeBonusCents("150", promotions), 2000);
  assert.equal(rechargeBonusCents("200", promotions), 5000);
  assert.equal(rechargeBonusCents("1000", promotions), 5000);
});

test("人民币小数转换为整数分，不接受三位小数、非数值和溢出", () => {
  assert.equal(rechargeAmountCents("0.29"), 29);
  assert.equal(rechargeAmountCents("12.34"), 1234);
  assert.equal(rechargeAmountCents("0.01"), 1);
  for (const input of ["", "0", "-1", "NaN", "1.001", "1e2", "9007199254740992", null])
    assert.equal(rechargeAmountCents(input), null);
});

test("缺失活动或损坏的展示配置不产生赠送", () => {
  assert.deepEqual(normalizeAIRechargePromotions(null), []);
  assert.deepEqual(normalizeAIRechargePromotions({ amount_cents: 10000 }), []);
  assert.deepEqual(normalizeAIRechargePromotions([null, {}, { amount_cents: -1, bonus_cents: 20 }]), []);
  assert.equal(rechargeBonusCents("100", []), 0);
});

test("增删活动保留内置 AI 密钥、横幅和其他系统字段，清空数组停用活动", () => {
  const original = {
    builtin_ai: { api_key: "test-only-secret", models: [{ id: "qwen-test" }] },
    admin_banners: [{ title: "原有横幅" }],
    nested: { enabled: true },
  };
  const updated = JSON.parse(updateAIRechargePromotions(JSON.stringify(original), promotions));
  assert.deepEqual(updated, { ...original, ai_recharge_promotions: promotions });
  const cleared = JSON.parse(updateAIRechargePromotions(JSON.stringify(updated), []));
  assert.deepEqual(cleared, { ...original, ai_recharge_promotions: [] });
  assert.equal(updateAIRechargePromotions("{broken", []), null);
  assert.equal(updateAIRechargePromotions("[]", []), null);
});

test("活动配置拒绝空金额、分的小数及重复门槛，同时兼容尚未配置活动的历史数据", () => {
  assert.equal(validateAIRechargePromotions("{}"), "");
  assert.equal(validateAIRechargePromotions('{"ai_recharge_promotions":[]}'), "");
  assert.equal(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: promotions })), "");
  for (const item of [
    { amount_cents: null, bonus_cents: 2000 },
    { amount_cents: 0, bonus_cents: 2000 },
    { amount_cents: 10000, bonus_cents: 0 },
    { amount_cents: 10000, bonus_cents: 2.5 },
  ]) assert.notEqual(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: [item] })), "");
  assert.match(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: [promotions[0], promotions[0]] })), /重复/);
});

test("活动门槛遵守 1–1000 元范围，赠送不超过 1000 元且最多 50 档", () => {
  for (const item of [
    { amount_cents: 99, bonus_cents: 1 },
    { amount_cents: 100001, bonus_cents: 1 },
    { amount_cents: 100, bonus_cents: 100001 },
  ]) assert.notEqual(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: [item] })), "");
  const fifty = Array.from({ length: 50 }, (_, i) => ({ amount_cents: 100 + i, bonus_cents: 1 }));
  assert.equal(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: fifty })), "");
  assert.match(validateAIRechargePromotions(JSON.stringify({ ai_recharge_promotions: [...fifty, { amount_cents: 200, bonus_cents: 1 }] })), /最多配置 50/);
});
