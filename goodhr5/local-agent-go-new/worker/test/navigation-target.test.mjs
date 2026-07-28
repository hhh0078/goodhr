// 文件作用说明：验证打开页面时复用已有标签页的 URL 匹配规则。

import assert from "node:assert/strict";
import test from "node:test";

import { pageURLContainsTarget } from "../dist/browser/session/browser-session.js";

/** 验证带筛选参数的页面会命中不带参数的目标地址。 */
test("带筛选参数的页面可以复用", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://www.zhipin.com/web/chat/recommend?jobId=123&city=101010100",
      "https://www.zhipin.com/web/chat/recommend",
    ),
    true,
  );
});

/** 验证目标地址末尾斜杠不会影响标签页复用。 */
test("目标地址末尾斜杠不影响匹配", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://rd6.zhaopin.com/app/recommend?jobNumber=CC123",
      "https://rd6.zhaopin.com/app/recommend/",
    ),
    true,
  );
});

/** 验证不同路径不会误复用。 */
test("不同目标路径不会复用", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://www.zhipin.com/web/geek/recommend",
      "https://www.zhipin.com/web/chat/recommend",
    ),
    false,
  );
});

/** 验证登录页查询参数即使包含目标地址也不会被误复用。 */
test("登录页不会因为回跳参数而误复用", () => {
  assert.equal(
    pageURLContainsTarget(
      "https://login.zhipin.com/?redirect=https://www.zhipin.com/web/chat/recommend",
      "https://www.zhipin.com/web/chat/recommend",
    ),
    false,
  );
});
