/** 本文件验证统一分段拟人输入的拆分、字符延时和段间停顿规则。 */

import assert from "node:assert/strict";
import test from "node:test";

import { humanTypeText } from "./human-type.js";

test("中文内容按一到两个字符分段并加入随机停顿", async () => {
  const typed = [];
  const waits = [];
  const keyboard = {
    // type 记录每段文字和字符输入延时，避免测试真实浏览器。
    async type(text, options) {
      typed.push({ text, delay: options.delay });
    },
  };
  const result = await humanTypeText(keyboard, "你好世界", {
    random: () => 0,
    wait: async (milliseconds) => waits.push(milliseconds),
  });

  assert.deepEqual(typed, [
    { text: "你", delay: 25 },
    { text: "好", delay: 25 },
    { text: "世", delay: 25 },
    { text: "界", delay: 25 },
  ]);
  assert.deepEqual(waits, [80, 80, 80]);
  assert.deepEqual(result, { chars: 4, chunks: 4 });
});

test("明确输入速度时保持固定字符延时并允许两字一段", async () => {
  const typed = [];
  const keyboard = {
    // type 记录兼容参数传入后的实际输入结果。
    async type(text, options) {
      typed.push({ text, delay: options.delay });
    },
  };
  const result = await humanTypeText(keyboard, "联系候选人", {
    chunk_min: 2,
    chunk_max: 2,
    typing_delay_ms: 40,
    delay_min_ms: 0,
    delay_max_ms: 0,
    random: () => 0.5,
  });

  assert.deepEqual(typed, [
    { text: "联系", delay: 40 },
    { text: "候选", delay: 40 },
    { text: "人", delay: 40 },
  ]);
  assert.deepEqual(result, { chars: 5, chunks: 3 });
});

test("空内容不会触发键盘输入", async () => {
  let calls = 0;
  const result = await humanTypeText(
    { async type() { calls += 1; } },
    "",
  );
  assert.equal(calls, 0);
  assert.deepEqual(result, { chars: 0, chunks: 0 });
});
