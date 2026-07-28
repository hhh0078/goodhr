// 文件作用说明：验证浏览器启动参数保留强类型视口和 User-Agent 配置。

import assert from "node:assert/strict";
import test from "node:test";

import { parseBrowserStartRequest } from "../dist/validation/action-requests.js";

/** 验证合法视口和 User-Agent 会进入 CloakBrowser 启动参数。 */
test("保留合法视口和 User-Agent", () => {
  const request = parseBrowserStartRequest(
    {
      user_agent: "GoodHR-Test",
      viewport_width: 1440,
      viewport_height: 900,
    },
    "trace-test",
    "browser.start",
  );
  assert.equal(request.user_agent, "GoodHR-Test");
  assert.equal(request.viewport_width, 1440);
  assert.equal(request.viewport_height, 900);
});

/** 验证不完整视口会被请求边界拒绝。 */
test("拒绝只有宽度的视口", () => {
  assert.throws(() =>
    parseBrowserStartRequest(
      { viewport_width: 1440 },
      "trace-test",
      "browser.start",
    ),
  );
});
