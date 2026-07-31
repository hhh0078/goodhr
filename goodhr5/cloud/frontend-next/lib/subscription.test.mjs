/** 本文件负责验证前端会员权限和 Plus 升级 Max 的预计抵扣金额。 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  canUseAutoReply,
  estimateSubscriptionQuote,
  isPlanDowngradeBlocked,
  normalizeSubscription,
  normalizeSubscriptionPlans,
} from "./subscription.ts";

const plans = normalizeSubscriptionPlans([
  {
    id: "monthly",
    name: "基础包月版",
    member_type: "plus",
    duration_days: 30,
    original_price: 70,
    discount_amount: 30,
    allow_auto_reply: false,
  },
  {
    id: "yearly",
    name: "全能包年版",
    member_type: "max",
    duration_days: 365,
    original_price: 840,
    discount_amount: 500,
    allow_auto_reply: true,
  },
]);

test("Plus 剩余十五天升级 Max 时抵扣二十元", () => {
  const subscription = normalizeSubscription({
    active: true,
    member_type: "plus",
    remaining_seconds: 15 * 24 * 60 * 60,
    allow_ai: true,
    allow_auto_reply: false,
  });
  const quote = estimateSubscriptionQuote(subscription, plans, plans[1]);
  assert.equal(quote.creditCents, 2000);
  assert.equal(quote.amountCents, 32000);
});

test("自动回复只接受后端返回的明确权限", () => {
  assert.equal(
    canUseAutoReply(
      normalizeSubscription({
        active: true,
        member_type: "plus",
        allow_auto_reply: false,
      }),
    ),
    false,
  );
  assert.equal(
    canUseAutoReply(
      normalizeSubscription({
        active: true,
        member_type: "max",
        allow_auto_reply: true,
      }),
    ),
    true,
  );
});

test("有效 Max 不能购买 Plus", () => {
  const subscription = normalizeSubscription({
    active: true,
    member_type: "max",
    allow_ai: true,
    allow_auto_reply: true,
  });
  assert.equal(isPlanDowngradeBlocked(subscription, plans[0]), true);
});
