// 文件作用说明：验证异步新标签页只登记，显式切换、点击等待及窗口恢复才决定后续操作的页面。
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import test from 'node:test';
import { ActionService } from '../dist/browser/actions/action-service.js';

/** actionContext 为合成会话创建独立、可取消的请求。 */
function actionContext(action = 'page.use', signal) {
  return { action, trace_id: 'page-selection', started_at: Date.now(), ...(signal ? { signal } : {}) };
}

/** fixture 使用标准页面事件替身，保留真实 Session、ActionService、Click 和 Read 组装。 */
function fixture(t, service = new ActionService()) {
  service.logger.writeLine = () => {};
  service.interactionQueue.logger.writeLine = () => {};
  const session = service.session;
  const browserContext = new EventEmitter();
  const pages = [];
  browserContext.pages = () => pages.filter(page => !page.isClosed());
  /** addPage 模拟页面创建；发出事件不等于调用方显式选择。 */
  function addPage(url, emit = true) {
    const page = new EventEmitter();
    page.address = url;
    page.closed = false;
    page.fronts = 0;
    page.url = () => page.address;
    page.title = async () => '合成页面';
    page.context = () => browserContext;
    page.isClosed = () => page.closed;
    page.mainFrame = () => page;
    page.bringToFront = async () => { page.fronts++; await page.onFront?.(); };
    page.goto = async url => { await page.onGoto?.(); page.address = url; };
    page.waitForLoadState = async () => { await page.onLoad?.(); };
    page.close = async () => { page.closed = true; page.emit('close'); };
    pages.push(page);
    if (emit) browserContext.emit('page', page);
    return page;
  }
  const original = addPage('https://example.test/list', false);
  browserContext.newPage = async () => addPage('about:blank');
  session.context = browserContext;
  session.currentPage = original;
  session.registerContext(browserContext);
  const box = { x: 10, y: 10, width: 80, height: 30 };
  service.locator.resolve = async (page, spec) => ({ page, locator: { innerText: async () => page.url(), locator: () => ({ count: async () => 1 }) },
    view: { box: { ...box }, viewport: { width: 800, height: 600 }, visible: true, in_viewport: true },
    matched_selector: spec.target.selectors[0], attempts: [] });
  service.locator.box = async () => ({ ...box });
  service.scrollAction.ensureVisible = async () => undefined;
  service.moveAction.toElement = async target => ({ x: 50, y: 20, view: target.view });
  service.mouse.down = async () => {};
  service.mouse.up = async () => {};
  t.after(() => session.elements.clear());
  return { service, session, browserContext, original, addPage };
}

const selector = { description: '合成目标', timeout_ms: 0, target: { selectors: [{ type: 'css', value: '.target' }] } };

test('Use 原页后晚到 PDF、关闭预览后的新 PDF 都不抢走后续 Read', async t => {
  const f = fixture(t);
  const preview = f.addPage('https://example.test/preview');
  await f.session.usePage('1', actionContext());
  await f.session.usePage('0', actionContext());
  f.addPage('file:///tmp/synthetic-resume.pdf');
  await preview.close();
  f.addPage('file:///tmp/synthetic-resume-2.pdf');
  assert.equal((await f.service.readElement({ selector }, actionContext('element.read'))).value, f.original.url());
  const list = await f.session.listPages();
  assert.deepEqual(list.pages.filter(page => page.current).map(page => page.url), [f.original.url()]);
  assert.equal(list.count, 3);
});

test('Click wait_for_new_page 显式提交新页，返回 URL 和后续 Read 仍一致', async t => {
  const f = fixture(t);
  let detail;
  f.service.mouse.down = async () => {
    detail = f.addPage('https://example.test/detail');
    detail.onFront = async () => { f.addPage('file:///tmp/late-download.pdf'); };
  };
  const result = await f.service.clickElement({ selector, wait_for_new_page: true }, actionContext('element.click'));
  assert.equal(result.new_page_opened, true);
  assert.equal(result.new_page_url, detail.url());
  assert.equal(f.session.currentPage, detail);
  assert.equal((await f.service.readElement({ selector }, actionContext('element.read'))).value, detail.url());
});

test('普通 Click 产生新标签仍保留来源页，不凭新页事件改变读取归属', async t => {
  const f = fixture(t);
  f.service.mouse.down = async () => { f.addPage('file:///tmp/download.pdf'); };
  const result = await f.service.clickElement({ selector }, actionContext('element.click'));
  assert.equal(result.new_page_opened, false);
  assert.equal(result.new_page_url, '');
  assert.equal(f.session.currentPage, f.original);
});

for (const state of ['cancelled', 'closed']) {
  test(`Click 新页加载期间 ${state} 不提交新操作页`, async t => {
    const f = fixture(t);
    const cancel = new AbortController();
    let detail;
    f.service.mouse.down = async () => {
      detail = f.addPage('https://example.test/detail');
      detail.onLoad = async () => { if (state === 'cancelled') cancel.abort(); else await detail.close(); };
    };
    await assert.rejects(f.service.clickAction.execute({ selector, wait_for_new_page: true }, actionContext('element.click', cancel.signal)),
      { code: state === 'cancelled' ? 'ACTION_CANCELLED' : 'PAGE_NOT_AVAILABLE' });
    assert.equal(f.session.currentPage, f.original);
    assert.equal(detail.fronts, 0);
  });
}

for (const failure of ['cancelled', 'closed', 'foreign_context', 'activation_failed']) {
  test(`Use 激活期间 ${failure} 保留原有效操作页`, async t => {
    const f = fixture(t);
    const target = f.addPage('https://example.test/target');
    const cancel = new AbortController();
    if (failure === 'foreign_context') target.context = () => new EventEmitter();
    target.onFront = async () => {
      if (failure === 'cancelled') cancel.abort();
      if (failure === 'closed') await target.close();
      if (failure === 'activation_failed') throw new Error('synthetic activation failure');
    };
    await assert.rejects(f.session.usePage('1', actionContext('page.use', cancel.signal)));
    assert.equal(f.session.currentPage, f.original);
  });
}

test('Open 新页或复用页都在激活完成后选择，失败和取消不提交', async t => {
  const f = fixture(t);
  const reused = f.addPage('https://example.test/reused');
  reused.onFront = async () => { assert.equal(f.session.currentPage, f.original); f.addPage('file:///tmp/late.pdf'); };
  assert.equal((await f.session.open({ url: reused.url() }, actionContext('page.open'))).current, true);
  const opened = await f.session.open({ url: 'https://example.test/new', new_tab: true }, actionContext('page.open'));
  assert.equal(opened.current, true);
  assert.equal(f.session.currentPage.url(), opened.url);
  const selected = f.session.currentPage;
  const cancel = new AbortController();
  f.browserContext.newPage = async () => {
    const page = f.addPage('about:blank');
    page.onGoto = async () => { assert.equal(f.session.currentPage, selected); cancel.abort(); };
    return page;
  };
  await assert.rejects(f.session.open({ url: 'https://example.test/cancelled', new_tab: true }, actionContext('page.open', cancel.signal)), { code: 'ACTION_CANCELLED' });
  assert.equal(f.session.currentPage, selected);
});

test('窗口恢复创建临时页、期间下载新页后仍回到同一原页', async t => {
  const f = fixture(t);
  f.session.focusBrowserWindow = async () => ({ status: 'not_found' });
  const observed = [];
  f.browserContext.newPage = async () => {
    const temporary = f.addPage('about:blank');
    observed.push(f.session.currentPage);
    temporary.onFront = async () => { f.addPage('file:///tmp/late-window.pdf'); observed.push(f.session.currentPage); };
    return temporary;
  };
  await f.session.reactivatePage(f.original, actionContext('element.scroll'));
  assert.deepEqual(observed, [f.original, f.original]);
  assert.equal(f.session.currentPage, f.original);
  assert.equal((await f.session.listPages()).pages.some(page => page.url === 'about:blank'), false);
});

test('旧会话新页事件不注册为新会话当前页，显式 Close 与失效回退保持兼容', async t => {
  const f = fixture(t);
  const second = f.addPage('https://example.test/second');
  await f.session.usePage('1', actionContext());
  await f.session.closeCurrentPage(actionContext('page.close'));
  assert.equal(f.session.currentPage, f.original);
  assert.equal(second.isClosed(), true);
  const fallback = f.addPage('https://example.test/fallback');
  await f.original.close();
  assert.equal(await f.session.requirePage(actionContext(), 'find'), fallback);
  const newContext = new EventEmitter();
  newContext.pages = () => [fallback];
  f.session.context = newContext;
  f.session.registerContext(newContext);
  const late = f.addPage('file:///tmp/old-session.pdf');
  assert.equal(f.session.currentPage, fallback);
  assert.equal(f.session.registeredPages.has(late), false);
});

/** 验证只关闭原生回执的精确新文件页，并保留人工查看的无关标签以及同名文件。 */
test('按本次下载回执收起文件页，只剩原页才显式激活', async t => {
  for (const others of [false, true]) {
    const f = fixture(t);
    const url = 'file:///tmp/resume-2.pdf';
    const viewer = f.addPage(url);
    const manual = others ? f.addPage('file:///other/resume-2.pdf') : null;
    const item = { id: 'native_download_24', status: 'saved', page_url: f.original.url(), file_path: '/tmp/resume-2.pdf' };
    f.session.downloadManager.list = async () => ({ downloads: [item] });
    const restore = { original_url: f.original.url(), viewer_url: url, download_id: item.id };
    const result = await f.session.usePage('0', actionContext(), restore);
    assert.equal(result.download_viewer_closed, true);
    assert.equal(viewer.closed, true);
    assert.equal(f.original.fronts, others ? 0 : 1);
    assert.equal(manual?.closed ?? false, false);
    const reopened = f.addPage(url);
    await assert.rejects(f.session.usePage('0', actionContext(), restore));
    assert.equal(reopened.closed, false);
  }
});

/** 验证同路径多页、下载路径或原页不符、人工明确换页都不能复用关闭动作。 */
test('下载文件页面或原生回执存在歧义时，保留所有页面', async t => {
  for (const scenario of ['duplicate','wrong_file','wrong_origin','other_page','wrong_receipt']) {
    const f = fixture(t);
    const viewer = f.addPage('file:///tmp/sample.pdf');
    if (scenario==='duplicate') f.addPage(viewer.url());
    if (scenario==='other_page') {
      f.addPage('https://manual.test');
      await f.session.usePage('2', actionContext());
    }
    const item = {id:'native_download_24', status:'saved', page_url: f.original.url(), file_path: scenario==='wrong_file' ? '/other/sample.pdf' : '/tmp/sample.pdf'};
    f.session.downloadManager.list = async () => ({downloads:[item]});
    await assert.rejects(f.session.usePage('0',actionContext(),{
      original_url: scenario==='wrong_origin' ? 'https://other.test' : f.original.url(),
      viewer_url: viewer.url(), download_id: scenario==='wrong_receipt' ? 'unknown' : item.id,
    }));
    assert.equal(viewer.closed,false);
    assert.equal(f.original.fronts,0);
  }
});

/** 验证本次回执复用旧文件名时，只有原页和同一文件两页才切回；已有查看器一直保留。 */
test('复用已有文件标签时保留原页和文件，出现第三页不抢焦点',async t => {
  for (const third of [false,true]) {
    const f=fixture(t);
    const viewer=f.addPage('file:///tmp/resume-5.pdf');
    if(third) f.addPage('https://example.test/manual');
    const item={id:'native_download_repeat_path',status:'saved',page_url:f.original.url(),file_path:'/tmp/resume-5.pdf'};
    f.session.downloadManager.list=async()=>({downloads:[item]});
    const req={original_url:f.original.url(),download_id:item.id,viewer_url:viewer.url(),keep_viewer:true};
    if(third) await assert.rejects(f.session.usePage('0',actionContext(),req));
    else assert.equal((await f.session.usePage('0',actionContext(),req)).download_viewer_preserved,true);
    assert.equal(viewer.closed,false);
    assert.equal(f.original.fronts, third ? 0 : 1);
  }
});

/** 验证真实点击前开始监听，回执可能早于新标签创建、早于空白页变成 PDF，均只关闭实际命中的文件页。 */
test('本次 PDF 文件查看页在下载回执之后才创建或完成导航，仍按标准事件收起',async t=>{
  for (const scenario of ['already-ready','late-page','blank-then-file','existing','unrelated']) {
    const f=fixture(t);
    const uri='file:///tmp/verified%20attachment.pdf';
    let viewer = scenario==='existing' ? f.addPage(uri) : undefined;
    await f.session.watchDownloadViewer(actionContext());
    const item={id:'native_download_event', status:'saved', created_at:new Date().toISOString(), page_url:f.original.url(), file_path:'/tmp/verified attachment.pdf'};
    f.session.downloadManager.list=async()=>({downloads:[item]});
    if(scenario==='blank-then-file') viewer=f.addPage('about:blank');
    if(scenario==='already-ready') viewer=f.addPage(uri);
    let unrelated;
    if(scenario==='unrelated') unrelated=f.addPage('file:///other/verified%20attachment.pdf');
    const restore={original_url:f.original.url(),download_id:item.id,viewer_url:'',await_viewer:true};
    const result=f.session.usePage('0',actionContext(),restore);
    if(scenario==='blank-then-file' || scenario==='late-page' || scenario==='unrelated') {
      await new Promise(r=>setTimeout(r,25));
      if(viewer) {viewer.address=uri;viewer.emit('framenavigated',viewer);}
      else viewer=f.addPage(uri);
    }
    const done=await result;
    assert.equal(Boolean(done.download_viewer_closed),scenario!=='existing');
    assert.equal(Boolean(done.download_viewer_preserved),scenario==='existing');
    assert.equal(viewer.closed,scenario!=='existing');
    assert.equal(unrelated?.closed??false,false);
  }
});

/** 验证观察期限和取消能及时注销请求，迟到的人工标签不会触发后台关闭。 */
test('等候下载页超时和取消后不再执行标签动作',async t=>{
  const { DownloadViewerWatch } = await import('../dist/browser/session/download-viewer-watch.js');
  const f=fixture(t);
  const watch=new DownloadViewerWatch(f.original,f.browserContext);
  try {
    assert.deepEqual(await watch.wait('/tmp/late.pdf',undefined,20),[]);
    const abort=new AbortController();
    const pending=watch.wait('/tmp/late.pdf',abort.signal,1000);
    abort.abort();
    await assert.rejects(pending);
    const late=f.addPage('file:///tmp/late.pdf'); assert.equal(late.closed,false);
  } finally {watch.dispose();}
});
