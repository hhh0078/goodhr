// 文件作用说明：验证滚动状态优先读取鼠标落点最近的可滚动父级。

import assert from "node:assert/strict";
import test from "node:test";

import { ReadPrimitive } from "../dist/browser/primitives/read.js";

test("读取最近可滚动父级而不是误读整页", async () => {
  const scrollParent = {
    scrollLeft: 0,
    scrollTop: 180,
    scrollWidth: 640,
    scrollHeight: 1800,
    clientWidth: 640,
    clientHeight: 600,
    parentElement: null,
  };
  const anchor = {
    scrollLeft: 0,
    scrollTop: 0,
    scrollWidth: 120,
    scrollHeight: 40,
    clientWidth: 120,
    clientHeight: 40,
    parentElement: scrollParent,
  };
  globalThis.window = {
    getComputedStyle(element) {
      return element === scrollParent
        ? { overflowY: "auto", overflowX: "hidden" }
        : { overflowY: "visible", overflowX: "visible" };
    },
  };
  const locator = {
    evaluate(callback) {
      return callback(anchor);
    },
  };
  try {
    const state = await new ReadPrimitive().scrollState({}, locator);
    assert.equal(state.source, "anchor_parent");
    assert.equal(state.scroll_top, 180);
    assert.equal(state.scroll_height, 1800);
    assert.equal(state.client_height, 600);
  } finally {
    delete globalThis.window;
  }
});
