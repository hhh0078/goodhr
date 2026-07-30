// 文件作用说明：验证浏览器启动参数保留强类型视口和 User-Agent 配置。

import assert from "node:assert/strict";
import test from "node:test";

import { parseBrowserStartRequest } from "../dist/validation/action-requests.js";

/** 验证合法启动、GeoIP 和新增标签页配置会进入 CloakBrowser 启动参数。 */
test("保留合法启动、GeoIP 和新增标签页配置", () => {
  const request = parseBrowserStartRequest(
    {
      user_agent: "GoodHR-Test",
      viewport_width: 1440,
      viewport_height: 900,
      geoip: true,
      new_tab: true,
      wait_until: "domcontentloaded",
      timeout_ms: 45_000,
      extension_paths: ["/tmp/goodhr-extension"],
    },
    "trace-test",
    "browser.start",
  );
  assert.equal(request.user_agent, "GoodHR-Test");
  assert.equal(request.viewport_width, 1440);
  assert.equal(request.viewport_height, 900);
  assert.equal(request.geoip, true);
  assert.equal(request.new_tab, true);
  assert.equal(request.wait_until, "domcontentloaded");
  assert.equal(request.timeout_ms, 45_000);
  assert.deepEqual(request.extension_paths, ["/tmp/goodhr-extension"]);
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
