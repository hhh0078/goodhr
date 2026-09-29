// 文件作用：修正浏览器原生滚轮的刷新通知顺序，并记录激活、刷新和发送阶段，不操作招聘页面脚本。
  async ['Page.dispatchWheelEvent']({ x, y, button, deltaX, deltaY, deltaZ, modifiers }) {
    const diagnostic = this._goodhrInputDiagnostic('wheel', x, y, { delta_x: deltaX, delta_y: deltaY });
    await this._goodhrRunInput(diagnostic, async (guard) => {
      this._pageTarget.ensureContextMenuClosed();
      guard();
      diagnostic.stage = 'browser_bounds';
      const browser = this._pageTarget._linkedBrowser;
      browser.scrollRectIntoViewIfNeeded(x, y, 0, 0);
      const boundingBox = browser.getBoundingClientRect();
      const win = this._pageTarget._window;
      const { setTimeout, clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs');
      const observerService = Cc['@mozilla.org/observer-service;1'].getService(Ci.nsIObserverService);
      let repaintTimer;
      let repaintDone;
      const repaintPromise = new Promise(resolve => { repaintDone = resolve; });
      const observer = { observe() { diagnostic.apz_repaint_received = true; repaintDone(); } };
      // 必须先订阅再触发刷新，同步完成通知也不会丢失。
      observerService.addObserver(observer, 'apz-repaints-flushed');
      const repaintStarted = Date.now();
      try {
        diagnostic.stage = 'apz_repaint';
        diagnostic.apz_flush_requested = true;
        diagnostic.apz_flush_result = win.windowUtils.flushApzRepaints();
        if (diagnostic.apz_flush_result) {
          await Promise.race([
            repaintPromise,
            new Promise((_, reject) => { repaintTimer = setTimeout(() => reject(new Error(
              'GOODHR_MOUSE_APZ_TIMEOUT:wheel GOODHR_MOUSE_DIAGNOSTICS=' + JSON.stringify({ ...diagnostic,
                apz_wait_ms: Date.now() - repaintStarted, elapsed_ms: Date.now() - diagnostic.input_started_at,
                window_at_failure: this._goodhrWindowSnapshot(),
              }))), 5000); }),
          ]);
        }
      } finally {
        diagnostic.apz_wait_ms = Date.now() - repaintStarted;
        clearTimeout(repaintTimer);
        observerService.removeObserver(observer, 'apz-repaints-flushed');
      }
      guard();
      diagnostic.stage = 'wheel_dispatch';
      win.windowUtils.sendWheelEvent(x + boundingBox.left, y + boundingBox.top,
        deltaX, deltaY, deltaZ, 0, modifiers,
        deltaX > 0 ? Math.floor(deltaX) : Math.ceil(deltaX),
        deltaY > 0 ? Math.floor(deltaY) : Math.ceil(deltaY), 0);
      diagnostic.events_sent++;
    });
  }
