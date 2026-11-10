// 文件作用：用生产LocatorPrimitive验证BOSS原消息先固定CSS行、再过滤文本，避免全局Index落在过滤后的小集合；不控制真实页面。
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';

const context = { trace_id: 'boss-source-pin-contract', action: 'element.find', step: 'find', require_unique: true };
const produced = process.env.GOODHR_BOSS_PIN_SELECTOR_INPUT
  ? JSON.parse(readFileSync(process.env.GOODHR_BOSS_PIN_SELECTOR_INPUT, 'utf8')) : null;

/** contractSpec 默认使用标准协议；联跑时直接读取Go生产helper生成的合成选择器，不复制其实现。 */
function contractSpec(kind) {
  return structuredClone(produced?.[kind] ?? {
    parents: [
      { selectors: [{ type: 'css', value: '.chat-root:has(span.resume[ka="get_resume_10101"]):has(span.phone[ka="get_phone_10101"]):has(span.wechat[ka="get_wechat_10101"])' }] },
      { selectors: [{ type: 'css', value: '.message-list:has(> .message-item:nth-child(4)):not(:has(> .message-item:nth-child(5)))' }] },
      { selectors: [{ type: 'css', value: ':nth-match(.message-item, 3)' }], texts: [kind === 'offer' ? '唯一原生邀约' : '唯一附件.pdf'] },
    ], target: { selectors: [{ type: 'css', value: 'test' }] }, state: 'enabled', timeout_ms: 0, description: '合成来源按钮',
  });
}

/** fixture 提供真正执行集合过滤和nth语义的标准Locator替身，不接受任意选择器就返回成功。 */
function fixture(spec, change = '') {
  // 原生卡可见innerText带换行，DOM textContent里的同一组块文本没有这些布局换行。
  const text = spec.parents.at(-1).texts.join('');
  const rows = Array.from({ length: 4 }, (_, index) => ({ type: 'row', text: index === 2 ? text : `其它消息${index}`, original: index,
    children: [{ type: 'button', text: '原生按钮', original: index, children: [] }] }));
  if (change === 'reorder') [rows[0], rows[2]] = [rows[2], rows[0]];
  if (change === 'append') rows.push({ type: 'row', text: '新来信', original: 4, children: [] });
  const list = { type: 'list', text: '', children: rows };
  const root = { type: 'root', text: '', children: [list] };
  /** query 仅实现本合同用到的CSS集合规则；无法识别的语句直接拒绝，不能吞掉位置或身份约束。 */
  function query(nodes, css) {
    if (css.startsWith('.chat-root')) {
      const identities = [...css.matchAll(/\[ka="([^"]+)"\]/g)].map(match => match[1]);
      assert.equal(identities.length, 3, '生产来源定位必须保留三ka');
      return locator(nodes.filter(node => node.type === 'root' && change !== 'identity'));
    }
    if (css.startsWith('.message-list')) {
      const counts = [...css.matchAll(/nth-child\((\d+)\)/g)].map(match => Number(match[1]));
      assert.deepEqual(counts, [4, 5], '必须同时校验原总数和不存在第N+1行');
      return locator(nodes.filter(node => node.type === 'list' && node.children.length === counts[0]));
    }
    if (css === '.message-item') return locator(nodes.filter(node => node.type === 'row'));
    const nth = /^:nth-match\(\.message-item, (\d+)\)$/.exec(css);
    if (nth) return locator(nodes.filter(node => node.type === 'row').slice(Number(nth[1]) - 1, Number(nth[1])));
    if (css === 'test') return locator(nodes.filter(node => node.type === 'button'));
    throw new Error('合同未识别的CSS，不能默认为匹配');
  }
  /** locator 真实保留每次filter后的集合，nth的索引不会偷偷恢复到过滤前。 */
  function locator(nodes) {
    return { nodes, locator: css => query(nodes.flatMap(node => node.children), css),
      filter: ({ hasText }) => locator(nodes.filter(node => typeof hasText === 'string' ? node.text.includes(hasText) : hasText.test(node.text))),
      count: async () => nodes.length, nth: index => locator(nodes.slice(index, index + 1)),
      getAttribute: async () => null, isVisible: async () => true, isEnabled: async () => true,
      boundingBox: async () => ({ x: 10, y: 10, width: 40, height: 30 }) };
  }
  return { page: { locator: css => query([root], css), viewportSize: () => ({ width: 900, height: 700 }), url: () => 'https://fixture.invalid/' }, rows };
}

for (const kind of ['offer', 'resume']) {
  test(`${kind}: 多行中只有一行匹配原文，旧filter后全局Index失败，新CSS固定原行成功`, async () => {
    const spec = contractSpec(kind);
    spec.timeout_ms = 0;
    const old = structuredClone(spec);
    old.parents.at(-1).selectors[0].value = '.message-item';
    old.parents.at(-1).index = 2;
    const oldPage = fixture(old).page;
    await assert.rejects(new LocatorPrimitive().resolve(oldPage, old, context), { code: 'ELEMENT_NOT_FOUND' });
    const current = fixture(spec);
    const resolved = await new LocatorPrimitive().resolve(current.page, spec, context);
    assert.equal(resolved.locator.nodes[0].original, 2);
    for (const change of ['reorder', 'append', 'identity']) {
      await assert.rejects(new LocatorPrimitive().resolve(fixture(spec, change).page, spec, context), { code: 'ELEMENT_NOT_FOUND' });
    }
  });
}
