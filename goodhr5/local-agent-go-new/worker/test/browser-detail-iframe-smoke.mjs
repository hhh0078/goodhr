// 文件作用：按正式平台配置验证并列与内嵌 iframe 的详情打开、截图、正文缺失时关闭及真实滚轮，不注入脚本。
import assert from 'node:assert/strict';
import http from 'node:http';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { ActionService } from '../dist/browser/actions/action-service.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const config = JSON.parse(readFileSync(new URL('../../internal/platform/boss/config.json', import.meta.url), 'utf8'));
const selectors = config.selectors;
// 与 Go 的候选人动作组装一致，卡片内入口继承候选人卡片所在 iframe。
const openTarget = { ...selectors['candidate.open_target'], frames: [...(selectors['candidate.item'].frames ?? []), ...(selectors['candidate.open_target'].frames ?? [])] };
const served = [];
const server = http.createServer((request, response) => {
  const url = new URL(request.url, 'http://localhost');
  served.push({ path: url.pathname + url.search, at: new Date().toISOString() });
  const missing = url.searchParams.has('missing');
  const nested = url.searchParams.get('layout') === 'nested';
  const params = `layout=${nested ? 'nested' : 'sibling'}${missing ? '&missing=1' : ''}`;
  const dialog = url.searchParams.has('opened') ? `<iframe class="dialog-wrap active" style="position:fixed;inset:0;width:100%;height:100%;z-index:500;background:white" src="/web/frame/c-resume/?${params}"></iframe>` : '';
  response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
  if (url.pathname.startsWith('/web/frame/c-resume/')) {
    response.end(`<html><a class="resume-custom-close" href="/case?${params}" target="_top" style="position:fixed;top:10px;left:10px;display:block;width:150px;height:45px;z-index:10;background:white">关闭详情</a>${missing ? '<div>正文未加载</div>' : '<div id="resume" style="width:650px;height:700px">private-resume-do-not-log</div>'}</html>`);
  } else if (url.pathname === '/list') {
    response.end(`<html><style>html,body{margin:0;overflow:hidden}.card-list{height:100vh;overflow:auto}</style><div class="card-list"><div class="candidate-card-wrap"><a class="base-info join-text-wrap" style="display:block;width:300px;height:50px;margin:40px" href="/case?opened=1&${params}" target="_top">静态候选人详情入口</a></div><div style="height:1800px">静态列表滚动区域</div></div>${nested ? dialog : ''}</html>`);
  } else {
    response.end(`<html><iframe name="recommendFrame" style="width:900px;height:700px" src="/list?${params}${url.searchParams.has('opened') ? '&opened=1' : ''}"></iframe>${nested ? '' : dialog}</html>`);
  }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const root = `http://127.0.0.1:${server.address().port}`;
const actions = new ActionService();
/** run 用正式追踪上下文执行静态页面操作，保留服务器上传所用诊断形状。 */
async function run(action, phase, operation) {
  const context = { action, trace_id: 'detail-iframe-release-smoke', request_id: phase, started_at: Date.now(), deadline_at: Date.now() + 60000 };
  return actionContextStorage.run(context, () => operation(context));
}
/** probe 使用正式配置检查目标存在，不读取正文或创建长生命周期引用。 */
async function probe(key, phase) {
  return run('element.find_all', phase, context => actions.findElements({ selector: { ...selectors[key], timeout_ms: 700 }, max_items: 1, expected_missing: true, probe_only: true, diagnostic: true }, context));
}
/** close 通过详情 iframe 内的按钮关闭，必须确认两种层级内的弹窗都已消失。 */
async function close(phase) {
  return run('element.click', phase, context => actions.clickElement({ selector: selectors['candidate.detail_close'], diagnostic: true, verify: { target_hidden: selectors['candidate.detail_dialog'], timeout_ms: 2500 } }, context));
}
try {
  await run('browser.start', 'start', context => actions.startBrowser({ headless: true, url: root + '/case?layout=sibling', wait_until: 'load' }, context));
  for (const layout of ['sibling', 'nested']) {
  if (layout === 'nested') await run('page.open', layout + '-case', context => actions.openPage({ url: root + '/case?layout=' + layout, wait_until: 'load' }, context));
  await run('element.click', 'open-' + layout, context => actions.clickElement({ selector: openTarget, diagnostic: true }, context));
  assert.equal((await probe('candidate.detail', 'body')).length, 1);
  assert.equal((await probe('candidate.detail_screenshot', 'screenshot-region')).length, 1);
  const screenshot = await run('element.screenshot', 'capture-detail', context => actions.screenshot({ target: selectors['candidate.detail_screenshot'], directory: fileURLToPath(new URL('../../release/detail-iframe-smoke', import.meta.url)), filename: layout + '-detail.png' }, context));
  assert.ok(screenshot.size > 0);
  await close('close-normal');
  await assert.rejects(probe('candidate.detail_dialog', 'closed-normal'), { code: 'ELEMENT_NOT_FOUND' });
  await run('page.open', 'missing-case', context => actions.openPage({ url: root + '/case?missing=1&layout=' + layout, wait_until: 'load' }, context));
  await run('element.click', 'open-missing', context => actions.clickElement({ selector: openTarget, diagnostic: true }, context));
  await assert.rejects(probe('candidate.detail', 'body-missing'), { code: 'ELEMENT_NOT_FOUND' });
  assert.equal((await probe('candidate.detail_dialog', 'dialog-still-present')).length, 1);
  await close('close-without-body');
  await assert.rejects(probe('candidate.detail_dialog', 'closed-missing'), { code: 'ELEMENT_NOT_FOUND' });
  const before = await run('element.find', 'list-before-wheel', context => actions.findElement({ selector: selectors['candidate.item'] }, context));
  await run('page.scroll', 'list-after-cleanup', context => actions.scroll({ wheel_anchor: selectors['candidate.item'], distance: 200, max_attempts: 1, wait_ms: 150 }, context));
  const after = await run('element.find', 'list-after-wheel', context => actions.findElement({ selector: selectors['candidate.item'] }, context));
  assert.ok(after.view.box.y < before.view.box.y - 1, '弹窗清理后真实滚轮应能移动列表');
  console.log('DETAIL_IFRAME_LAYOUT_OK ' + JSON.stringify({ layout, body_and_screenshot_resolved: true, close_without_body: true, dialog_hidden_verified: true, real_wheel_after_cleanup: true }));
  }
  console.log('DETAIL_IFRAME_SMOKE_OK ' + JSON.stringify({ sibling_iframe: true, nested_iframe: true }));
} catch (error) {
  console.error('DETAIL_IFRAME_SMOKE_REQUESTS ' + JSON.stringify(served));
  await run('page.screenshot', 'failed-case-screenshot', context => actions.screenshot({ directory: fileURLToPath(new URL('../../release/detail-iframe-smoke', import.meta.url)), filename: 'failure.png' }, context)).catch(() => undefined);
  throw error;
} finally {
  await run('browser.stop', 'stop', context => actions.stopBrowser(context));
  await new Promise(resolve => server.close(resolve));
}
