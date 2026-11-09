// 文件作用说明：验证单元素只读跳过几何采样，仍保留层级、属性、状态、取消和元素引用安全。
import assert from 'node:assert/strict';
import test from 'node:test';
import { FindAction } from '../dist/browser/actions/find.js';
import { ReadAction } from '../dist/browser/actions/read.js';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { ElementRegistry } from '../dist/browser/session/element-registry.js';
import { startRequestProgress, requestProgress } from '../dist/logging/request-progress.js';
import { actionContextStorage } from '../dist/logging/operation.js';

/** group 创建标准选择器层级，不涉及平台内容。 */
const group = value => ({ selectors: [{ type: 'css', value }] });

/** harness 以标准 Locator 替身记录真实定位链、几何调用及状态读取。 */
function harness(t) {
  const counters = { box: 0, viewport: 0, text: 0, visible: 0, enabled: 0 };
  const events = [];
  const target = { text: '目标', attrs: { id: 'second' }, visible: true, enabled: true };
  const root = { attrs: { owner: 'right' }, children: { '.item': [{ ...target, attrs: { id: 'first' } }, target] } };
  /** scope 根据静态树提供 Locator 查询和属性交集，避免调用真实页面。 */
  function scope(children) {
    return { locator(value) {
      const decoded = value.replace(/\\([0-9a-f]+) /g, (_match, hex) => String.fromCodePoint(Number.parseInt(hex, 16)));
      const attributes = [...decoded.matchAll(/\[([^=]+)="([^"]*)"\]/g)];
      const nodes = attributes.length > 0
        ? Object.values(children).flat().filter(node => attributes.every(([, key, val]) => node.attrs?.[key] === val))
        : children[value] ?? [];
      return list(nodes);
    } };
  }
  /** list 模拟 nth、文本过滤和目标状态，不让轻量路径偷偷采集几何。 */
  function list(nodes) {
    return {
      nodes,
      ...scope(nodes[0]?.children ?? {}),
      count: async () => nodes.length,
      nth: index => list(nodes.slice(index, index + 1)),
      and: other => list(nodes.filter(node => other.nodes.includes(node))),
      filter: ({ hasText }) => list(nodes.filter(node => hasText instanceof RegExp ? hasText.test(node.text ?? '') : (node.text ?? '').includes(hasText))),
      contentFrame: () => scope(nodes[0]?.children ?? {}),
      getAttribute: async name => nodes[0]?.attrs?.[name] ?? null,
      isVisible: async () => { counters.visible++; target.onVisible?.(); return nodes[0]?.visible ?? true; },
      isEnabled: async () => { counters.enabled++; return nodes[0]?.enabled ?? true; },
      innerText: async () => { counters.text++; return nodes[0]?.text ?? ''; },
      innerHTML: async () => '<span>目标</span>',
      boundingBox: async () => { counters.box++; return { x: 10, y: 20, width: 40, height: 30 }; },
    };
  }
  const page = { ...scope({ '.frame': [{ children: { '.root': [{ attrs: { owner: 'wrong' } }, root] } }] }),
    url: () => 'https://example.test/fixture', isClosed: () => false };
  const primitive = new LocatorPrimitive();
  primitive.viewport.size = async () => { counters.viewport++; return { width: 800, height: 600 }; };
  const registry = new ElementRegistry();
  t.after(() => registry.clear());
  const session = { elements: registry, requirePage: async () => page, listPages: async () => ({ pages: [{ current: true, page_id: '1' }] }) };
  const logger = { info: (_ctx, step, status, data) => events.push({ step, status, ...data }), failure: () => {} };
  const find = new FindAction(session, primitive, logger);
  const read = new ReadAction(find, logger);
  const selector = { description: '合成目标', timeout_ms: 0, frames: [group('.missing')], frame_fallbacks: [[group('.frame')]],
    parents: [{ ...group('.root'), attributes: { owner: 'right' } }],
    target: { ...group('.item'), index: 1, text: '目标', exact_text: true, attributes: { id: 'second' } } };
  const context = { action: 'element.read', trace_id: 'read-geometry', started_at: Date.now() };
  return { counters, events, target, root, page, primitive, registry, find, read, selector, context };
}

test('文本、HTML 和选择器属性读取不采集几何，保留框架、父级、属性、序号和引用', async t => {
  const h = harness(t);
  for (const [extra, expected] of [[{}, '目标'], [{ property: 'html' }, '<span>目标</span>'],
    [{ use_selector_read: true, selector: { ...h.selector, read_attribute: 'id' } }, 'second']]) {
    const result = await h.read.execute({ selector: h.selector, ...extra }, h.context);
    assert.equal(result.value, expected);
    assert.equal(await h.registry.get(result.element_ref, h.page, { ...h.context, step: 'get' }).getAttribute('id'), 'second');
    assert.equal(Object.hasOwn(result, 'view'), false);
  }
  assert.equal(h.registry.entries.size, 3);
  assert.equal(h.counters.box, 0);
  assert.equal(h.counters.viewport, 0);
  assert.equal(h.counters.visible, 3);
  for (const event of h.events.filter(event => event.step === 'find' && event.status === 'success')) {
    assert.equal(event.geometry_read, false);
    assert.equal(event.state_checked, 'visible');
    assert.equal(Object.hasOwn(event, 'in_viewport'), false);
  }
});

test('只读保留 visible、enabled、attached 和属性失败语义', async t => {
  const h = harness(t);
  h.target.visible = false;
  await assert.rejects(h.read.execute({ selector: h.selector }, h.context), { code: 'ELEMENT_NOT_VISIBLE' });
  const attached = await h.read.execute({ selector: { ...h.selector, state: 'attached' } }, h.context);
  assert.equal(attached.value, '目标');
  h.target.visible = true;
  h.target.enabled = false;
  await assert.rejects(h.read.execute({ selector: { ...h.selector, state: 'enabled' } }, h.context), { code: 'ELEMENT_NOT_ENABLED' });
  h.target.enabled = true;
  assert.equal((await h.read.execute({ selector: { ...h.selector, state: 'enabled' } }, h.context)).value, '目标');
  await assert.rejects(h.read.execute({ selector: { ...h.selector, target: { ...h.selector.target, attributes: { id: 'wrong' } } } }, h.context), { code: 'ELEMENT_NOT_FOUND' });
  assert.equal(h.counters.text, 2);
  assert.equal(h.counters.box + h.counters.viewport, 0);
});

test('读取时父级仍须唯一，不能通过轻量模式猜测目标', async t => {
  const h = harness(t);
  await assert.rejects(h.read.execute({ selector: { ...h.selector, parents: [group('.root')] } }, h.context), { code: 'ELEMENT_AMBIGUOUS' });
  assert.equal(h.counters.text, 0);
  assert.equal(h.registry.entries.size, 0);
});

test('默认交互定位仍读取几何并提供真实视口状态', async t => {
  const h = harness(t);
  const found = await h.find.one(h.selector, h.context, true);
  assert.equal(h.counters.box, 1);
  assert.equal(h.counters.viewport, 1);
  assert.deepEqual(found.result.view.box, { x: 10, y: 20, width: 40, height: 30 });
  assert.equal(found.result.view.in_viewport, true);
  assert.equal(h.events.at(-1).in_viewport, true);
});

test('状态读取期间取消后不读取正文、不生成引用、不继续备用路径', async t => {
  const h = harness(t);
  const cancel = new AbortController();
  h.target.onVisible = () => cancel.abort();
  const context = { ...h.context, signal: cancel.signal };
  await assert.rejects(actionContextStorage.run(context, () => h.read.execute({ selector: h.selector }, context)), { code: 'ACTION_CANCELLED' });
  assert.equal(h.counters.text, 0);
  assert.equal(h.counters.box + h.counters.viewport, 0);
  assert.equal(h.registry.entries.size, 0);
});

test('普通元素阅读失败明确停在定位还是已定位后取值，仅真正的 Playwright 超时带取值时限', async t => {
  for (const mode of ['missing', 'timeout', 'other']) {
    const h = harness(t);
    const id = mode.padEnd(26, 'A');
    const ctx = { ...h.context, request_id: id };
    startRequestProgress(ctx);
    if (mode === 'missing') {
      h.target.visible = false;
    } else {
      // 模拟原生取值失败；始终禁止读或记录测试人的实际内容。
      h.read.primitive.text = async () => { throw Object.assign(new Error('synthetic'), { name: mode === 'timeout' ? 'TimeoutError' : 'Error' }); };
    }
    await assert.rejects(h.read.execute({selector: h.selector}, ctx), err => {
      const progress = requestProgress(id, {});
      assert.equal(progress.worker_phase, mode === 'missing' ? 'locate_read_target' : 'read_value');
      assert.equal(err.details?.read_value_timeout_ms, mode === 'timeout' ? 1500 : undefined);
      return true;
    });
  }
});
