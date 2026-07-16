// 本文件负责测试任务启动时复用已有浏览器标签页的 URL 包含匹配规则。

import assert from "node:assert/strict";
import test from "node:test";

import { pageURLContainsTarget } from "./navigation-target.js";

/**
 * 测试带筛选参数的页面可以命中不带参数的任务目标地址。
 */
test("带筛选参数的页面可以复用", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://www.zhipin.com/web/chat/recommend?jobId=123&city=101010100",
      "https://www.zhipin.com/web/chat/recommend",
    ),
    true,
  );
});

/**
 * 测试目标地址末尾斜杠不会影响标签页复用。
 */
test("目标地址末尾斜杠不影响匹配", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://rd6.zhaopin.com/app/recommend?jobNumber=CC123",
      "https://rd6.zhaopin.com/app/recommend/",
    ),
    true,
  );
});

/**
 * 测试不同平台或不同目标路径不会误命中。
 */
test("不同目标路径不会复用", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://www.zhipin.com/web/geek/recommend",
      "https://www.zhipin.com/web/chat/recommend",
    ),
    false,
  );
});
