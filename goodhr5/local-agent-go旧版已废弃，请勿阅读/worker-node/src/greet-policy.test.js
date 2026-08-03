// 本文件负责验证各招聘平台打招呼后的追加点击策略。

import assert from "node:assert/strict";
import test from "node:test";

import { shouldClickGreetFollowups } from "./greet-policy.js";

test("智联首次打招呼后不再点击继续聊天", () => {
  assert.equal(shouldClickGreetFollowups("zhaopin"), false);
  assert.equal(shouldClickGreetFollowups(" ZHAOPIN "), false);
});

test("Boss 保留原有继续和确认按钮处理", () => {
  assert.equal(shouldClickGreetFollowups("boss"), true);
  assert.equal(shouldClickGreetFollowups(""), true);
});
