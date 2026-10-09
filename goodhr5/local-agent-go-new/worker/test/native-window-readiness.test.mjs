// 文件作用：验证遮挡失焦不会拦截原生输入，并保留最小化恢复、隐藏容器、错误标签和超时保护，不操作招聘页面脚本。
import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { readFileSync } from 'node:fs';
import { normalizeWorkerError } from '../dist/errors/worker-error.js';
const helpers = readFileSync(new URL('../../internal/runtime/native_input_safety.js', import.meta.url), 'utf8');
const wheel = readFileSync(new URL('../../internal/runtime/wheel_safety.js', import.meta.url), 'utf8');

/** fixture 创建可恢复或恢复失败的原生窗口，真实轮询并记录输入次数。 */
function fixture({ minimized = false, hidden = false, active = !hidden, focused = !hidden,
  browserHidden = false, headless = false, recover = true, delayed = false } = {}) {
  let observer;
  const sent = [];
  const attempts = { restore: 0, focus: 0 };
  const browser = { hidden: browserHidden, docShellIsActive: active, scrollRectIntoViewIfNeeded() {},
    getBoundingClientRect: () => ({ left: 0, top: 70, width: 1200, height: 800 }) };
  const win = { windowState: minimized ? 2 : 0, STATE_MINIMIZED: 2, devicePixelRatio: 1,
    document: { hasFocus: () => focused, hidden },
    restore() { attempts.restore++; if (recover) win.windowState = 0; },
    focus() {
      attempts.focus++;
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
  // 此夹具只执行激活回调，不模拟原始 activateAndRun 内部聚焦；次数仅统计本补丁额外的恢复动作。
  handler._pageTarget = { _window: win, _linkedBrowser: browser, _tab: tab, ensureContextMenuClosed() {}, activateAndRun: callback => callback() };
  const ensureWindowReady = handler._goodhrEnsureWindowReady.bind(handler);
  // 测试缩短未就绪轮询预算，仍执行正式窗口检查和实际输入保护流程。
  handler._goodhrEnsureWindowReady = (diagnostic, guard, timeoutMS = 40) => ensureWindowReady(diagnostic, guard, timeoutMS);
  return { handler, win, browser, sent, attempts,
    run: () => handler['Page.dispatchWheelEvent']({ x: 200, y: 300, deltaX: 0, deltaY: 150, deltaZ: 0, modifiers: 0 }) };
}

test('最小化窗口恢复为真实活动状态后，滚轮只发送一次', async () => {
  const value = fixture({ minimized: true, hidden: true });
  await value.run();
  assert.equal(value.win.windowState, 0);
  assert.equal(value.browser.docShellIsActive, true);
  assert.equal(value.sent.length, 1);
});

test('被遮挡或失焦但标签正确的普通窗口直接输入，不追加聚焦或还原', async () => {
  for (const state of [
    { hidden: true, active: false, focused: true },
    { hidden: true, active: false, focused: false },
    { hidden: false, active: false, focused: false },
  ]) {
    const value = fixture({ ...state, recover: false });
    await value.run();
    assert.equal(value.sent.length, 1);
    assert.equal(value.win.document.hidden, state.hidden);
    assert.equal(value.browser.docShellIsActive, state.active);
    assert.deepEqual(value.attempts, { restore: 0, focus: 0 });
  }
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

test('输入途中窗口被遮挡或失焦仍能继续，不依赖活动标志恢复', async () => {
  const value = fixture();
  const diagnostic = value.handler._goodhrInputDiagnostic('mousemove', 100, 100);
  let count = 0;
  await value.handler._goodhrRunInput(diagnostic, async guard => {
    guard(); count++;
    value.win.document.hidden = true;
    value.browser.docShellIsActive = false;
    guard(); count++;
  });
  assert.equal(count, 2);
  assert.deepEqual(value.attempts, { restore: 0, focus: 0 });
});

test('隐藏内容容器或错误标签仍拒绝发送滚轮', async () => {
  for (const state of ['browser_hidden', 'tab_unselected', 'browser_unselected']) {
    const value = fixture({ recover: false });
    if (state === 'browser_hidden') value.browser.hidden = true;
    if (state === 'tab_unselected') value.win.gBrowser.selectedTab = {};
    if (state === 'browser_unselected') value.win.gBrowser.selectedBrowser = {};
    await assert.rejects(value.run(), /GOODHR_MOUSE_WINDOW_NOT_READY:wheel/);
    assert.equal(value.sent.length, 0);
  }
});

test('输入途中最小化或隐藏内容容器时拒绝后续输入，不重放先前动作', async () => {
  for (const state of ['minimized', 'browser_hidden']) {
    const value = fixture();
    const diagnostic = value.handler._goodhrInputDiagnostic('mousemove', 100, 100);
    let count = 0;
    await assert.rejects(value.handler._goodhrRunInput(diagnostic, async guard => {
      guard(); count++;
      if (state === 'minimized') value.win.windowState = value.win.STATE_MINIMIZED;
      if (state === 'browser_hidden') value.browser.hidden = true;
      guard(); count++;
    }), /GOODHR_MOUSE_WINDOW_NOT_READY:mousemove/);
    assert.equal(count, 1);
  }
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

test('窗口被遮挡时仍保留底层刷新错误，滚轮未发送', async () => {
  const value = fixture();
  value.win.windowUtils.flushApzRepaints = () => {
    value.win.document.hidden = true; value.browser.docShellIsActive = false;
    throw new Error('synthetic APZ failure');
  };
  await assert.rejects(value.run(), /synthetic APZ failure/);
  assert.equal(value.sent.length, 0);
});

test('窗口在刷新等待期间最小化时，以窗口根因停止并且不发送滚轮', async () => {
  const value = fixture();
  value.win.windowUtils.flushApzRepaints = () => {
    value.win.windowState = value.win.STATE_MINIMIZED;
    throw new Error('synthetic APZ failure');
  };
  await assert.rejects(value.run(), /GOODHR_MOUSE_WINDOW_NOT_READY:wheel/);
  assert.equal(value.sent.length, 0);
});
