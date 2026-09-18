// 文件作用：修正浏览器原生拟人轨迹的重复像素回执等待，仅运行于浏览器内部协议模块。
    const sendEvents = async (types) => {
      const { clearTimeout } = ChromeUtils.importESModule('resource://gre/modules/Timer.sys.mjs');
      this._pageTarget._linkedBrowser.scrollRectIntoViewIfNeeded(x, y, 0, 0);
      const boundingBox = this._pageTarget._linkedBrowser.getBoundingClientRect();
      if (win.windowUtils.flushApzRepaints())
        await helper.awaitTopic('apz-repaints-flushed');
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
            dispatchX === previousX && dispatchY === previousY) return;
        const jugglerEventId = win.windowUtils.jugglerSendMouseEvent(
          eventType, dispatchX, dispatchY,
          button, clickCount, modifiers, false, 0.0, 0, true, false, buttons,
          win.windowUtils.DEFAULT_MOUSE_POINTER_ID, false
        );
        if (eventType === 'mousemove') {
          lastX = dispatchX - boundingBox.left;
          lastY = dispatchY - boundingBox.top;
          this._lastTrackedPos = { x: lastX, y: lastY };
        }
        let timer;
        try {
          await Promise.race([
            watcher.ensureEvent(eventType, eventObject => eventObject.jugglerEventId === jugglerEventId),
            new Promise((_, reject) => {
              timer = setTimeout(() => reject(new Error('GOODHR_MOUSE_ACK_TIMEOUT:' + eventType +
                ' from=' + previousX + ',' + previousY + ' to=' + dispatchX + ',' + dispatchY)), 5000);
            }),
          ]);
        } finally {
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
