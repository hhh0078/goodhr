// 文件作用：修正原生鼠标回执等待，并在超时时携带浏览器内部刷新和窗口状态，不读取或注入招聘页面。
    const sendEvents = async (types) => {
      const { clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs');
      const startedAt = Date.now();
      let repaintStartedAt = 0;
      let ackStartedAt = 0;
      const diagnostic = inputDiagnostic;
      diagnostic.event_types = types.join(',');
      guard();
      /** windowSnapshot 只读取浏览器内部窗口状态，失败时保留无法读取的标记。 */
      const windowSnapshot = () => {
        return this._goodhrWindowSnapshot();
      };
      /** diagnosticError 保留稳定错误前缀，并携带有界的原生阶段证据供 Worker 结构化上传。 */
      const diagnosticError = (message) => new Error(message + ' GOODHR_MOUSE_DIAGNOSTICS=' + JSON.stringify({
        ...diagnostic, elapsed_ms: Date.now() - diagnostic.input_started_at,
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
        guard();
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
            // 偏好属于浏览器内部原生输入设置；这里不执行、读取或修改招聘页面脚本。
            const preferences = Cc['@mozilla.org/preferences-service;1'].getService(Ci.nsIPrefBranch);
            const minimumMS = Math.max(90, Math.min(1800, preferences.getIntPref('goodhr.mouse.min_ms', 180)));
            const maximumMS = Math.max(minimumMS, Math.min(1800, preferences.getIntPref('goodhr.mouse.max_ms', 360)));
            const durationMS = minimumMS + Math.floor(Math.random() * (maximumMS - minimumMS + 1));
            const movementStartedAt = Date.now();
            const trajectory = ChromeUtils.camouGetMouseTrajectory(lastX, lastY, x, y);
            const points = [];
            for (let i = 2; i < trajectory.length - 2; i += 2) {
              const currentX = trajectory[i];
              const currentY = trajectory[i + 1];
              // 原生曲线可能越过目标后贴近窗口边缘；缩放下这些点会被识别为移出内容区。
              // 仅跳过曲线的边缘中间点，终点仍按调用者要求发送。
              if (!Number.isFinite(currentX) || !Number.isFinite(currentY) ||
                  currentX < 8 || currentY < 8 || currentX >= boundingBox.width - 8 || currentY >= boundingBox.height - 8) continue;
              points.push([currentX, currentY]);
            }
            points.push([x, y]);
            diagnostic.mouse_duration_min_ms = minimumMS;
            diagnostic.mouse_duration_max_ms = maximumMS;
            diagnostic.mouse_duration_planned_ms = durationMS;
            // 使用整段轨迹的截止时刻排程，回执开销不再逐点叠加；迟到时跳过中间点，终点必达。
            for (let i = 0; i < points.length; i++) {
              const remainingMS = movementStartedAt + durationMS * (i + 1) / points.length - Date.now();
              if (remainingMS > 0) await new Promise(resolve => setTimeout(resolve, remainingMS));
              guard();
              while (i < points.length - 1 &&
                     Date.now() - movementStartedAt >= durationMS * (i + 2) / points.length) i++;
              // 临近终点时为最近一次回执预留开销，避免倒数第二点把终点推迟一整次回执。
              if (i < points.length - 1 && Date.now() + Math.max(1, diagnostic.mouse_ack_wait_ms) >= movementStartedAt + durationMS) {
                i = points.length - 1;
                const finalWaitMS = movementStartedAt + durationMS - Date.now();
                if (finalWaitMS > 0) await new Promise(resolve => setTimeout(resolve, finalWaitMS));
                guard();
              }
              await sendOne('mousemove', points[i][0], points[i][1]);
            }
            diagnostic.mouse_duration_actual_ms = Date.now() - movementStartedAt;
            continue;
          }
          await sendOne(eventType, x, y);
        }
      } finally {
        watcher.dispose();
      }
    };
