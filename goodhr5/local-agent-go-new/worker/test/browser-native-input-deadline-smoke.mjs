// 文件作用：仅在明确隔离的测试浏览器中验证协议滚轮永不返回时 Worker 限时与旧会话隔离，不访问招聘平台。
import assert from 'node:assert/strict';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const isolated = fileURLToPath(new URL('../../release/native-input-v7-browser', import.meta.url));
assert.equal(process.env.GOODHR_INPUT_DEADLINE_FAULT_TEST, 'true', '必须显式启用隔离测试');
assert.ok(path.resolve(process.env.CAMOUFOX_BINARY_PATH ?? '').startsWith(path.resolve(isolated) + path.sep), '必须使用隔离浏览器');
const server = http.createServer((_, response) => { response.writeHead(200, { 'Content-Type': 'text/html' }); response.end('<html><div id="list" style="height:2200px">static-scroll-fixture</div></html>'); });
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const actions = new ActionService();
/** run 为原生限时验证提供独立追踪及比单次输入更宽的总预算。 */
async function run(action, phase, operation) {
  const context = { action, trace_id: 'native-input-deadline-smoke', request_id: phase, started_at: Date.now(), deadline_at: Date.now() + 60000 };
  return actionContextStorage.run(context, () => operation(context));
}
try {
  await run('browser.start', 'start', context => actions.startBrowser({ headless: true, url: `http://127.0.0.1:${server.address().port}` }, context));
  const started = Date.now();
  await assert.rejects(run('page.scroll', 'wheel-hang', context => actions.scroll({ distance: 200, max_attempts: 1, wait_ms: 0 }, context)), error => {
    assert.equal(error.code, 'WORKER_UNRESPONSIVE'); assert.equal(error.step, 'mouse_wheel');
    assert.equal(error.details.output.input_wait.origin, 'worker');
    assert.equal(error.details.output.input_wait.timeout_ms, 18000);
    assert.equal(error.details.recovery_required, true); return true;
  });
  assert.ok(Date.now() - started < 25000);
  await assert.rejects(run('page.scroll', 'blocked-followup', context => actions.scroll({ distance: 100 }, context)), { code: 'WORKER_UNRESPONSIVE' });
  assert.equal(actions.healthStatus().queue.blocked, true);
  const cleanupDeadline = Date.now() + 10000;
  while ((await actions.browserStatus()).running && Date.now() < cleanupDeadline) await new Promise(resolve => setTimeout(resolve, 50));
  assert.equal((await actions.browserStatus()).running, false, '故障回调必须回收旧浏览器');
  console.log('NATIVE_INPUT_DEADLINE_SMOKE_OK ' + JSON.stringify({ elapsed_ms: Date.now() - started, timeout_origin: 'worker', queue_isolated: true, old_browser_stopped: true }));
} finally {
  // 隔离队列按设计拒绝所有新动作，故障回调已经负责关闭浏览器；正常路径仍显式关闭。
  if (!actions.healthStatus().queue.blocked) await run('browser.stop', 'stop', context => actions.stopBrowser(context));
  await new Promise(resolve => server.close(resolve));
}
