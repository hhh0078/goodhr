// 文件作用说明：复现鼠标失败或延迟时的新标签页超时，验证 Worker 不再闪退且监听会被清理。

import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { NewPageWaiter } from "../dist/browser/actions/new-page-waiter.js";

/** context 为等待测试创建可取消的操作上下文。 */
function context(signal) {
  return { action: "element.click", trace_id: "new-page-test", started_at: Date.now(), signal };
}

test("新标签页到达后只接收一次并清理全部监听", async () => {
  const browser = new EventEmitter();
  const waiter = new NewPageWaiter(browser, context(), 1000);
  const page = {};
  browser.emit("page", page);
  assert.equal(await waiter.wait(), page);
  assert.equal(browser.listenerCount("page"), 0);
  assert.equal(browser.listenerCount("close"), 0);
});

for (const trigger of ["timeout", "close", "abort", "cancel"]) {
  test(`等待${trigger}后返回可处理的错误且清理监听`, async () => {
    const browser = new EventEmitter();
    const controller = new AbortController();
    const waiter = new NewPageWaiter(browser, context(controller.signal), 10);
    if (trigger === "close") browser.emit("close");
    if (trigger === "abort") controller.abort();
    if (trigger === "cancel") waiter.cancel();
    await assert.rejects(waiter.wait(), error => error.step === "wait_new_page" && error.retryable === false);
    assert.equal(browser.listenerCount("page"), 0);
    assert.equal(browser.listenerCount("close"), 0);
  });
}

for (const mouseFails of [true, false]) {
  test(`真实 Node 子进程：鼠标${mouseFails ? "报错" : "慢于新标签页超时"}后不触发未处理异常`, () => {
    const code = `
      import assert from 'node:assert/strict';
      import { EventEmitter } from 'node:events';
      import { ClickAction } from './dist/browser/actions/click.js';
      process.on('unhandledRejection', error => { throw error; });
      const browser = new EventEmitter();
      const box = { x: 10, y: 10, width: 100, height: 40 };
      const found = { result: { element_ref: 'button' }, resolved: { page: { context: () => browser }, locator: {}, view: { box } } };
      const mouse = { async down() { ${mouseFails ? "throw new Error('GOODHR_MOUSE_APZ_TIMEOUT:mousedown');" : "await new Promise(resolve => setTimeout(resolve, 80));"} }, async up() {} };
      const action = new ClickAction({ async one() { return found; } }, { async ensureVisible() {} },
        { async toElement() {} }, { async box() { return box; } }, mouse, { info() {}, failure() {} });
      await assert.rejects(action.execute({ selector: { description: '候选人详情' }, wait_for_new_page: true, new_page_timeout_ms: 20 },
        { action: 'element.click', trace_id: 'regression', started_at: Date.now() }));
      await new Promise(resolve => setTimeout(resolve, 60));
      assert.equal(browser.listenerCount('page'), 0);
      assert.equal(browser.listenerCount('close'), 0);
    `;
    const child = spawnSync(process.execPath, ["--input-type=module", "-e", code], { cwd: new URL("..", import.meta.url), encoding: "utf8", timeout: 5000 });
    assert.equal(child.status, 0, child.stderr || String(child.error ?? ""));
  });
}
