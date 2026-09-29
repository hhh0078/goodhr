// 文件作用：修正原生鼠标回执等待，并在超时时携带浏览器内部刷新和窗口状态，不读取或注入招聘页面。
    const sendEvents = async (types) => {
      const { clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs');
      const startedAt = Date.now();
      let repaintStartedAt = 0;
      let ackStartedAt = 0;
      const diagnostic = {
        schema_version: 1, mouse_safety_version: 'v6', stage: 'prepare',
        event_types: types.join(','), target_x: Math.round(x), target_y: Math.round(y), buttons,
        apz_flush_requested: false, apz_flush_result: null, apz_repaint_received: false,
        apz_wait_ms: 0, mouse_ack_wait_ms: 0, events_sent: 0, events_acknowledged: 0,
        duplicate_points_skipped: 0,
      };
      /** windowSnapshot 只读取浏览器内部窗口状态，失败时保留无法读取的标记。 */
      const windowSnapshot = () => {
        try {
          const browser = this._pageTarget._linkedBrowser;
          const rect = browser.getBoundingClientRect();
          return {
            window_state: typeof win.windowState === 'number' ? win.windowState : null,
            window_minimized: typeof win.STATE_MINIMIZED === 'number' ? win.windowState === win.STATE_MINIMIZED : null,
            chrome_has_focus: typeof win.document?.hasFocus === 'function' ? win.document.hasFocus() : null,
            browser_active: typeof browser.docShellIsActive === 'boolean' ? browser.docShellIsActive : null,
            browser_hidden: typeof browser.hidden === 'boolean' ? browser.hidden : null,
            device_pixel_ratio: Number.isFinite(win.devicePixelRatio) ? win.devicePixelRatio : null,
            browser_rect: { x: Math.round(rect.left), y: Math.round(rect.top), width: Math.round(rect.width), height: Math.round(rect.height) },
          };
        } catch (_) { return { window_state_unavailable: true }; }
      };
      diagnostic.window_before = windowSnapshot();
      /** diagnosticError 保留稳定错误前缀，并携带有界的原生阶段证据供 Worker 结构化上传。 */
      const diagnosticError = (message) => new Error(message + ' GOODHR_MOUSE_DIAGNOSTICS=' + JSON.stringify({
        ...diagnostic, elapsed_ms: Date.now() - startedAt,
        apz_wait_ms: repaintStartedAt ? Date.now() - repaintStartedAt : diagnostic.apz_wait_ms,
        mouse_ack_wait_ms: ackStartedAt ? Date.now() - ackStartedAt : diagnostic.mouse_ack_wait_ms,
        window_at_failure: windowSnapshot(),
      }));
      this._pageTarget._linkedBrowser.scrollRectIntoViewIfNeeded(x, y, 0, 0);
      const boundingBox = this._pageTarget._linkedBrowser.getBoundingClientRect();
      // 先订阅再刷新，防止合成器通知先到；丢失通知时明确退出，不无限占用输入队列。
      const observerService = Cc['@mozilla.org/observer-service;1'].getService(Ci.nsIObserverService);
      let repaintTimer;
      let repaintDone;
      const repaintPromise = new Promise(resolve => { repaintDone = resolve; });
      const repaintObserver = { observe() { diagnostic.apz_repaint_received = true; repaintDone(); } };
      observerService.addObserver(repaintObserver, 'apz-repaints-flushed');
      try {
        diagnostic.stage = 'apz_repaint';
        diagnostic.apz_flush_requested = true;
        repaintStartedAt = Date.now();
        diagnostic.apz_flush_result = win.windowUtils.flushApzRepaints();
        if (diagnostic.apz_flush_result) {
          await Promise.race([
            repaintPromise,
            new Promise((_, reject) => {
              repaintTimer = setTimeout(() => reject(diagnosticError('GOODHR_MOUSE_APZ_TIMEOUT:' + types.join(','))), 5000);
            }),
          ]);
        }
      } finally {
        diagnostic.apz_wait_ms = Date.now() - repaintStartedAt;
        repaintStartedAt = 0;
        clearTimeout(repaintTimer);
        observerService.removeObserver(repaintObserver, 'apz-repaints-flushed');
      }
      const watcher = new EventWatcher(this._pageEventSink, types, this._pendingEventWatchers);
      let lastX = this._lastTrackedPos.x;
      let lastY = this._lastTrackedPos.y;
      /** sendOne 跳过重复像素，串行发送真实鼠标事件，并限制回执等待时间。 */
      const sendOne = async (eventType, eventX, eventY) => {
        // 比较和实际发送使用同一整数坐标，避免小数点在底层二次取整后变成零位移。
        const dispatchX = Math.max(Math.ceil(boundingBox.left), Math.min(
          Math.ceil(boundingBox.left + boundingBox.width) - 1, Math.round(eventX + boundingBox.left)));
        const dispatchY = Math.max(Math.ceil(boundingBox.top), Math.min(
          Math.ceil(boundingBox.top + boundingBox.height) - 1, Math.round(eventY + boundingBox.top)));
        const previousX = Math.round(lastX + boundingBox.left);
        const previousY = Math.round(lastY + boundingBox.top);
        if (eventType === 'mousemove' &&
            dispatchX === previousX && dispatchY === previousY) {
          diagnostic.duplicate_points_skipped++;
          return;
        }
        diagnostic.stage = 'dispatch';
        diagnostic.last_event = eventType;
        const jugglerEventId = win.windowUtils.jugglerSendMouseEvent(
          eventType, dispatchX, dispatchY,
          button, clickCount, modifiers, false, 0.0, 0, true, false, buttons,
          win.windowUtils.DEFAULT_MOUSE_POINTER_ID, false
        );
        diagnostic.events_sent++;
        diagnostic.last_event_id = jugglerEventId;
        if (eventType === 'mousemove') {
          lastX = dispatchX - boundingBox.left;
          lastY = dispatchY - boundingBox.top;
          this._lastTrackedPos = { x: lastX, y: lastY };
        }
        let timer;
        try {
          diagnostic.stage = 'mouse_ack';
          ackStartedAt = Date.now();
          await Promise.race([
            watcher.ensureEvent(eventType, eventObject => eventObject.jugglerEventId === jugglerEventId),
            new Promise((_, reject) => {
              timer = setTimeout(() => reject(diagnosticError('GOODHR_MOUSE_ACK_TIMEOUT:' + eventType +
                ' from=' + previousX + ',' + previousY + ' to=' + dispatchX + ',' + dispatchY)), 5000);
            }),
          ]);
          diagnostic.events_acknowledged++;
        } finally {
          diagnostic.mouse_ack_wait_ms = Date.now() - ackStartedAt;
          ackStartedAt = 0;
          clearTimeout(timer);
        }
      };
      try {
        for (const eventType of types) {
          if (eventType === 'mousemove' && ChromeUtils.camouGetBool('humanize', false)) {
            const trajectory = ChromeUtils.camouGetMouseTrajectory(lastX, lastY, x, y);
            for (let i = 2; i < trajectory.length - 2; i += 2) {
              const currentX = trajectory[i];
              const currentY = trajectory[i + 1];
              // 原生曲线可能越过目标后贴近窗口边缘；缩放下这些点会被识别为移出内容区。
              // 仅跳过曲线的边缘中间点，终点仍按调用者要求发送。
              if (!Number.isFinite(currentX) || !Number.isFinite(currentY) ||
                  currentX < 8 || currentY < 8 || currentX >= boundingBox.width - 8 || currentY >= boundingBox.height - 8) continue;
              await sendOne('mousemove', currentX, currentY);
              await new Promise(resolve => setTimeout(resolve, 10));
            }
          }
          await sendOne(eventType, x, y);
        }
      } finally {
        watcher.dispose();
      }
    };
