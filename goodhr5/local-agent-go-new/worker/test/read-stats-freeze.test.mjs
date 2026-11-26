// 文件作用说明：验证读取统计未知值、去重和截止/取消/冻结边界，不发出浏览器命令。
import assert from 'node:assert/strict';
import test from 'node:test';
import { randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { completeReadItem, finishRequestProgress, planReadStats, requestReadStats, returnReadStats, startReadStats, startRequestProgress } from '../dist/logging/request-progress.js';

/** context 建立唯一请求，耗时起点刻意包含排队前的时间。 */
function context() { return { request_id: randomUUID(), trace_id: 'synthetic', action: 'element.find_all', started_at: Date.now() - 200, deadline_at: Date.now() + 5000 }; }

test('未启用不统计，启用后在计划未知时只返回编号和完整请求耗时', () => {
  const ctx = context();
  startRequestProgress(ctx);
  assert.equal(requestReadStats(ctx.request_id), undefined);
  startReadStats(ctx);
  completeReadItem(ctx, 0);
  const stats = requestReadStats(ctx.request_id);
  assert.deepEqual(Object.keys(stats).sort(), ['elapsed_ms', 'request_id']);
  assert.ok(stats.elapsed_ms >= 200);
});

test('乱序和重复完成按独立槽位去重，返回数量只在完整成功后出现', () => {
  const ctx = context(); startRequestProgress(ctx); startReadStats(ctx); planReadStats(ctx, 3);
  for (const index of [2, 2, 0, -1, 3]) completeReadItem(ctx, index);
  assert.equal(requestReadStats(ctx.request_id).raw_completed_count, 2);
  assert.equal(requestReadStats(ctx.request_id).returned_count, undefined);
  completeReadItem(ctx, 1); returnReadStats(ctx, 3);
  assert.equal(requestReadStats(ctx.request_id).raw_completed_count, 3);
  assert.equal(requestReadStats(ctx.request_id).returned_count, 3);
});

for (const boundary of ['cancel', 'deadline', 'freeze']) {
  test(`${boundary}之后的完成、返回和计划更新均不能改写快照`, async () => {
    const ctx = context(), controller = new AbortController(); ctx.signal = controller.signal;
    startRequestProgress(ctx); startReadStats(ctx); planReadStats(ctx, 2); completeReadItem(ctx, 0);
    if (boundary === 'cancel') controller.abort();
    if (boundary === 'deadline') ctx.deadline_at = Date.now() - 1;
    if (boundary === 'freeze') finishRequestProgress(ctx.request_id);
    completeReadItem(ctx, 1); returnReadStats(ctx, 2); planReadStats(ctx, 10);
    finishRequestProgress(ctx.request_id);
    const frozen = requestReadStats(ctx.request_id);
    await delay(10);
    completeReadItem(ctx, 1); returnReadStats(ctx, 2); startReadStats(ctx);
    assert.deepEqual(requestReadStats(ctx.request_id), frozen);
    assert.equal(frozen.planned_count, 2);
    assert.equal(frozen.raw_completed_count, 1);
    assert.equal(frozen.returned_count, undefined);
  });
}
