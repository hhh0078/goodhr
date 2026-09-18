// 文件作用说明：验证启动请求保留导航配置并忽略旧客户端固定宽高。

import assert from "node:assert/strict";
import test from "node:test";

import { parseBrowserStartRequest } from "../dist/validation/action-requests.js";

/** 验证合法启动和新增标签页配置会进入 Camoufox 启动参数。 */
test("保留合法启动和新增标签页配置", () => {
  const request = parseBrowserStartRequest(
    {
      user_agent: "GoodHR-Test",
      viewport_width: 1440,
      viewport_height: 900,
      new_tab: true,
      wait_until: "domcontentloaded",
      timeout_ms: 45_000,
      extension_paths: ["/tmp/goodhr-extension"],
    },
    "trace-test",
    "browser.start",
  );
  assert.equal(request.user_agent, "GoodHR-Test");
  assert.equal(request.viewport_width, undefined);
  assert.equal(request.viewport_height, undefined);
  assert.equal(request.new_tab, true);
  assert.equal(request.wait_until, "domcontentloaded");
  assert.equal(request.timeout_ms, 45_000);
  assert.deepEqual(request.extension_paths, ["/tmp/goodhr-extension"]);
});

/** 验证旧版单个宽高字段也不影响自动窗口启动。 */
test("忽略旧客户端单独传入的宽度", () => {
  const request = parseBrowserStartRequest(
    { viewport_width: 1440 }, "trace-test", "browser.start",
  );
  assert.equal(request.viewport_width, undefined);
});
