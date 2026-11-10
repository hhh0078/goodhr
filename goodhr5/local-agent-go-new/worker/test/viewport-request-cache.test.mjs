// 文件作用：验证自然窗口测量仅在同一ALS请求和页面内复用，窗口跨请求变化与失败不会沿用旧尺寸，不连接真实浏览器。
import test from 'node:test';
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { ViewportPrimitive } from '../dist/browser/primitives/viewport.js';
import { actionContextStorage } from '../dist/logging/operation.js';

/** pngHeader 只构造视口读取所需的PNG尺寸头，避免引入图片依赖。 */
function pngHeader(width, height) {
  const png = Buffer.alloc(24);
  png.write('PNG', 1, 'ascii');
  png.writeUInt32BE(width, 16);
  png.writeUInt32BE(height, 20);
  return png;
}

/** request 创建独立请求对象；追踪编号可相同，但请求生命周期不能共用尺寸缓存。 */
function request(trace = randomUUID()) {
  return { action: 'element.click', trace_id: trace, started_at: Date.now() };
}

/** fixture 模拟标准截图和HTML几何读取，故意不提供任何改窗口或执行页面脚本的能力。 */
function fixture() {
  const state = { configured: null, png: pngHeader(1280, 720), rootWidth: 1280, captures: [], bounds: 0, captureFailures: 0, boundsFailures: 0, gate: undefined };
  const page = {
    viewportSize() { return state.configured; },
    async screenshot(options) {
      state.captures.push(options);
      assert.deepEqual(options, { type: 'png', scale: 'css', timeout: 5000 });
      if (state.captureFailures-- > 0) throw new Error('capture failed');
      if (state.gate) await state.gate;
      return state.png;
    },
    locator(selector) {
      assert.equal(selector, 'html');
      return { async boundingBox() {
        state.bounds++;
        if (state.boundsFailures-- > 0) throw new Error('layout unavailable');
        return { x: 0, y: -3000, width: state.rootWidth, height: 6000 };
      } };
    },
  };
  return { state, page };
}

test('配置尺寸直接返回，同请求调用不会截图或读取HTML', async () => {
  const f = fixture(); f.state.configured = { width: 900, height: 600 };
  await actionContextStorage.run(request(), async () => {
    assert.deepEqual(await new ViewportPrimitive().size(f.page), f.state.configured);
    assert.deepEqual(await new ViewportPrimitive().size(f.page), f.state.configured);
  });
  assert.equal(f.state.captures.length, 0);
  assert.equal(f.state.bounds, 0);
});

test('同一请求跨Primitive实例和并发读取只测一次，保留真实HTML换算', async () => {
  const f = fixture(); f.state.png = pngHeader(2560, 1440);
  let release;
  f.state.gate = new Promise(resolve => { release = resolve; });
  await actionContextStorage.run(request(), async () => {
    const a = new ViewportPrimitive().size(f.page);
    const b = new ViewportPrimitive().size(f.page);
    assert.equal(f.state.captures.length, 1);
    release();
    assert.deepEqual(await a, { width: 1280, height: 720 });
    assert.deepEqual(await b, { width: 1280, height: 720 });
    assert.deepEqual(await new ViewportPrimitive().size(f.page), { width: 1280, height: 720 });
  });
  assert.equal(f.state.captures.length, 1);
  assert.equal(f.state.bounds, 1);
});

test('跨请求重新测量自然窗口变化，即使trace编号相同', async () => {
  const f = fixture(), primitive = new ViewportPrimitive(), trace = randomUUID();
  assert.deepEqual(await actionContextStorage.run(request(trace), () => primitive.size(f.page)), { width: 1280, height: 720 });
  f.state.png = pngHeader(900, 640); f.state.rootWidth = 900;
  assert.deepEqual(await actionContextStorage.run(request(trace), () => primitive.size(f.page)), { width: 900, height: 640 });
  assert.equal(f.state.captures.length, 2);
  assert.equal(f.state.bounds, 2);
});

test('同一请求中不同页面各自测量，无ALS时每次重新测量', async () => {
  const a = fixture(), b = fixture(), primitive = new ViewportPrimitive();
  b.state.png = pngHeader(800, 600); b.state.rootWidth = 800;
  await actionContextStorage.run(request(), async () => {
    assert.deepEqual(await primitive.size(a.page), { width: 1280, height: 720 });
    assert.deepEqual(await primitive.size(b.page), { width: 800, height: 600 });
  });
  await primitive.size(a.page); await primitive.size(a.page);
  assert.equal(a.state.captures.length, 3);
  assert.equal(b.state.captures.length, 1);
});

test('截图失败不能缓存，同请求重试成功后才复用', async () => {
  const f = fixture(); f.state.captureFailures = 1;
  await actionContextStorage.run(request(), async () => {
    await assert.rejects(new ViewportPrimitive().size(f.page), /capture failed/);
    assert.deepEqual(await new ViewportPrimitive().size(f.page), { width: 1280, height: 720 });
    await new ViewportPrimitive().size(f.page);
  });
  assert.equal(f.state.captures.length, 2);
  assert.equal(f.state.bounds, 1);
});

for (const invalid of [Buffer.from('bad-png'), pngHeader(0, 720)]) {
  test(`无效截图尺寸不能缓存：${invalid.length === 24 ? '零宽度' : '格式错误'}`, async () => {
    const f = fixture(); f.state.png = invalid;
    await actionContextStorage.run(request(), async () => {
      await assert.rejects(new ViewportPrimitive().size(f.page), /视口尺寸/);
      f.state.png = pngHeader(1280, 720);
      assert.deepEqual(await new ViewportPrimitive().size(f.page), { width: 1280, height: 720 });
    });
    assert.equal(f.state.captures.length, 2);
  });
}

test('HTML暂不可读时保留截图尺寸回退，但下次同请求仍重测', async () => {
  const f = fixture(); f.state.boundsFailures = 1;
  await actionContextStorage.run(request(), async () => {
    assert.deepEqual(await new ViewportPrimitive().size(f.page), { width: 1280, height: 720 });
    assert.deepEqual(await new ViewportPrimitive().size(f.page), { width: 1280, height: 720 });
    await new ViewportPrimitive().size(f.page);
  });
  assert.equal(f.state.captures.length, 2);
  assert.equal(f.state.bounds, 2);
});
