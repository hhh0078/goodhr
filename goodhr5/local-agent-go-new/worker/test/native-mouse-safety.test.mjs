// 文件作用：模拟浏览器原生鼠标回执，验证重复像素不会卡死、回执丢失会退出且停止后续输入。
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../../internal/runtime/mouse_safety.js', import.meta.url), 'utf8');

/** fixture 提供没有页面脚本的协议单元测试环境，模拟浏览器仅为有效移动发送回执。 */
function fixture(missing = false, trajectory = [0,0, 0.1,0.1, 8.2,8.2, 8.3,8.3, 100,100, 10,10, 10,10], repaint = 'none') {
  const sent = [];
  let disposed = false;
  let observer;
  const observerService = {
    addObserver(value) { observer = value; },
    removeObserver(value) { assert.equal(observer, value); observer = undefined; },
  };
  const target = {
    _lastTrackedPos: { x: 0, y: 0 },
    _pendingEventWatchers: new Set(),
    _pageTarget: { _linkedBrowser: {
      scrollRectIntoViewIfNeeded() {},
      getBoundingClientRect: () => ({ left: 0, top: 0, width: 100, height: 100 }),
    } },
  };
  const context = vm.createContext({
    win: { windowUtils: {
      flushApzRepaints() {
        if (repaint === 'immediate') observer.observe();
        return repaint !== 'none';
      },
      jugglerSendMouseEvent(type, x, y) { sent.push({type,x,y}); return sent.length; },
    } },
    x: 10, y: 10, button: 0, clickCount: 0, modifiers: 0, buttons: 0,
    Cc: { '@mozilla.org/observer-service;1': { getService: () => observerService } },
    Ci: { nsIObserverService: {} },
    ChromeUtils: {
      importESModule: () => ({ clearTimeout }),
      camouGetBool: () => true,
      camouGetMouseTrajectory: () => trajectory,
    },
    EventWatcher: class {
      ensureEvent() { return missing ? new Promise(() => {}) : Promise.resolve(); }
      dispose() { disposed = true; }
    },
    setTimeout: (fn, ms) => setTimeout(fn, ms === 5000 ? 5 : 0), clearTimeout,
    target,
  });
  const run = vm.runInContext(`(async function() { ${source}\n await sendEvents(['mousemove']); }).bind(target)`, context);
  return { run, sent, disposed: () => disposed, observer: () => observer };
}

test('轨迹重复像素及越界点不发送，仍能精确到达终点', async () => {
  const value = fixture();
  await value.run();
  assert.deepEqual(value.sent, [{ type: 'mousemove', x: 8, y: 8 }, { type: 'mousemove', x: 10, y: 10 }]);
  assert.equal(value.disposed(), true);
});

test('丢失回执时明确失败、清理观察器，不再发送剩余轨迹', async () => {
  const value = fixture(true);
  await assert.rejects(value.run(), /GOODHR_MOUSE_ACK_TIMEOUT/);
  assert.equal(value.sent.length, 1);
  assert.equal(value.disposed(), true);
});

test('窗口边缘的中间轨迹被跳过，仍会到达指定终点', async () => {
  const value = fixture(false, [0,0, 99.8,99.8, 10,10]);
  await value.run();
  assert.deepEqual(value.sent, [{ type: 'mousemove', x: 10, y: 10 }]);
});

test('合成器同步返回通知时不会漏掉，结束后解除订阅', async () => {
  const value = fixture(false, [0,0, 10,10], 'immediate');
  await value.run();
  assert.equal(value.sent.length, 1);
  assert.equal(value.observer(), undefined);
});

test('合成器未返回通知时停止且不发送鼠标事件，解除订阅', async () => {
  const value = fixture(false, [0,0, 10,10], 'missing');
  await assert.rejects(value.run(), /GOODHR_MOUSE_APZ_TIMEOUT/);
  assert.equal(value.sent.length, 0);
  assert.equal(value.observer(), undefined);
});
