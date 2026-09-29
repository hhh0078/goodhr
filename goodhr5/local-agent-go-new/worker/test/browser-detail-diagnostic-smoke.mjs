// 文件作用：通过本地静态双层 iframe 页面验证详情诊断、真实点击遮挡和网络故障，不访问招聘账号或注入脚本。
import assert from 'node:assert/strict';
import http from 'node:http';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const server = http.createServer((request, response) => {
  const url = new URL(request.url, 'http://localhost');
  response.writeHead(url.pathname === '/denied' ? 403 : 200, { 'Content-Type': 'text/html; charset=utf-8' });
  const blocked = url.searchParams.has('blocked');
  if (url.pathname === '/outer') response.end(`<html><style>body{padding:30px}a{display:block;width:250px;height:50px;margin:30px}iframe{width:700px;height:350px}.dialog-mask{position:fixed;inset:0;background:#eee;z-index:500}</style><a id="open" href="/detail" target="inner">打开详情</a><iframe name="inner" src="/blank"></iframe>${blocked ? '<div class="dialog-mask">验证遮挡</div>' : ''}</html>`);
  else if (url.pathname === '/detail') response.end('<html><div id="detail">private-resume-body-do-not-log</div></html>');
  else if (url.pathname === '/blank') response.end('<html><div>等待点击</div></html>');
  else if (url.pathname === '/denied') response.end('<html>访问未通过</html>');
  else response.end(`<html><iframe name="outer" style="width:900px;height:700px" src="/outer${blocked ? '?blocked=1' : ''}"></iframe>${url.pathname === '/network' ? '<iframe src="/denied"></iframe>' : ''}</html>`);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const root = `http://127.0.0.1:${server.address().port}`;
const actions = new ActionService();
/** context 为每次静态验证生成独立操作编号和有界等待。 */
function context(action, phase) { return { action, trace_id: 'detail-native-v7-smoke', request_id: phase, started_at: Date.now(), deadline_at: Date.now() + 60000 }; }
/** run 使用正式异步追踪环境，确保鼠标日志也进入同一次诊断。 */
async function run(action, phase, operation) { const value = context(action, phase); return actionContextStorage.run(value, () => operation(value)); }
/** frame 创建静态验证页的通用 iframe 选择器。 */
const frame = name => ({ selectors: [{ type: 'css', value: `iframe[name='${name}']` }] });
const detail = { frames: [frame('outer'), frame('inner')], target: { selectors: [{ type: 'css', value: '#detail' }] }, timeout_ms: 500, description: '静态验证详情' };
const click = { frames: [frame('outer')], target: { selectors: [{ type: 'css', value: '#open' }] }, description: '静态验证入口' };
/** probe 只检查元素状态，不读取详情正文，并保留预期缺失的诊断。 */
async function probe(phase) { return run('element.find_all', phase, ctx => actions.findElements({ selector: detail, max_items: 1, expected_missing: true, diagnostic: true, diagnostic_attempt: 1, probe_only: true }, ctx)); }
try {
  await run('browser.start', 'start', ctx => actions.startBrowser({ headless: true, url: root + '/normal' }, ctx));
  await assert.rejects(probe('before-click'), error => error.code === 'ELEMENT_NOT_FOUND');
  await run('element.click', 'normal-click', ctx => actions.clickElement({ selector: click, diagnostic: true, diagnostic_attempt: 1 }, ctx));
  const items = await probe('after-click');
  assert.equal(items.length, 1); assert.equal(items[0].text, '');
  await run('page.open', 'blocked-page', ctx => actions.openPage({ url: root + '/blocked?blocked=1' }, ctx));
  await run('element.click', 'blocked-click', ctx => actions.clickElement({ selector: click, diagnostic: true, diagnostic_attempt: 2 }, ctx));
  await assert.rejects(probe('blocked-detail'), error => error.code === 'ELEMENT_NOT_FOUND');
  await run('page.open', 'network-page', ctx => actions.openPage({ url: root + '/network' }, ctx));
  await assert.rejects(probe('network-detail'), error => error.code === 'ELEMENT_NOT_FOUND');
  console.log('DETAIL_NATIVE_V7_SMOKE_OK ' + JSON.stringify({ detail_opened: true, probe_only: true, pointer_intercepted_case: true, network_403_case: true }));
} finally {
  await run('browser.stop', 'stop', ctx => actions.stopBrowser(ctx));
  await new Promise(resolve => server.close(resolve));
}
