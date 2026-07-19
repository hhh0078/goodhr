/** 本文件验证浏览器显示校准失败时返回可供用户自行调整的错误说明。 */
import assert from "node:assert/strict";
import test from "node:test";

import { browserDisplayAdjustmentMessage } from "./browser-display.js";

/** 验证 90% 页面缩放会生成包含当前尺寸、缩放比例和处理方法的错误说明。 */
test("browserDisplayAdjustmentMessage explains how to fix 90 percent zoom", () => {
  const message = browserDisplayAdjustmentMessage({
    target_width: 1280,
    target_height: 720,
    inner_width: 1422,
    inner_height: 800,
  });

  assert.match(message, /期望视口 1280x720/);
  assert.match(message, /实际 1422x800/);
  assert.match(message, /缩放约 90%/);
  assert.match(message, /任务已停止，浏览器会保持打开/);
  assert.match(message, /Ctrl\+0/);
});
