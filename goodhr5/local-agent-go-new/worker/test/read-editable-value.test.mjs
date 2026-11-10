// 文件作用说明：验证普通输入框与富文本草稿共用值读取，并保留缺失、失败、取消及回车前草稿核验。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ReadAction } from '../dist/browser/actions/read.js';
import { KeyboardAction } from '../dist/browser/actions/keyboard.js';
import { ReadPrimitive } from '../dist/browser/primitives/read.js';
import { WorkerError } from '../dist/errors/worker-error.js';
import { actionContextStorage } from '../dist/logging/operation.js';

const nonInputMessage = 'locator.inputValue: Error: Node is not an <input>, <textarea> or <select> element';
const logger = { info() {}, failure() {} };
const selector = { description: '合成草稿', target: { selectors: [{ type: 'css', value: '.editor' }] } };

/** context 创建有明确动作和追踪标识的测试上下文，不连接浏览器。 */
function context(action = 'element.read') {
  return { action, trace_id: 'editable-value-test', started_at: Date.now() };
}

/** harness 为读取动作提供标准 Locator 替身，记录原生值和文本调用次数。 */
function harness(options = {}) {
  const calls = { input: 0, text: 0 };
  const locator = {
    async inputValue() {
      calls.input++;
      if (options.input) return options.input();
      if (options.rich) throw new Error(nonInputMessage);
      return options.value ?? '';
    },
    async textContent() {
      calls.text++;
      if (options.text) return options.text();
      return options.value ?? '';
    },
    async innerText() { throw new Error('草稿不能使用会折叠空白的可见文本'); },
    async isVisible() { return true; },
    locator: () => ({ count: async () => 1 }),
  };
  const find = { async one() {
    if (options.missing) throw options.missing;
    return { resolved: { locator }, result: { element_ref: 'editor-ref' } };
  } };
  return { action: new ReadAction(find, logger), calls, locator };
}

for (const kind of ['input', 'textarea', 'select']) {
  test(`${kind} 值保留空格和换行，不转读文本`, async () => {
    const h = harness({ value: '  草稿\n第二行  ' });
    const result = await h.action.execute({ selector, property: 'value' }, context());
    assert.equal(result.value, '  草稿\n第二行  ');
    assert.equal(result.property, 'value');
    assert.equal(result.element_ref, 'editor-ref');
    assert.deepEqual(h.calls, { input: 1, text: 0 });
  });
}

for (const value of ['草稿\n第二行', '  草稿\r\n第二行  ', '']) {
  test(`contenteditable 原样读取${value ? '多行及空格草稿' : '已清空的输入框'}`, async () => {
    const h = harness({ rich: true, value });
    const result = await h.action.execute({ selector, property: 'value' }, context());
    assert.equal(result.value, value);
    assert.equal(result.property, 'value');
    assert.deepEqual(h.calls, { input: 1, text: 1 });
  });
}

test('选择器 read_property=value 同样兼容富文本草稿', async () => {
  const h = harness({ rich: true, value: '草稿' });
  const result = await h.action.execute({ selector: { ...selector, read_property: 'value' }, use_selector_read: true }, context());
  assert.equal(result.value, '草稿');
  assert.deepEqual(h.calls, { input: 1, text: 1 });
});

test('目标不存在时保留定位错误，不读值也不伪装成空草稿', async () => {
  const missing = new WorkerError({ code: 'ELEMENT_NOT_FOUND', message: '合成缺失', action: 'element.read', step: 'find', trace_id: 'missing' });
  const h = harness({ missing });
  await assert.rejects(h.action.execute({ selector, property: 'value' }, context()), error => error === missing);
  assert.deepEqual(h.calls, { input: 0, text: 0 });
});

for (const name of ['Error', 'TimeoutError']) {
  test(`原生值读取 ${name} 不降级为空文本`, async () => {
    const failure = Object.assign(new Error('synthetic native read failed'), { name });
    const h = harness({ input: async () => { throw failure; }, value: '' });
    await assert.rejects(h.action.execute({ selector, property: 'value' }, context()), error => {
      assert.equal(error.details.original_error, failure.message);
      assert.equal(error.details.read_value_timeout_ms, name === 'TimeoutError' ? 1500 : undefined);
      return true;
    });
    assert.deepEqual(h.calls, { input: 1, text: 0 });
  });
}

test('富文本在回退取字时消失，保留原生错误而非返回空白', async () => {
  const failure = new Error('synthetic editor disappeared');
  const h = harness({ rich: true, text: async () => { throw failure; } });
  await assert.rejects(h.action.execute({ selector, property: 'value' }, context()), error => error.cause === failure);
  assert.deepEqual(h.calls, { input: 1, text: 1 });
});

test('富文本原始内容为null时明确报错，不伪装为空草稿', async () => {
  const h = harness({ rich: true, text: async () => null });
  await assert.rejects(h.action.execute({ selector, property: 'value' }, context()), error => error.details.original_error.includes('原始草稿尚未读到'));
  assert.deepEqual(h.calls, { input: 1, text: 1 });
});

for (const timing of ['before', 'during', 'before_fallback']) {
  test(`取消发生于 ${timing} 时不继续旧读取或返回草稿`, async () => {
    const controller = new AbortController();
    const h = harness({ input: async () => {
      controller.abort();
      if (timing === 'before_fallback') throw new Error(nonInputMessage);
      return '晚到草稿';
    } });
    const ctx = { ...context(), signal: controller.signal };
    if (timing === 'before') controller.abort();
    await assert.rejects(actionContextStorage.run(ctx, () => h.action.execute({ selector, property: 'value' }, ctx)), { code: 'ACTION_CANCELLED' });
    assert.deepEqual(h.calls, { input: timing === 'before' ? 0 : 1, text: 0 });
  });
}

test('原子值读取保留类型错误以外的错误对象，不用文本覆盖', async () => {
  const failure = new Error('synthetic connection closed');
  const h = harness({ input: async () => { throw failure; } });
  await assert.rejects(new ReadPrimitive().editableValue(h.locator), error => error === failure);
  assert.deepEqual(h.calls, { input: 1, text: 0 });
});

for (const value of ['原草稿\n第二行', '已变化']) {
  test(`回车前用相同富文本读取核验草稿：${value}`, async () => {
    const h = harness({ rich: true, value });
    let presses = 0;
    const page = { url: () => 'https://example.test/chat' };
    const action = new KeyboardAction({ requirePage: async () => page }, { press: async () => { presses++; } }, logger,
      { resolve: async () => ({ locator: h.locator }) });
    const request = { key: 'Enter', target: selector, expected_page_url: page.url(), expected_value: '原草稿\n第二行', require_focus: true };
    if (value === '原草稿\n第二行') assert.equal((await action.press(request, context('keyboard.press'))).pressed, true);
    else await assert.rejects(action.press(request, context('keyboard.press')), error => error.details.key_started === false);
    assert.equal(presses, value === '原草稿\n第二行' ? 1 : 0);
    assert.deepEqual(h.calls, { input: 1, text: 1 });
  });
}
