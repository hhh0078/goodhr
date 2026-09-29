// 文件作用：给浏览器原生输入增加激活阶段保护和窗口证据，超时后禁止迟到的输入，不读取招聘页面。
  /** _goodhrWindowSnapshot 只读取浏览器窗口、标签页和内容容器的内部状态。 */
  _goodhrWindowSnapshot() {
    try {
      const browser = this._pageTarget._linkedBrowser;
      const win = this._pageTarget._window;
      const rect = browser.getBoundingClientRect();
      return {
        window_state: win.windowState, window_minimized: win.windowState === win.STATE_MINIMIZED,
        chrome_has_focus: win.document.hasFocus(), chrome_document_hidden: win.document.hidden,
        browser_active: browser.docShellIsActive, browser_hidden: browser.hidden,
        tab_selected: this._pageTarget._tab ? win.gBrowser?.selectedTab === this._pageTarget._tab : null,
        browser_selected: win.gBrowser ? win.gBrowser.selectedBrowser === browser : null,
        device_pixel_ratio: win.devicePixelRatio,
        browser_rect: { x: Math.round(rect.left), y: Math.round(rect.top), width: Math.round(rect.width), height: Math.round(rect.height) },
      };
    } catch (_) { return { window_state_unavailable: true }; }
  }

  /** _goodhrInputDiagnostic 创建鼠标和滚轮共用的阶段证据。 */
  _goodhrInputDiagnostic(eventTypes, x, y, extra = {}) {
    return {
      schema_version: 1, mouse_safety_version: 'v7', stage: 'activation', event_types: eventTypes,
      target_x: Math.round(x), target_y: Math.round(y), input_started_at: Date.now(),
      activation_wait_ms: 0, apz_wait_ms: 0, mouse_ack_wait_ms: 0,
      apz_flush_requested: false, apz_flush_result: null, apz_repaint_received: false,
      events_sent: 0, events_acknowledged: 0, duplicate_points_skipped: 0,
      window_before: this._goodhrWindowSnapshot(), ...extra,
    };
  }

  /** _goodhrRunInput 限制激活和整个输入的等待，回调迟到时直接退出，防止超时后继续点击。 */
  async _goodhrRunInput(diagnostic, operation) {
    const { setTimeout, clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs');
    let expired = false;
    let timer;
    /** timeoutError 带上当前阶段和窗口证据，要求上层回收无法取消的原生调用。 */
    const timeoutError = () => new Error('GOODHR_MOUSE_INPUT_TIMEOUT:' + diagnostic.event_types +
      ' GOODHR_MOUSE_DIAGNOSTICS=' + JSON.stringify({ ...diagnostic,
        elapsed_ms: Date.now() - diagnostic.input_started_at, input_timeout_ms: 15000,
        activation_wait_ms: diagnostic.stage === 'activation' ? Date.now() - diagnostic.input_started_at : diagnostic.activation_wait_ms,
        recovery_required: true, window_at_failure: this._goodhrWindowSnapshot(),
      }));
    /** guard 在每次实际输入前拒绝已经超时的回调。 */
    const guard = () => {
      if (expired || Date.now() - diagnostic.input_started_at >= 15000) { expired = true; throw timeoutError(); }
    };
    try {
      await Promise.race([
        this._pageTarget.activateAndRun(async () => {
          guard();
          diagnostic.activation_wait_ms = Date.now() - diagnostic.input_started_at;
          diagnostic.stage = 'context_menu';
          await operation(guard);
          guard();
        }, { muteNotificationsPopup: true }),
        new Promise((_, reject) => { timer = setTimeout(() => { expired = true; reject(timeoutError()); }, 15000); }),
      ]);
    } finally { clearTimeout(timer); }
  }
