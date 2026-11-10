// 文件作用：回归点击阶段诊断和回车兜底日志，防止点击结果不确定时重复发送。
import assert from 'node:assert/strict';
import test from 'node:test';
import { ClickAction } from '../dist/browser/actions/click.js';
import { KeyboardAction } from '../dist/browser/actions/keyboard.js';
import { WorkerError } from '../dist/errors/worker-error.js';
import { parseKeyboardPressRequest } from '../dist/validation/action-requests.js';

for (const stage of ['find', 'move', 'down', 'up']) {
  test(`点击在 ${stage} 失败时保留是否已经开始点击`, async () => {
    const box = { x:20,y:20,width:100,height:40 };
    const page = {};
    const resolved = { page,locator:{ locator: () => ({ count: async () => 1 }) },view:{box} };
    const fail = () => { throw new WorkerError({code:'CLICK_FAILED',message:'测试点击失败',action:'element.click',step:stage,trace_id:'test'}); };
    const action = new ClickAction(
      {one:async () => { if (stage === 'find') fail(); return {resolved,result:{element_ref:'button'}}; }},
      {ensureVisible:async () => {}},
      {toElement:async target => { if (stage === 'move') fail(); return { view: target.view }; }},
      {box:async () => box},
      {down:async () => { if (stage === 'down') fail(); },up:async () => { if (stage === 'up') fail(); }},
      {info(){},failure(){}},
    );
    await assert.rejects(action.execute({selector:{description:'发送按钮'}},{action:'element.click',trace_id:'test',started_at:Date.now()}),error => {
      assert.equal(error.details.click_started,stage === 'down' || stage === 'up'); return true;
    });
  });
}

test('回车兜底的诊断参数经过协议校验，开始和完成都保留中文说明', async () => {
  const request = parseKeyboardPressRequest({key:'Enter',diagnostic:true,description:'发送按钮未能点击，改用回车发送消息'},'test','keyboard.press');
  const logs = [];
  const action = new KeyboardAction({requirePage:async () => ({})},{press:async () => {}},{info:(ctx,step,status,fields) => logs.push({status,...fields})});
  assert.equal((await action.press(request,{action:'keyboard.press',trace_id:'test'})).pressed,true);
  assert.deepEqual(logs.map(item => item.status),['start','success']);
  assert.ok(logs.every(item => item.diagnostic && item.target_description === request.description));
});

for (const failure of ['none', 'page', 'hidden', 'focus', 'draft', 'native']) {
  test(`真实按键前约束页面、可见目标、草稿和焦点并保留派发阶段：${failure}`, async () => {
    const context = { action: 'keyboard.press', trace_id: 'guarded-send' };
    let presses = 0;
    const page = { url: () => failure === 'page' ? 'other' : 'confirmed' };
    const target = { isVisible: async () => failure !== 'hidden',
      inputValue: async () => failure === 'draft' ? 'modified' : 'fixture',
      locator: selector => { assert.equal(selector, ':scope:focus'); return { count: async () => failure === 'focus' ? 0 : 1 }; } };
    const action = new KeyboardAction({ requirePage: async () => page },
      { press: async () => { presses++; if (failure === 'native') throw Error('transport'); } },
      { info() {}, failure() {} }, { resolve: async () => ({ locator: target }) });
    const request = parseKeyboardPressRequest({ key: 'Enter', target: { target: { selectors: [{ type: 'css', value: 'textarea' }] }, description: 'draft' },
      expected_page_url: 'confirmed', expected_value: 'fixture', require_focus: true }, 'guarded', 'keyboard.press');
    const result = action.press(request, context);
    if (failure === 'none') assert.equal((await result).pressed, true);
    else await assert.rejects(result, e => e.details.key_started === (failure === 'native'));
    assert.equal(presses, failure === 'none' || failure === 'native' ? 1 : 0);
  });
}
