/** 文件作用说明：验证自动回复仅开放三个指定平台，缺失和未知平台不能误放行。 */
import assert from "node:assert/strict";
import test from "node:test";
import { AUTO_REPLY_ROLLOUT_CLOSED, AUTO_REPLY_TEST_NOTICE, isAutoReplyPlatformSupported } from "./auto-reply-rollout.ts";

test("仅BOSS、猎聘企业端和智联开放测试", () => {
  assert.equal(AUTO_REPLY_ROLLOUT_CLOSED, false);
  for (const id of ["boss", "liepin", "zhaopin", " BOSS "]) assert.equal(isAutoReplyPlatformSupported(id), true);
  for (const id of ["hliepin", "51job", "unknown", "boss.example", "", null, undefined]) assert.equal(isAutoReplyPlatformSupported(id), false);
  assert.match(AUTO_REPLY_TEST_NOTICE, /测试中/);
  assert.match(AUTO_REPLY_TEST_NOTICE, /作者微信/);
});
