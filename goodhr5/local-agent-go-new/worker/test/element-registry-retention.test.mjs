// 文件作用说明：验证元素引用空闲时自动过期、突发数量上限和页面清理，不依赖真实招聘页面。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ElementRegistry } from '../dist/browser/session/element-registry.js';

const page = { isClosed: () => false };
const context = { action: 'element.find', step: 'get', trace_id: 'registry-retention' };

test('不再次访问元素引用，三十秒到期也会自动释放', t => {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: 1000 });
  const registry = new ElementRegistry();
  for (let index = 0; index < 10000; index++) registry.remember(page, { index });
  assert.equal(registry.entries.size, 10000);
  t.mock.timers.tick(30000);
  assert.equal(registry.entries.size, 0);
  assert.equal(registry.cleanupTimer, undefined);
});

test('清理旧引用不会提前删除刚生成的引用，清空后不留定时器', t => {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: 1000 });
  const registry = new ElementRegistry();
  const old = registry.remember(page, { kind: 'old' });
  t.mock.timers.tick(10000);
  const current = { kind: 'current' };
  const fresh = registry.remember(page, current);
  t.mock.timers.tick(20000);
  assert.throws(() => registry.get(old, page, context), { code: 'ELEMENT_REF_EXPIRED' });
  assert.equal(registry.get(fresh, page, context), current);
  registry.clearPage(page);
  assert.equal(registry.entries.size, 0);
  assert.equal(registry.cleanupTimer, undefined);
  t.mock.timers.tick(30000);
  assert.equal(registry.entries.size, 0);
});

test('十万次突发查找最多保留一万个引用，仍可正常读取最新引用', () => {
  const registry = new ElementRegistry();
  try {
    const old = registry.remember(page, {});
    let latest;
    for (let index = 0; index < 100000; index++) latest = registry.remember(page, { index });
    assert.equal(registry.entries.size, 10000);
    assert.throws(() => registry.get(old, page, context), { code: 'ELEMENT_REF_EXPIRED' });
    assert.equal(registry.get(latest, page, context).index, 99999);
  } finally { registry.clear(); }
});
