// 文件作用：模拟原生浏览器刷新通知和激活等待，防止滚轮漏通知及超时后迟到输入再次发生。
import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { readFileSync } from 'node:fs';
const helpers = readFileSync(new URL('../../internal/runtime/native_input_safety.js', import.meta.url), 'utf8');
const wheel = readFileSync(new URL('../../internal/runtime/wheel_safety.js', import.meta.url), 'utf8');

/** fixture 创建独立协议环境，把生产定时器缩短为测试等待，同时保留回调和观察器真实顺序。 */
function fixture(mode) {
  let observer, activateLater;
  const sent = [];
  const win = { windowState: 0, STATE_MINIMIZED: 2, devicePixelRatio: 1.5,
    document: { hasFocus: () => true, hidden: false }, windowUtils: {
      flushApzRepaints() { if (mode === 'immediate') observer.observe(); return mode !== 'none'; },
      sendWheelEvent(...args) { sent.push(args); },
    } };
  const browser = { docShellIsActive: true, hidden: false, scrollRectIntoViewIfNeeded() {},
    getBoundingClientRect: () => ({ left: 0, top: 80, width: 1200, height: 800 }) };
  const tab = {};
  win.gBrowser = { selectedBrowser: browser, selectedTab: tab };
  const sandbox = vm.createContext({ Cc: { '@mozilla.org/observer-service;1': { getService: () => ({
    addObserver(value) { observer = value; }, removeObserver(value) { assert.equal(observer, value); observer = undefined; },
  }) } }, Ci: { nsIObserverService: {} }, ChromeUtils: { importESModule: () => ({
    setTimeout: (fn, ms) => setTimeout(fn, ms === 15000 ? 30 : 5), clearTimeout,
  }) } });
  const Safety = vm.runInContext(`(class { ${helpers}\n${wheel} })`, sandbox);
  const handler = new Safety();
  handler._pageTarget = { _window: win, _linkedBrowser: browser, _tab: tab, ensureContextMenuClosed() {},
    activateAndRun(callback) { if (mode === 'activation') return new Promise(resolve => { activateLater = () => Promise.resolve(callback()).then(resolve); }); return callback(); } };
  return { run: () => handler['Page.dispatchWheelEvent']({ x: 300, y: 400, deltaX: 0, deltaY: 100, deltaZ: 0, modifiers: 0 }),
    sent, observer: () => observer, activateLater: () => activateLater() };
}

test('同步刷新通知不会漏掉，滚轮准确发送一次并清理订阅', async () => {
  const value = fixture('immediate'); await value.run();
  assert.equal(value.sent.length, 1); assert.equal(value.sent[0][3], 100); assert.equal(value.observer(), undefined);
});

test('没有刷新通知时停止在发送前，并带出窗口、滚轮和等待阶段', async () => {
  const value = fixture('missing');
  await assert.rejects(value.run(), error => {
    const evidence = JSON.parse(error.message.split(' GOODHR_MOUSE_DIAGNOSTICS=')[1]);
    assert.equal(evidence.stage, 'apz_repaint'); assert.equal(evidence.delta_y, 100);
    assert.equal(evidence.events_sent, 0); assert.equal(evidence.window_at_failure.browser_active, true);
    assert.equal(evidence.window_before.tab_selected, true); return true;
  });
  assert.equal(value.sent.length, 0); assert.equal(value.observer(), undefined);
});

test('激活卡住时明确退出，迟到回调不得再发送滚轮', async () => {
  const value = fixture('activation');
  await assert.rejects(value.run(), /GOODHR_MOUSE_INPUT_TIMEOUT:wheel/);
  await assert.rejects(value.activateLater(), /GOODHR_MOUSE_INPUT_TIMEOUT:wheel/);
  assert.equal(value.sent.length, 0);
});
