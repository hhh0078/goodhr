// 文件作用：在显式隔离的有窗口浏览器中验证真实最小化恢复与一次滚轮输入，用测试协议返回窗口证据，不访问招聘平台。
import assert from 'node:assert/strict';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const isolated = fileURLToPath(new URL('../../release/native-input-v7-browser', import.meta.url));
assert.equal(process.env.GOODHR_WINDOW_FAULT_TEST, 'true', '必须显式启用隔离窗口恢复测试');
assert.ok(path.resolve(process.env.CAMOUFOX_BINARY_PATH ?? '').startsWith(path.resolve(isolated) + path.sep), '必须使用隔离浏览器');
const server = http.createServer((_, response) => {
  response.writeHead(200, { 'Content-Type': 'text/html' });
  response.end('<html><div id="list" style="height:2200px">static-window-fixture</div></html>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const actions = new ActionService();

/** run 为真实窗口恢复验证提供正式追踪和独立总预算。 */
async function run(action, phase, operation) {
  const context = { action, trace_id: 'native-window-smoke', request_id: phase, started_at: Date.now(), deadline_at: Date.now() + 60000 };
  return actionContextStorage.run(context, () => operation(context));
}

try {
  await run('browser.start', 'start', context => actions.startBrowser({ headless: false, url: `http://127.0.0.1:${server.address().port}` }, context));
  await assert.rejects(run('page.scroll', 'minimize-before-wheel', context => actions.scroll({ distance: 200, max_attempts: 1, wait_ms: 0 }, context)), error => {
    const evidence = error.details.native_mouse ?? error.details.output?.native_mouse;
    assert.match(error.details.original_error ?? error.details.output?.original_error ?? '', /GOODHR_WINDOW_SMOKE_SUCCESS/);
    assert.equal(evidence.mouse_safety_version, 'v9');
    assert.equal(evidence.window_before.headless, false);
    assert.equal(evidence.window_before.window_minimized, true);
    assert.equal(evidence.window_ready_before_input.window_minimized, false);
    assert.equal(evidence.window_ready_before_input.chrome_document_hidden, false);
    assert.equal(evidence.window_ready_before_input.browser_active, true);
    assert.ok(evidence.window_restore_attempts >= 1);
    assert.equal(evidence.events_sent, 1);
    console.log('NATIVE_WINDOW_SMOKE_OK ' + JSON.stringify({ runtime_version: 'v9', real_minimized_window: true,
      window_restored_and_active: true, events_sent: evidence.events_sent, restore_attempts: evidence.window_restore_attempts }));
    return true;
  });
} finally {
  await run('browser.stop', 'stop', context => actions.stopBrowser(context));
  await new Promise(resolve => server.close(resolve));
}
