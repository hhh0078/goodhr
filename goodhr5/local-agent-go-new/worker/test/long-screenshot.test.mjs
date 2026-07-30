/** 文件作用说明：验证长截图每次真实滚轮只截取一张新画面，并在重复画面出现时停止。 */

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { ScreenshotAction } from "../dist/browser/actions/screenshot.js";

test("long screenshot captures once per wheel and stops on duplicate", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-long-shot-"));
  const frames = [
    Buffer.from("frame-000001"),
    Buffer.from("frame-999999"),
    Buffer.from("frame-999999"),
  ];
  let screenshotCalls = 0;
  let wheelCalls = 0;
  const page = {};
  const locator = {
    async screenshot() {
      const frame = frames[Math.min(screenshotCalls, frames.length - 1)];
      screenshotCalls += 1;
      return frame;
    },
  };
  const found = {
    resolved: {
      locator,
      page,
      view: { viewport: { width: 1200, height: 800 } },
    },
  };
  const action = new ScreenshotAction(
    { async requirePage() { return page; } },
    { async one() { return found; } },
    { async toElement() {} },
    { async wheel() { wheelCalls += 1; } },
    { info() {}, failure() {} },
  );

  try {
    const result = await action.long(
      {
        target: { description: "候选人详情" },
        directory,
        filename: "candidate-001.png",
        distance: 520,
        max_parts: 10,
        wait_ms: 50,
      },
      { action: "element.screenshot_long", trace_id: "test-long-shot" },
    );
    assert.equal(result.complete, true);
    assert.equal(result.count, 2);
    assert.equal(screenshotCalls, 3);
    assert.equal(wheelCalls, 2);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});
