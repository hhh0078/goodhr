// 文件作用说明：验证公共输入会把完整文本交给 CloakBrowser，并安全隔离特殊符号。

import assert from "node:assert/strict";
import test from "node:test";
import { safeTypingChunks } from "../dist/browser/actions/input.js";
import { ReadPrimitive } from "../dist/browser/primitives/read.js";

test("普通中英文内容会完整交给 CloakBrowser", () => {
  const text = "招生教务老师";
  assert.deepEqual(safeTypingChunks(text), [
    { text, cloakbrowser: true },
  ]);
});

test("Shift 特殊符号会与 CloakBrowser 输入片段隔离且不丢字", () => {
  const text = "Hello:你好？";
  const chunks = safeTypingChunks(text);
  assert.equal(chunks.map((item) => item.text).join(""), text);
  assert.deepEqual(chunks, [
    { text: "Hello", cloakbrowser: true },
    { text: ":", cloakbrowser: false },
    { text: "你好？", cloakbrowser: true },
  ]);
});

test("富文本输入框使用可见文本验证输入结果", async () => {
  const reader = new ReadPrimitive();
  const value = await reader.editableValue({
    async inputValue() {
      throw new Error("contenteditable 不支持 inputValue");
    },
    async innerText() {
      return "你好";
    },
  });
  assert.equal(value, "你好");
});
