// 文件作用说明：验证鼠标档位协议、默认值、浏览器复用保护，以及完整原生轨迹的时间上下限。
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { MOUSE_SPEED_DURATIONS, mouseSpeedPreferences, normalizeMouseSpeed } from '../dist/browser/session/mouse-speed.js';
import { parseBrowserStartRequest } from '../dist/validation/action-requests.js';
import { BrowserSession } from '../dist/browser/session/browser-session.js';

const source = readFileSync(new URL('../../internal/runtime/mouse_safety.js', import.meta.url), 'utf8');

/** nativeTiming 使用虚拟时钟和原生回执模拟长曲线，确认逐点开销不会继续累加固定等待。 */
async function nativeTiming(range, random, acknowledgementMS, trajectory) {
  let now = 0;
  let diagnostic;
  const sent = [];
  const target = { _lastTrackedPos: {x:10,y:10}, _pendingEventWatchers:new Set(),
    _pageTarget:{_linkedBrowser:{scrollRectIntoViewIfNeeded(){},getBoundingClientRect:()=>({left:0,top:0,width:200,height:200})}} };
  const context = vm.createContext({ target, x:180, y:180, button:0, clickCount:0, modifiers:0, buttons:0,
    Date:{now:()=>now}, Math:Object.assign(Object.create(Math),{random:()=>random}),
    win:{windowUtils:{flushApzRepaints:()=>false,jugglerSendMouseEvent(type,x,y){sent.push({type,x,y});return sent.length;}}},
    Cc:{'@mozilla.org/observer-service;1':{getService:()=>({addObserver(){},removeObserver(){}})},
      '@mozilla.org/preferences-service;1':{getService:()=>({getIntPref:name=>name.endsWith('min_ms')?range[0]:range[1]})}},
    Ci:{nsIObserverService:{},nsIPrefBranch:{}},
    ChromeUtils:{importESModule:()=>({clearTimeout(){}}),camouGetBool:()=>true,camouGetMouseTrajectory:()=>trajectory},
    EventWatcher:class {ensureEvent(){now+=acknowledgementMS;return Promise.resolve();}dispose(){}},
    setTimeout(fn,ms){if(ms<5000){now+=ms;queueMicrotask(fn);}return 1;},
    capture(value){diagnostic=value;},
  });
  await vm.runInContext(`(async function(){const inputDiagnostic={events_sent:0,events_acknowledged:0,duplicate_points_skipped:0};const guard=()=>{};${source}\n await sendEvents(['mousemove']);capture(inputDiagnostic);}).call(target)`,context);
  return {elapsed:now,sent,diagnostic};
}

test('五档协议完整保留，旧版缺失和未知配置统一使用快速默认档', () => {
  assert.equal(normalizeMouseSpeed(undefined),'fast');
  assert.equal(normalizeMouseSpeed('unknown'),'fast');
  for (const [speed,range] of Object.entries(MOUSE_SPEED_DURATIONS)) {
    assert.equal(parseBrowserStartRequest({mouse_speed:speed},'trace','browser.start').mouse_speed,speed);
    assert.deepEqual(mouseSpeedPreferences(speed),{'goodhr.mouse.min_ms':range[0],'goodhr.mouse.max_ms':range[1]});
    assert.ok(range[1]<=1800);
  }
  for (const value of [null,100,'unknown','']) {
    assert.throws(()=>parseBrowserStartRequest({mouse_speed:value},'trace','browser.start'),{code:'INVALID_REQUEST',trace_id:'trace'});
  }
});

test('复用时保留档位，切换速度明确要求重开且不擅自关闭窗口', async () => {
  const session = new BrowserSession({info(){},error(){},failure(){}});
  session.isRunning=async()=>true;
  session.status=async reused=>({running:true,reused,current_url:''});
  session.mouseSpeed='slow';
  let closes=0;
  session.dispose=async()=>{closes++;};
  const context={action:'browser.start',trace_id:'trace',deadline_at:Date.now()+1000};
  assert.equal((await session.start({},context)).reused,true);
  assert.equal((await session.start({mouse_speed:'slow'},context)).reused,true);
  await assert.rejects(session.start({mouse_speed:'fast'},context),{step:'browser_config_change'});
  assert.equal(closes,0);
});

test('每档完整轨迹覆盖下限与上限，回执只增加最终事件开销', async () => {
  const trajectory=[10,10];
  for(let i=11;i<180;i++)trajectory.push(i,i);
  trajectory.push(180,180);
  for(const range of Object.values(MOUSE_SPEED_DURATIONS)) {
    for(const random of [0,0.999999]) {
      const value=await nativeTiming(range,random,4,trajectory);
      const planned=random===0?range[0]:range[1];
      assert.equal(value.diagnostic.mouse_duration_planned_ms,planned);
      assert.ok(value.elapsed>=planned);
      assert.ok(value.elapsed<=planned+4,JSON.stringify({range,elapsed:value.elapsed,planned}));
      assert.deepEqual(value.sent.at(-1),{type:'mousemove',x:180,y:180});
    }
  }
});

test('极短和被过滤的轨迹仍完成整段时长，慢回执不会逐点拖长轨迹', async () => {
  const empty=await nativeTiming([90,210],0,3,[10,10,180,180]);
  assert.equal(empty.elapsed,93);
  assert.equal(empty.sent.length,1);
  const trajectory=[10,10];
  for(let i=11;i<180;i++)trajectory.push(i,i);
  trajectory.push(180,180);
  const slow=await nativeTiming([90,210],0,20,trajectory);
  assert.ok(slow.elapsed<=110);
  assert.ok(slow.sent.length<10);
});
