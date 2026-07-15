/** 本文件验证候选人详情容器的100毫秒轮询和5秒超时规则。 */

import assert from "node:assert/strict";
import test from "node:test";

import { waitForDetailContainer } from "./detail-ready.js";

test("详情容器出现后立即继续", async () => {
  let clock = 0;
  let searches = 0;
  const result = await waitForDetailContainer({
    timeoutMs: 5000,
    pollIntervalMs: 100,
    now: () => clock,
    wait: async (milliseconds) => {
      clock += milliseconds;
    },
    findVisible: async () => {
      searches += 1;
      return searches === 3 ? [{}] : [];
    },
  });
  assert.equal(result.ready, true);
  assert.equal(result.attempts, 3);
  assert.equal(result.elapsed_ms, 200);
});

test("5秒内始终找不到详情容器时返回失败", async () => {
  let clock = 0;
  const result = await waitForDetailContainer({
    timeoutMs: 5000,
    pollIntervalMs: 100,
    now: () => clock,
    wait: async (milliseconds) => {
      clock += milliseconds;
    },
    findVisible: async () => [],
  });
  assert.equal(result.ready, false);
  assert.equal(result.attempts, 50);
  assert.equal(result.elapsed_ms, 5000);
  assert.equal(result.reason, "未找到可见详情容器");
});
