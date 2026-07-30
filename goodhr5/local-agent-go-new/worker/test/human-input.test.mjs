// 文件作用说明：验证公共输入能力会把中文内容拆成多个真人输入片段。

import assert from "node:assert/strict";
import test from "node:test";
import { humanTextSegments } from "../dist/browser/actions/input.js";
import { ReadPrimitive } from "../dist/browser/primitives/read.js";

test("中文岗位词会拆成多个输入片段且不丢字", () => {
  const text = "招生教务老师";
  const segments = humanTextSegments(text);
  assert.equal(segments.join(""), text);
  assert.ok(segments.length > 1);
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
