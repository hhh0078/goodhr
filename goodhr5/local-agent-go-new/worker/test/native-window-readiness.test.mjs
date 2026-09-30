// 文件作用：验证浏览器内部窗口恢复与原生输入保护，覆盖最小化、隐藏、无窗口运行和恢复失败，不操作招聘页面脚本。
import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { readFileSync } from 'node:fs';
import { normalizeWorkerError } from '../dist/errors/worker-error.js';
const helpers = readFileSync(new URL('../../internal/runtime/native_input_safety.js', import.meta.url), 'utf8');
const wheel = readFileSync(new URL('../../internal/runtime/wheel_safety.js', import.meta.url), 'utf8');

/** fixture 创建可恢复或恢复失败的原生窗口，真实轮询并记录输入次数。 */
function fixture({ minimized = false, hidden = false, headless = false, recover = true, delayed = false } = {}) {
  let observer;
  const sent = [];
  const browser = { hidden: false, docShellIsActive: !hidden, scrollRectIntoViewIfNeeded() {},
    getBoundingClientRect: () => ({ left: 0, top: 70, width: 1200, height: 800 }) };
  const win = { windowState: minimized ? 2 : 0, STATE_MINIMIZED: 2, devicePixelRatio: 1,
    document: { hasFocus: () => !win.document.hidden, hidden },
    restore() { if (recover) win.windowState = 0; },
    focus() {
      if (!recover) return;
      const activate = () => { win.document.hidden = false; browser.docShellIsActive = true; };
      if (delayed) setTimeout(activate, 20); else activate();
    },
    windowUtils: { flushApzRepaints() { observer.observe(); return true; }, sendWheelEvent(...args) { sent.push(args); } } };
  const tab = {};
  win.gBrowser = { selectedBrowser: browser, selectedTab: tab };
  const sandbox = vm.createContext({ Cc: {
    '@mozilla.org/gfx/info;1': { getService: () => ({ isHeadless: headless }) },
    '@mozilla.org/observer-service;1': { getService: () => ({ addObserver(value) { observer = value; }, removeObserver() { observer = undefined; } }) },
  }, Ci: { nsIObserverService: {}, nsIGfxInfo: {} }, ChromeUtils: { importESModule: () => ({ setTimeout, clearTimeout }) } });
  const Safety = vm.runInContext(`(class { ${helpers}\n${wheel} })`, sandbox);
  const handler = new Safety();
  handler._pageTarget = { _window: win, _linkedBrowser: browser, _tab: tab, ensureContextMenuClosed() {}, activateAndRun: callback => callback() };
  return { handler, win, browser, sent,
    run: () => handler['Page.dispatchWheelEvent']({ x: 200, y: 300, deltaX: 0, deltaY: 150, deltaZ: 0, modifiers: 0 }) };
}

test('最小化窗口恢复为真实活动状态后，滚轮只发送一次', async () => {
  const value = fixture({ minimized: true, hidden: true });
  await value.run();
  assert.equal(value.win.windowState, 0);
  assert.equal(value.browser.docShellIsActive, true);
  assert.equal(value.sent.length, 1);
});

test('未最小化但隐藏的窗口等待活动状态恢复后才输入', async () => {
  const value = fixture({ hidden: true, delayed: true });
  await value.run();
  assert.equal(value.sent.length, 1);
  assert.equal(value.win.document.hidden, false);
});

test('恢复失败明确返回窗口错误，零输入且上层不会重试其他候选人', async () => {
  const value = fixture({ hidden: true, minimized: true, recover: false });
  const diagnostic = value.handler._goodhrInputDiagnostic('wheel', 200, 300);
  await assert.rejects(value.handler._goodhrEnsureWindowReady(diagnostic, () => {}, 40), error => {
    const normalized = normalizeWorkerError(error, { action: 'page.scroll', step: 'wheel', trace_id: 'window', code: 'SCROLL_FAILED', retryable: true });
    assert.equal(normalized.code, 'BROWSER_WINDOW_NOT_READY');
    assert.equal(normalized.retryable, false);
    assert.equal(normalized.details.native_mouse.window_before.window_minimized, true);
    assert.equal(normalized.details.native_mouse.window_after_restore.browser_active, false);
    assert.ok(normalized.details.native_mouse.window_ready_wait_ms >= 40);
    assert.equal(normalized.details.native_mouse.window_focus_attempts, 1);
    assert.equal(normalized.details.native_mouse.events_sent, 0);
    return true;
  });
  assert.equal(value.sent.length, 0);
});

test('实际无窗口模式允许隐藏的原生窗口，保留原有截图自动化能力', async () => {
  const value = fixture({ hidden: true, headless: true, recover: false });
  await value.run();
  assert.equal(value.sent.length, 1);
});

test('输入途中窗口隐藏时拒绝后续输入，不重放先前已经发送的动作', async () => {
  const value = fixture();
  const diagnostic = value.handler._goodhrInputDiagnostic('mousemove', 100, 100);
  let count = 0;
  await assert.rejects(value.handler._goodhrRunInput(diagnostic, async guard => {
    guard(); count++;
    value.win.document.hidden = true;
    value.browser.docShellIsActive = false;
    guard(); count++;
  }), /GOODHR_MOUSE_WINDOW_NOT_READY:mousemove/);
  assert.equal(count, 1);
});

test('鼠标松开已收到回执后允许新标签选中，详情点击不被误判成窗口故障', async () => {
  const value = fixture();
  const diagnostic = value.handler._goodhrInputDiagnostic('mouseup', 100, 100);
  await value.handler._goodhrRunInput(diagnostic, async guard => {
    guard();
    diagnostic.events_sent = 1;
    diagnostic.events_acknowledged = 1;
    value.win.gBrowser.selectedTab = {};
    value.win.gBrowser.selectedBrowser = {};
  });
  assert.equal(diagnostic.events_acknowledged, 1);
  assert.notEqual(value.win.gBrowser.selectedTab, value.handler._pageTarget._tab);
});

test('鼠标松开未收到回执前切换标签仍拒绝继续输入', async () => {
  const value = fixture();
  const diagnostic = value.handler._goodhrInputDiagnostic('mouseup', 100, 100);
  await assert.rejects(value.handler._goodhrRunInput(diagnostic, async guard => {
    value.win.gBrowser.selectedTab = {};
    guard();
  }), /GOODHR_MOUSE_WINDOW_NOT_READY:mouseup/);
  assert.equal(diagnostic.events_acknowledged, 0);
});

test('鼠标松开已收到回执后保留原始操作错误', async () => {
  const value = fixture();
  const diagnostic = value.handler._goodhrInputDiagnostic('mouseup', 100, 100);
  await assert.rejects(value.handler._goodhrRunInput(diagnostic, async guard => {
    guard();
    diagnostic.events_acknowledged = 1;
    value.win.gBrowser.selectedTab = {};
    throw new Error('新标签等待失败');
  }), /新标签等待失败/);
});

test('窗口在刷新等待期间隐藏时，以窗口根因停止并且不发送滚轮', async () => {
  const value = fixture();
  value.win.windowUtils.flushApzRepaints = () => {
    value.win.document.hidden = true; value.browser.docShellIsActive = false;
    throw new Error('synthetic APZ failure');
  };
  await assert.rejects(value.run(), /GOODHR_MOUSE_WINDOW_NOT_READY:wheel/);
  assert.equal(value.sent.length, 0);
});
