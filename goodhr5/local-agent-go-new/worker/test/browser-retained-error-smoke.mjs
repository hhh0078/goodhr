// 文件作用说明：用本地静态页和独立浏览器验证操作超时后窗口保留、旧请求不补发、明确重新开始复用同一窗口。
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { setImmediate as nextTurn } from 'node:timers/promises';
import { ActionService } from '../dist/browser/actions/action-service.js';

assert.equal(process.env.GOODHR_RETAIN_BROWSER_SMOKE, 'true', '必须明确启用本地独立浏览器验证');
assert.ok(process.env.CAMOUFOX_BINARY_PATH, '必须指定独立测试浏览器，禁止使用招聘账号的 Profile');
const output = path.resolve(process.env.GOODHR_RETAIN_BROWSER_OUTPUT ?? '../release/retain-browser-smoke');
await fs.mkdir(output, { recursive: true });
const server = http.createServer((_, response) => {
  response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
  response.end('<!doctype html><title>保留浏览器验证</title><body style="padding:40px;font:24px sans-serif"><h1>浏览器窗口保留验证</h1><p>本页只用于本地验证，没有页面脚本。</p><div style="height:1800px">滚动区域</div><p>窗口还在，超时不会自动关闭浏览器。</p></body>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const actions = new ActionService();

/** context 为独立静态页面的每次请求提供新的追踪和限时。 */
function context(action, timeout = 60000) {
  return { action, trace_id: 'real-retained-browser', request_id: action, started_at: Date.now(), deadline_at: Date.now() + timeout };
}

let release;
let page;
let originalInputs = 0;
const originalPress = actions.keyboardPrimitive.press.bind(actions.keyboardPrimitive);
try {
  await actions.startBrowser({ headless: process.env.GOODHR_RETAIN_BROWSER_HEADLESS === 'true',
    url: `http://127.0.0.1:${server.address().port}/`, downloads_path: output }, context('browser.start'));
  // 只延迟已完成的标准按键结果，模拟响应迟到；不修改招聘页面或浏览器资源。
  actions.keyboardPrimitive.press = async (currentPage, key) => {
    page = currentPage;
    originalInputs++;
    await originalPress(currentPage, key);
    await new Promise(resolve => { release = resolve; });
  };
  await assert.rejects(actions.pressKey({ key: 'End' }, context('keyboard.press', 500)), { code: 'WORKER_UNRESPONSIVE' });
  assert.equal((await actions.browserStatus()).running, true);
  assert.equal(page.isClosed(), false);
  assert.equal(actions.healthStatus().queue.blocked, true);
  assert.equal(originalInputs, 1);
  await page.screenshot({ path: path.join(output, 'retained-after-timeout.png'), timeout: 5000 });
  await assert.rejects(actions.startBrowser({}, context('browser.start')), { step: 'queue_recovery_wait' });
  release();
  await nextTurn();
  actions.keyboardPrimitive.press = originalPress;
  const recovered = await actions.startBrowser({ downloads_path: output }, context('browser.start'));
  assert.equal(recovered.reused, true);
  assert.equal(originalInputs, 1, '明确恢复不能补发上一条按键');
  assert.equal(await actions.session.requirePage(context('page.status'), 'verify_retained'), page);
  await actions.pressKey({ key: 'Home' }, context('keyboard.press'));
  await actions.scroll({ distance: 240, max_attempts: 1 }, context('page.scroll'));
  await actions.screenshot({ directory: output, filename: 'same-browser-recovered.png' }, context('page.screenshot'));
  assert.equal(page.isClosed(), false);
  console.log('RETAIN_BROWSER_SMOKE_OK ' + JSON.stringify({ window_retained: true, same_page_reused: true, input_replayed: false,
    queue_recovered: true, real_key_and_wheel: true, screenshot_directory: output }));
} finally {
  release?.();
  await actions.stopBrowser(context('browser.stop'));
  await new Promise(resolve => server.close(resolve));
}
