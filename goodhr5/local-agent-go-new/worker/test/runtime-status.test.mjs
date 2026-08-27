// 文件作用说明：验证 Worker 运行状态只读取 CloakBrowser 官方 Stable 缓存和本机 Key 状态。

import assert from "node:assert/strict";
import test from "node:test";

import { ActionService } from "../dist/browser/actions/action-service.js";

/** 验证旧二进制覆盖变量不会改变 Worker 报告的官方缓存路径。 */
test("运行状态忽略旧版二进制覆盖路径", () => {
  const previousPath = process.env.CLOAKBROWSER_BINARY_PATH;
  const previousKey = process.env.CLOAKBROWSER_LICENSE_KEY;
  try {
    process.env.CLOAKBROWSER_BINARY_PATH = "/tmp/legacy-chromium";
    process.env.CLOAKBROWSER_LICENSE_KEY = "cb_test_runtime_status_key";
    const status = new ActionService().runtimeStatus();
    assert.notEqual(status.binary_path, "/tmp/legacy-chromium");
    assert.equal(status.license_configured, true);
  } finally {
    restoreEnvironment("CLOAKBROWSER_BINARY_PATH", previousPath);
    restoreEnvironment("CLOAKBROWSER_LICENSE_KEY", previousKey);
  }
});

/** restoreEnvironment 恢复测试前的环境变量值。 */
function restoreEnvironment(name, value) {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}
