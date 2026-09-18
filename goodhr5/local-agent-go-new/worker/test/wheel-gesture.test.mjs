// 文件作用：验证拟人滚轮保留精确距离、不反向、节奏有变化，取消时停止后续输入。
import test from 'node:test';
import assert from 'node:assert/strict';
import { wheelPlan, variedDistance, wheelGesture } from '../dist/browser/actions/wheel-gesture.js';

test('滚轮分段精确保留正负距离并具有不同节奏', () => {
  for (const distance of [-520, -1, 1, 520]) {
    const plan = wheelPlan(0, distance, () => 0.4);
    assert.equal(plan.reduce((sum, step) => sum + step.y, 0), distance);
    assert.ok(plan.every(step => step.y === 0 || Math.sign(step.y) === Math.sign(distance)));
  }
  assert.notDeepEqual(wheelPlan(0, 520, () => 0), wheelPlan(0, 520, () => 0.9));
  assert.ok(variedDistance(300, () => 0) < variedDistance(300, () => 0.9));
});

test('取消后不发送下一段滚轮', async () => {
  const controller = new AbortController();
  let calls = 0;
  await assert.rejects(wheelGesture({ async wheel() { calls++; controller.abort(); } }, {}, 0, 520, { signal: controller.signal }, { info() {} }), { name: 'AbortError' });
  assert.equal(calls, 1);
});
