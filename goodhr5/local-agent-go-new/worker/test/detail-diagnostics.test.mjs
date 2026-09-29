// 文件作用：验证详情预期缺失仍保留逐层定位证据，诊断不读取简历正文，并识别遮挡和网络错误。
import assert from 'node:assert/strict';
import test from 'node:test';
import { EventEmitter } from 'node:events';
import { FindAction } from '../dist/browser/actions/find.js';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { WorkerLogger } from '../dist/logging/logger.js';
import { clickActionability, selectorDiagnostic } from '../dist/browser/actions/diagnostic-state.js';
import { pageDiagnostic, trackPageDiagnostics } from '../dist/browser/session/page-diagnostics.js';
import { parseElementFindAllRequest, parseElementClickRequest } from '../dist/validation/action-requests.js';

/** fixture 模拟存在但隐藏、完全缺失和查询失败的页面，不提供任何正文读取能力。 */
function fixture(mode) {
  const locator = { count: async () => { if (mode === 'query') throw new Error('unavailable'); return mode === 'missing' ? 0 : 1; },
    nth() { return locator; }, isVisible: async () => mode !== 'hidden' };
  const page = new EventEmitter();
  page.locator = () => locator; page.url = () => 'https://example.com/list?token=private-secret';
  page.isClosed = () => false; page.frames = () => []; page.context = () => ({ pages: () => [page] });
  const lines = []; const logger = new WorkerLogger(line => lines.push(JSON.parse(line)));
  const find = new FindAction({ requirePage: async () => page }, new LocatorPrimitive(), logger);
  const spec = { target: { selectors: [{ type: 'css', value: '#detail' }] }, timeout_ms: 0, description: '详情正文' };
  return { page, lines, spec, run: () => find.all(spec, 1, {}, { action: 'element.find_all', trace_id: 'probe', started_at: Date.now() },
    false, 10, 0, { diagnostic: true, attempt: 2, probe_only: true }) };
}

test('详情存在时只探测，不读取正文也不生成无用的元素引用', async () => {
  const value = fixture('visible'); const result = await value.run();
  assert.equal(result.length, 1); assert.equal(result[0].text, ''); assert.equal(result[0].element_ref, '');
  const evidence = value.lines.find(item => item.step === 'selector_probe' && item.status === 'success');
  assert.equal(evidence.output.selector_attempts[0].state_matches, 1); assert.equal(evidence.diagnostic, true);
});

test('详情缺失、隐藏及查询异常分别带出真实匹配和状态证据', async () => {
  for (const mode of ['missing', 'hidden', 'query']) {
    const value = fixture(mode); await assert.rejects(value.run());
    const evidence = value.lines.find(item => item.status === 'missing');
    assert.equal(evidence.input.attempt, 2); assert.equal(evidence.diagnostic, true);
    assert.equal(evidence.output.selector_attempts[0].matches, mode === 'hidden' ? 1 : 0);
    if (mode === 'hidden') assert.equal(evidence.output.selector_attempts[0].state_matches, 0);
    if (mode === 'query') assert.equal(evidence.output.selector_attempts[0].query_error_type, 'Error');
    assert.doesNotMatch(JSON.stringify(value.lines), /private-secret/);
  }
});

test('遮挡检查只保留结构信息，不把候选人正文写进日志', async () => {
  const result = await clickActionability({ click: async () => { throw new Error('<div class="dialog-mask">private-resume</div> intercepts pointer events'); } });
  assert.equal(result.reason, 'pointer_intercepted'); assert.equal(result.blockers[0].css_class, 'dialog-mask');
  assert.doesNotMatch(JSON.stringify(result), /private-resume/);
  const selector = selectorDiagnostic({ target: { selectors: [{ type: 'text', value: 'private-name' }], text: 'private-name' } });
  assert.doesNotMatch(JSON.stringify(selector), /private-name/);
});

test('页面事件只订阅一次，保留网络状态且有界，不含查询凭据和响应正文', () => {
  const { page } = fixture('visible'); trackPageDiagnostics(page); trackPageDiagnostics(page);
  assert.equal(page.listenerCount('response'), 1);
  for (let index = 0; index < 30; index++) page.emit('response', { status: () => 403,
    url: () => 'https://example.com/detail?token=private-secret', request: () => ({ resourceType: () => 'xhr' }) });
  const evidence = pageDiagnostic(page);
  assert.equal(evidence.recent_page_events.length, 12); assert.equal(evidence.recent_page_events[0].status, 403);
  assert.doesNotMatch(JSON.stringify(evidence), /private-secret/);
  for (let index = 0; index < 30; index++) page.emit('response', { status: () => 200,
    url: () => 'https://example.com/background', request: () => ({ resourceType: () => 'xhr' }) });
  assert.ok(pageDiagnostic(page).recent_page_failures.some(item => item.status === 403));
});

test('大量页面结构仍满足上传字段大小限制，失败证据优先保留', () => {
  const { page } = fixture('visible');
  const longURL = 'https://example.com/' + '长'.repeat(250);
  page.url = () => longURL;
  page.frames = () => Array.from({ length: 30 }, () => ({ name: () => '框'.repeat(48), url: () => longURL, parentFrame: () => null }));
  page.context = () => ({ pages: () => Array.from({ length: 10 }, () => page) });
  trackPageDiagnostics(page);
  for (let index = 0; index < 30; index++) page.emit('response', { status: () => 403, url: () => longURL, request: () => ({ resourceType: () => 'xhr' }) });
  const evidence = pageDiagnostic(page);
  assert.ok(Buffer.byteLength(JSON.stringify(evidence)) < 6100);
  assert.equal(evidence.evidence_truncated, true);
  assert.ok(evidence.recent_page_failures.length >= 2);
});

test('Go 和 Worker 复用现有接口传递探测和诊断参数', () => {
  const selector = { description: '详情', target: { selectors: [{ type: 'css', value: '#detail' }] } };
  const probe = parseElementFindAllRequest({ selector, diagnostic: true, diagnostic_attempt: 2, probe_only: true, expected_missing: true }, 'trace', 'element.find_all');
  assert.equal(probe.probe_only, true); assert.equal(probe.diagnostic_attempt, 2);
  const click = parseElementClickRequest({ selector, diagnostic: true, diagnostic_attempt: 1 }, 'trace', 'element.click');
  assert.equal(click.diagnostic, true); assert.equal(click.diagnostic_attempt, 1);
});
