/** 本文件负责验证浏览器启动参数不再携带固定窗口尺寸。 */

import assert from "node:assert/strict";
import test from "node:test";
import { BrowserBaseActions } from "./browser-actions.js";

test("browser launch options do not set a fixed window size", () => {
  const actions = new BrowserBaseActions({
    downloadsPath: "C:/goodhr-downloads",
    logger: { log() {} },
  });
  const options = actions.buildLaunchOptions({
    args: ["--window-size=1440,900", "--test-type"],
  });

  assert.equal(options.viewport, null);
  assert.equal(
    options.args.some((item) => String(item).startsWith("--window-size=")),
    false,
  );
  assert.deepEqual(options.args, ["--test-type"]);
});
