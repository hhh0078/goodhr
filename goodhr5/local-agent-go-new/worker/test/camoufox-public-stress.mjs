// 文件作用：用隔离浏览器在允许的公开文档站循环验证真实鼠标、滚轮、链接和标签；不连接招聘账号或执行页面脚本。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { execFileSync, spawn } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { captureOwnedProcesses, terminateOwnedProcesses, processCleanupSelfTest } from './camoufox-owned-processes.mjs';

const SCRIPT = fileURLToPath(import.meta.url);
const SITES = {
  playwright: 'https://playwright.dev/docs/intro',
  mdn: 'https://developer.mozilla.org/en-US/docs/Web/API/MouseEvent',
  selenium: 'https://www.selenium.dev/documentation/webdriver/',
  heroku: 'https://the-internet.herokuapp.com/large',
  wikipedia: 'https://en.wikipedia.org/wiki/Computer_mouse',
};
/** sleep 仅在 Node 侧放慢本地输入节奏，不增加网站请求。 */
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
let childAbort;
let runDirectory;
let metadata;

/** argumentsForRun 校验命令行；网址只能从固定公开名单选择，路径必须明确指定。 */
function argumentsForRun(argv) {
  const { values } = parseArgs({ args: argv, options: {
    'install-dir': { type: 'string' }, binary: { type: 'string' }, 'output-dir': { type: 'string' },
    label: { type: 'string', default: 'camoufox' }, engine: { type: 'string', default: 'official' },
    sessions: { type: 'string', default: '3' }, rounds: { type: 'string', default: '30' },
    site: { type: 'string', multiple: true }, url: { type: 'string', multiple: true },
    'timeout-ms': { type: 'string', default: '18000' }, humanize: { type: 'string', default: 'true' },
    'launch-timeout-ms': { type: 'string', default: '60000' },
    headless: { type: 'boolean', default: false }, headed: { type: 'boolean', default: false },
    'window-state': { type: 'string', default: 'unchanged' },
    'hide-before-wheel-round': { type: 'string', default: '0' },
    'cover-before-wheel-round': { type: 'string', default: '0' }, 'cover-binary': { type: 'string' },
    'cover-seconds': { type: 'string', default: '30' },
    'child-session': { type: 'string', default: '0' }, 'run-dir': { type: 'string' }, 'run-id': { type: 'string' },
    'self-test': { type: 'boolean', default: false }, help: { type: 'boolean', default: false },
  } });
  if (values.help || values['self-test']) return values;
  assert.equal(values.engine, 'official', '当前仅支持官方 SDK 路径；正式 Worker 路径需单独集成验收');
  const options = { ...values, timeout: Number(values['timeout-ms']), launchTimeout: Number(values['launch-timeout-ms']),
    sessions: Number(values.sessions), rounds: Number(values.rounds),
    childSession: Number(values['child-session']), headless: values.headless && !values.headed };
  for (const [name, value, maximum] of [['sessions', options.sessions, 20], ['rounds', options.rounds, 300],
    ['timeout-ms', options.timeout, 180000], ['launch-timeout-ms', options.launchTimeout, 300000]]) {
    assert.ok(Number.isInteger(value) && value > 0 && value <= maximum, `${name} 必须是 1 到 ${maximum} 的整数`);
  }
  assert.ok(['true', 'false'].includes(values.humanize), 'humanize 只能为 true 或 false');
  assert.ok(['visible', 'unchanged'].includes(values['window-state']), 'window-state 只能为 visible 或 unchanged');
  options.humanize = values.humanize === 'true';
  options.hideBeforeWheelRound = Number(values['hide-before-wheel-round']);
  assert.ok(Number.isInteger(options.hideBeforeWheelRound) && options.hideBeforeWheelRound >= 0 && options.hideBeforeWheelRound <= options.rounds,
    'hide-before-wheel-round 必须为 0 或不超过 rounds 的正整数');
  if (options.hideBeforeWheelRound > 0) assert.ok(process.platform === 'darwin' && !options.headless, '隐藏对照仅支持 Mac 有窗口模式');
  options.coverBeforeWheelRound = Number(values['cover-before-wheel-round']);
  options.coverSeconds = Number(values['cover-seconds']);
  assert.ok(Number.isInteger(options.coverBeforeWheelRound) && options.coverBeforeWheelRound >= 0 && options.coverBeforeWheelRound <= options.rounds,
    'cover-before-wheel-round 必须为 0 或不超过 rounds 的正整数');
  assert.ok(Number.isInteger(options.coverSeconds) && options.coverSeconds > 0 && options.coverSeconds <= 300, 'cover-seconds 必须为 1 到 300 的整数');
  if (options.coverBeforeWheelRound > 0) {
    assert.ok(['darwin', 'win32'].includes(process.platform) && !options.headless, '遮挡对照仅支持 Mac 或 Windows 有窗口模式');
    assert.equal(options.hideBeforeWheelRound, 0, '遮挡和隐藏对照必须分开运行');
    assert.ok(values['cover-binary'] && path.isAbsolute(values['cover-binary']), '遮挡对照必须指定 cover-binary 绝对路径');
    fs.accessSync(values['cover-binary'], process.platform === 'win32' ? fs.constants.R_OK : fs.constants.X_OK);
  }
  options.sites = [...new Set([...(values.site ?? []), ...(values.url ?? []).map(url => siteForURL(url))])];
  if (!options.sites.length) options.sites = ['playwright', 'mdn', 'selenium'];
  assert.ok(options.sites.every(site => Object.hasOwn(SITES, site)), 'site 只能是 playwright、mdn、selenium、heroku、wikipedia');
  options.installDir = values['install-dir'] ?? process.env.CAMOUFOX_INSTALL_DIR;
  options.binary = values.binary ?? process.env.CAMOUFOX_BINARY_PATH;
  options.outputDir = values['output-dir'];
  for (const [name, value] of [['install-dir', options.installDir], ['binary', options.binary], ['output-dir', options.outputDir]]) {
    assert.ok(value && path.isAbsolute(value), `${name} 必须明确指定绝对路径`);
  }
  const installRoot = fs.realpathSync(options.installDir);
  assertIsolatedInstallation(installRoot);
  const relative = path.relative(installRoot, fs.realpathSync(options.binary));
  assert.ok(relative && !relative.startsWith('..') && !path.isAbsolute(relative), '浏览器启动文件必须在明确的隔离安装目录内');
  return options;
}

/** assertIsolatedInstallation 只接受 camoufox-upgrade 内的副本；Mac 和 Windows 均拒绝用户正式运行目录。 */
function assertIsolatedInstallation(directory) {
  const normalized = directory.replaceAll('\\', '/').toLowerCase();
  assert.ok(normalized.split('/').includes('camoufox-upgrade'), '安装目录必须位于 camoufox-upgrade 隔离目录内');
  for (const forbidden of ['/library/application support/goodhr/', '/appdata/roaming/goodhr/', '/appdata/local/goodhr/']) {
    assert.ok(!(normalized + '/').includes(forbidden), '禁止使用用户正在运行的 GoodHR 浏览器目录');
  }
}

/** siteForURL 仅接受名单中同一文档的 HTTPS 地址，拒绝其他站点、凭据和查询参数。 */
function siteForURL(value) {
  const url = new URL(value);
  assert.ok(!url.username && !url.password && !url.search && url.protocol === 'https:', '仅允许无凭据和查询参数的公开 HTTPS 文档');
  const site = Object.keys(SITES).find(key => {
    const allowed = new URL(SITES[key]);
    return url.origin === allowed.origin && url.pathname === allowed.pathname;
  });
  assert.ok(site, `网址不在允许的公开文档名单内：${url.origin}${url.pathname}`);
  return site;
}

/** record 增量写入 JSONL，并通过 IPC 告知监护进程当前动作和超时预算。 */
function record(event) {
  const item = { timestamp: new Date().toISOString(), ...metadata, ...event };
  fs.appendFileSync(path.join(runDirectory, 'events.jsonl'), JSON.stringify(item) + '\n');
  if (process.send) process.send(item);
}

/** checked 在 Node 侧限制单次操作；只使用标准浏览器能力，超时后不复用该测试会话。 */
async function checked(step, operation, detail = {}, timeout = metadata.timeout_ms) {
  const started = performance.now();
  record({ event: 'action_start', step, timeout_ms: timeout, ...detail });
  let timer;
  let aborted;
  try {
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => reject(Object.assign(new Error(`${step} 超过 ${timeout} 毫秒`), { code: 'DEMO_ACTION_TIMEOUT' })), timeout);
      aborted = () => reject(Object.assign(new Error('本次独立测试已收到停止信号'), { code: 'DEMO_ABORTED' }));
      childAbort?.signal.addEventListener('abort', aborted, { once: true });
      if (childAbort?.signal.aborted) aborted();
    });
    const result = await Promise.race([Promise.resolve().then(operation), deadline]);
    record({ event: 'action_end', step, status: 'passed', duration_ms: Math.round(performance.now() - started), ...detail });
    return result;
  } catch (error) {
    record({ event: 'action_end', step, status: 'failed', duration_ms: Math.round(performance.now() - started),
      error_code: error.code ?? error.name, error_message: error.message,
      ...(error.stdout ? { helper_stdout: String(error.stdout) } : {}),
      ...(error.stderr ? { helper_stderr: String(error.stderr) } : {}), ...detail });
    throw error;
  } finally {
    clearTimeout(timer);
    childAbort?.signal.removeEventListener('abort', aborted);
  }
}

/** screenshot 保存当前可见窗口；不使用整页截图或修改页面尺寸。 */
async function screenshot(page, name, detail) {
  const filename = path.join(runDirectory, 'screenshots', `${metadata.session}-${name}.png`);
  await checked('screenshot', () => page.screenshot({ path: filename, timeout: metadata.timeout_ms, scale: 'css' }), detail);
  return filename;
}

/** setTestWindowState 复用按会话 PID 限定的 AppKit 辅助，记录并确认本次测试窗口的真实状态。 */
async function setTestWindowState(state, detail = {}, cover = {}) {
  const helper = fileURLToPath(new URL('./camoufox-window-state.mjs', import.meta.url));
  const args = [helper, '--session-pid', String(process.pid), '--state', state];
  if (state === 'covered') args.push('--cover-binary', cover.binary, '--cover-seconds', String(cover.seconds));
  const step = state === 'visible' ? 'activate_test_window' : state === 'covered' ? 'cover_test_window' : 'hide_test_window';
  const budget = state === 'covered' && process.platform === 'win32' ? 30000 : 12000;
  const result = await checked(step, () => JSON.parse(execFileSync(process.execPath,
    args, { encoding: 'utf8', timeout: budget, maxBuffer: 1024 * 1024 }).trim()), detail, Math.max(metadata.timeout_ms, budget));
  record({ event: 'window_state', requested: state, result, ...detail });
  const verified = state === 'covered' ? result.ready && result.covered && result.cover_pid > 0 : state === 'hidden' ? result.hidden : !result.hidden && result.active;
  assert.ok(result.ok && verified, `测试浏览器没有确认处于 ${state} 状态`);
}

/** anchorBox 读取标题的标准 Locator 边界，用坐标变化证明滚轮确实移动了正文。 */
async function anchorBox(page, detail) {
  const box = await checked('read_heading_box', () => page.locator('h1, h3').first().boundingBox({ timeout: metadata.timeout_ms }), detail);
  assert.ok(box && Number.isFinite(box.y), '公开文档标题不存在或无法读取边界');
  return box;
}

/** clickSafeAnchor 只点击当前文档内可见的锚点，避免重复请求文档或跳到名单外。 */
async function clickSafeAnchor(page, site) {
  const frame = await checked('anchor_viewport_screenshot', () => page.screenshot({ timeout: metadata.timeout_ms, scale: 'css' }), { site });
  const viewport = page.viewportSize() ?? { width: frame.readUInt32BE(16), height: frame.readUInt32BE(20) };
  const links = page.locator('a[href^="#"]');
  const count = Math.min(await checked('count_anchor_links', () => links.count(), { site }), 100);
  for (let index = 0; index < count; index++) {
    const link = links.nth(index);
    const candidate = await checked('read_anchor_link', async () => ({ href: await link.getAttribute('href'),
      target: await link.getAttribute('target'), visible: await link.isVisible(), box: await link.boundingBox() }), { site, index });
    if (!candidate.href || candidate.href === '#' || /skip|docusaurus_skip/i.test(candidate.href) ||
        candidate.target === '_blank' || !candidate.visible || !candidate.box || candidate.box.x < 0 ||
        candidate.box.x + candidate.box.width > viewport.width || candidate.box.y < 0 ||
        candidate.box.y + candidate.box.height > viewport.height || candidate.box.width < 1 || candidate.box.height < 1) continue;
    assert.equal(siteForURL(new URL(candidate.href, page.url()).href), site);
    try { await link.click({ trial: true, timeout: 1000 }); }
    catch { record({ event: 'safe_link_candidate_skipped', site, index, href: candidate.href, reason: '标准点击试探未通过' }); continue; }
    await checked('click_document_anchor', () => link.click({ timeout: metadata.timeout_ms }), { site, href: candidate.href });
    assert.equal(siteForURL(page.url()), site);
    record({ event: 'safe_link_result', site, status: 'passed', href: candidate.href });
    return;
  }
  record({ event: 'safe_link_result', site, status: 'unavailable', reason: '当前公开页面没有可见的文档内锚点，未跳转其他页面' });
}

/** coveredWheelObservation 仅允许显式遮挡已触发的站点继续采样；普通场景和遮挡前仍严格验收。 */
function coveredWheelObservation(options, site, round) {
  return options.coverBeforeWheelRound > 0 && site === options.sites[0] && round >= options.coverBeforeWheelRound;
}

/** scrollAndVerify 返回实际滚动结果；普通模式无移动立即失败，显式遮挡实验保留失败并继续等待真实输入故障。 */
async function scrollAndVerify(page, delta, site, round, observeCover = false) {
  const detail = { site, round, delta_y: delta, cover_observation: observeCover };
  const before = await anchorBox(page, detail);
  await checked('mouse_wheel', () => page.mouse.wheel(0, delta), detail);
  let after = before;
  for (let attempt = 0; attempt < 12; attempt++) {
    await sleep(100);
    after = await anchorBox(page, detail);
    if ((after.y - before.y) * Math.sign(delta) < -2) break;
  }
  const moved = (after.y - before.y) * Math.sign(delta) < -2;
  record({ event: 'scroll_verification', ...detail, status: moved ? 'passed' : 'failed', before_y: before.y, after_y: after.y,
    ...(moved ? {} : { error_code: 'SCROLL_NOT_OBSERVED', error_message: '滚轮调用已返回，正文坐标没有按预期移动；此记录不代表输入超时' }) });
  if (!observeCover) assert.ok(moved, `真实滚轮已返回，但 ${site} 正文未按预期移动；delta=${delta} before=${before.y} after=${after.y}`);
  return moved;
}

/** testSite 同一会话只加载一次公开文档，循环真实输入并定期切换到空白标签再回来。 */
async function testSite(context, page, spare, site, options) {
  record({ event: 'site_start', site, rounds: options.rounds });
  const response = await checked('navigate', () => page.goto(SITES[site], { waitUntil: 'domcontentloaded', timeout: options.timeout }), { site });
  assert.ok(response && response.status() < 400, `公开文档访问未成功：${response?.status()}`);
  assert.equal(siteForURL(page.url()), site);
  await checked('wait_heading', () => page.locator('h1, h3').first().waitFor({ state: 'visible', timeout: options.timeout }), { site });
  await clickSafeAnchor(page, site);
  await sleep(300);
  await screenshot(page, `${site}-before`, { site });
  let passedRounds = 0;
  for (let round = 1; round <= options.rounds; round++) {
    if (childAbort.signal.aborted) throw new Error('独立测试已停止');
    const detail = { site, round };
    await checked('mouse_move', () => page.mouse.move(530 + (round % 4) * 27, 350 + (round % 3) * 19), detail);
    if (site === options.sites[0] && round === options.hideBeforeWheelRound) {
      await setTestWindowState('hidden', { ...detail, reason: 'controlled_hide_before_first_wheel' });
    }
    if (site === options.sites[0] && round === options.coverBeforeWheelRound) {
      await setTestWindowState('covered', { ...detail, reason: 'controlled_cover_before_first_wheel' },
        { binary: options['cover-binary'], seconds: options.coverSeconds });
    }
    const observeCover = coveredWheelObservation(options, site, round);
    const downMoved = await scrollAndVerify(page, 240 + (round % 3) * 30, site, round, observeCover);
    if (round === 1 || round % 10 === 0) await screenshot(page, `${site}-round-${round}-down`, detail);
    const upMoved = await scrollAndVerify(page, -(240 + (round % 3) * 30), site, round, observeCover);
    if (round % 5 === 0) {
      await checked('switch_blank_tab', () => spare.bringToFront(), detail);
      await sleep(150);
      await checked('reuse_document_tab', () => page.bringToFront(), detail);
      assert.equal(context.pages().length, 2, '只允许本会话的文档与空白验证标签');
    }
    const verified = downMoved && upMoved;
    if (verified) passedRounds++;
    record({ event: 'round_end', ...detail, status: verified ? 'passed' : 'failed', down_moved: downMoved, up_moved: upMoved });
    await sleep(200);
  }
  await screenshot(page, `${site}-after`, { site });
  record({ event: 'site_end', site, status: passedRounds === options.rounds ? 'passed' : 'failed', completed_rounds: passedRounds, attempted_rounds: options.rounds });
  return passedRounds === options.rounds;
}

/** childSession 在独立 Profile 启动官方引擎；只关闭本脚本创建的上下文。 */
async function childSession(options) {
  runDirectory = options['run-dir'];
  metadata = { run_id: options['run-id'], label: options.label, engine: 'official', session: options.childSession,
    platform: process.platform, arch: process.arch, timeout_ms: options.timeout, launch_timeout_ms: options.launchTimeout, pid: process.pid };
  childAbort = new AbortController();
  process.on('SIGTERM', () => childAbort.abort());
  process.on('SIGINT', () => childAbort.abort());
  const profile = path.join(runDirectory, 'profiles', `session-${options.childSession}`);
  await fsp.mkdir(profile, { recursive: false });
  record({ event: 'session_start', fresh_started_at: new Date().toISOString(), profile, humanize: options.humanize, headless: options.headless });
  process.env.CAMOUFOX_INSTALL_DIR = options.installDir;
  process.env.CAMOUFOX_BINARY_PATH = options.binary;
  let context;
  let failed = false;
  try {
    const { launchOptions } = await import('camoufox-js');
    const { firefox } = await import('playwright-core');
    const prepared = await checked('prepare_official_options', () => launchOptions({ headless: options.headless, humanize: options.humanize,
      addons: [], exclude_addons: ['UBO'], locale: 'en-US', window: [1280, 900], args: ['-purgecaches'] }));
    context = await checked('launch_fresh_browser', () => firefox.launchPersistentContext(profile, {
      ...prepared, executablePath: options.binary, viewport: null, timeout: options.launchTimeout,
    }), {}, options.launchTimeout);
    record({ event: 'browser_started', browser_version: context.browser()?.version(), fresh: true, profile });
    if (process.platform === 'darwin' && !options.headless && options['window-state'] === 'visible') {
      await setTestWindowState('visible');
    } else {
      record({ event: 'window_state', requested: options['window-state'], applied: false,
        reason: process.platform !== 'darwin' ? '当前系统不使用 Mac AppKit' : options.headless ? '无界面测试' : '按参数保持窗口现状' });
    }
    const page = context.pages()[0] ?? await checked('new_document_tab', () => context.newPage(), {}, options.launchTimeout);
    const spare = await checked('new_blank_tab', () => context.newPage(), {}, options.launchTimeout);
    await checked('select_document_tab', () => page.bringToFront());
    page.on('crash', () => record({ event: 'page_crash' }));
    context.browser()?.on('disconnected', () => record({ event: 'browser_disconnected' }));
    for (const site of options.sites) if (!(await testSite(context, page, spare, site, options))) failed = true;
  } catch (error) {
    failed = true;
    record({ event: 'session_error', error_code: error.code ?? error.name, error_message: error.message, stack: error.stack });
    const page = context?.pages().find(item => !item.isClosed());
    if (page && !childAbort.signal.aborted) await screenshot(page, 'failure', {}).catch(() => {});
  } finally {
    if (context) {
      let timer;
      try {
        await Promise.race([context.close(), new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('测试浏览器关闭超时')), 5000); })]);
        record({ event: 'browser_closed', status: 'passed' });
      } catch (error) {
        failed = true;
        record({ event: 'browser_closed', status: 'failed', error_message: error.message });
      } finally { clearTimeout(timer); }
    }
    record({ event: 'session_end', status: failed || childAbort.signal.aborted ? 'failed' : 'passed' });
    process.exitCode = failed || childAbort.signal.aborted ? 1 : 0;
  }
}

/** monitorSession 监护独立测试进程，底层调用卡死时也不会遗留测试浏览器或操作用户窗口。 */
async function monitorSession(options, session, signal) {
  const child = spawn(process.execPath, [SCRIPT, ...process.argv.slice(2), '--child-session', String(session), '--run-dir', runDirectory, '--run-id', metadata.run_id], {
    detached: process.platform !== 'win32', stdio: ['ignore', 'inherit', 'inherit', 'ipc'],
  });
  const captured = new Map();
  captureOwnedProcesses(child, captured);
  record({ event: 'child_spawned', session, child_pid: child.pid, fresh_started_at: new Date().toISOString() });
  return new Promise(resolve => {
    let watchdog;
    let forceTimer;
    let stopped = false;
    /** terminate 先请求本次测试退出，仍未结束时强制回收本次进程树。 */
    const terminate = reason => {
      if (stopped) return;
      stopped = true;
      captureOwnedProcesses(child, captured);
      record({ event: 'supervisor_stop', session, child_pid: child.pid, reason });
      const cleanup = terminateOwnedProcesses(child, captured);
      if (cleanup?.status === 'failed') { record({ event: 'process_cleanup', session, ...cleanup }); child.kill('SIGTERM'); }
      forceTimer = setTimeout(() => { captureOwnedProcesses(child, captured); terminateOwnedProcesses(child, captured, true); }, 6000);
    };
    /** arm 按当前动作预算设置外部兜底，留出失败截图及关闭窗口的时间。 */
    const arm = duration => { clearTimeout(watchdog); watchdog = setTimeout(() => terminate('child_action_watchdog'), duration); };
    const stop = () => terminate('user_signal');
    signal.addEventListener('abort', stop, { once: true });
    child.on('message', event => {
      if (event.event === 'browser_started') captureOwnedProcesses(child, captured);
      if (event.event === 'window_state' && event.requested === 'covered') captureOwnedProcesses(child, captured,
        { pid: event.result?.cover_pid, browserPID: event.result?.browser_pid, binary: options['cover-binary'],
          sessionPID: event.result?.session_pid, helperPID: event.result?.fixture_parent_pid, executable: event.result?.fixture_executable });
      if (event.event === 'action_start') arm(event.timeout_ms + 8000);
      else if (event.event === 'action_end') arm(30000);
    });
    child.once('error', error => { record({ event: 'child_spawn_error', session, error_message: error.message }); });
    child.once('close', (code, childSignal) => {
      clearTimeout(watchdog); clearTimeout(forceTimer); signal.removeEventListener('abort', stop);
      const cleanup = terminateOwnedProcesses(child, captured, true);
      if (cleanup) record({ event: 'process_cleanup', session, child_pid: child.pid, ...cleanup });
      record({ event: 'child_exit', session, child_pid: child.pid, exit_code: code, signal: childSignal });
      resolve(code === 0 && !stopped);
    });
    arm(60000);
    if (signal.aborted) stop();
  });
}

/** metrics 计算实际动作样本的 p95 和最大耗时；不把未执行轮次算作成功。 */
function metrics(values) {
  const sorted = [...values].sort((a, b) => a - b);
  return { count: sorted.length, p95_ms: sorted.length ? sorted[Math.ceil(sorted.length * 0.95) - 1] : null, max_ms: sorted.at(-1) ?? null };
}

/** writeSummary 从增量事件计算完整度与失败，方便运行中断后复核真实证据。 */
async function writeSummary(options) {
  const events = (await fsp.readFile(path.join(runDirectory, 'events.jsonl'), 'utf8')).trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  const actions = events.filter(event => event.event === 'action_end');
  const completed = events.filter(event => event.event === 'round_end' && event.status === 'passed').length;
  const expected = options.sessions * options.sites.length * options.rounds;
  const exits = events.filter(event => event.event === 'child_exit');
  const sessionsPassed = events.filter(event => event.event === 'session_end' && event.status === 'passed').length;
  const allChildrenExited = exits.length === options.sessions && exits.every(event => event.exit_code === 0 && !event.signal);
  const errors = events.filter(event => (['action_end', 'scroll_verification', 'browser_closed', 'session_end', 'process_cleanup'].includes(event.event) && event.status === 'failed') ||
    ['session_error', 'supervisor_stop'].includes(event.event) || (event.event === 'child_exit' && (event.exit_code !== 0 || Boolean(event.signal))));
  const summary = { ...metadata, headless: options.headless, humanize: options.humanize, sites: options.sites.map(site => SITES[site]),
    requested_sessions: options.sessions, window_state_requested: options['window-state'],
    hide_before_wheel_round: options.hideBeforeWheelRound,
    cover_before_wheel_round: options.coverBeforeWheelRound, cover_seconds: options.coverSeconds,
    sessions_passed: sessionsPassed, child_exits_observed: exits.length, child_exits_all_successful: allChildrenExited,
    expected_rounds: expected, completed_rounds: completed,
    all_requested_rounds_passed: completed === expected && errors.length === 0 && sessionsPassed === options.sessions && allChildrenExited,
    action_metrics: metrics(actions.map(event => event.duration_ms)), mouse_wheel_metrics: metrics(actions.filter(event => event.step === 'mouse_wheel').map(event => event.duration_ms)),
    safe_link_results: events.filter(event => event.event === 'safe_link_result'),
    errors, error_count: errors.length, output_directory: runDirectory, ended_at: new Date().toISOString() };
  await fsp.writeFile(path.join(runDirectory, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');
  console.log('PUBLIC_STRESS_SUMMARY ' + JSON.stringify(summary));
  return summary;
}

/** selfTest 验证网址边界和统计边界，不启动浏览器或请求网络。 */
async function selfTest() {
  processCleanupSelfTest();
  assert.equal(coveredWheelObservation({ coverBeforeWheelRound: 0, sites: ['heroku'] }, 'heroku', 2), false);
  assert.equal(coveredWheelObservation({ coverBeforeWheelRound: 2, sites: ['heroku'] }, 'heroku', 1), false);
  assert.equal(coveredWheelObservation({ coverBeforeWheelRound: 2, sites: ['heroku'] }, 'heroku', 2), true);
  assert.equal(coveredWheelObservation({ coverBeforeWheelRound: 2, sites: ['heroku'] }, 'mdn', 2), false);
  for (const value of ['0', 'abc', '1.5', '300001']) assert.throws(() => argumentsForRun(['--launch-timeout-ms', value]), /launch-timeout-ms/);
  assert.throws(() => argumentsForRun(['--rounds', '30', '--hide-before-wheel-round', '31']), /hide-before-wheel-round/);
  assert.throws(() => argumentsForRun(['--hide-before-wheel-round', 'abc']), /hide-before-wheel-round/);
  assert.throws(() => argumentsForRun(['--headless', '--hide-before-wheel-round', '2']), /隐藏对照仅支持/);
  assert.throws(() => argumentsForRun(['--cover-before-wheel-round', '31']), /cover-before-wheel-round/);
  assert.throws(() => argumentsForRun(['--cover-seconds', '0']), /cover-seconds/);
  assert.throws(() => argumentsForRun(['--headless', '--cover-before-wheel-round', '2']), /遮挡对照仅支持/);
  assert.doesNotThrow(() => assertIsolatedInstallation('/tmp/camoufox-upgrade/158'));
  assert.throws(() => assertIsolatedInstallation('/tmp/normal-browser'));
  assert.throws(() => assertIsolatedInstallation('/Users/test/Library/Application Support/GoodHR/camoufox-upgrade/158'));
  assert.throws(() => assertIsolatedInstallation('C:\\Users\\test\\AppData\\Local\\GoodHR\\camoufox-upgrade\\158'));
  assert.doesNotThrow(() => assertIsolatedInstallation('D:\\Public Test\\camoufox-upgrade\\browser'));
  assert.throws(() => assertIsolatedInstallation('C:\\Users\\test\\AppData\\Roaming\\GoodHR\\camoufox-upgrade\\browser'));
  assert.equal(path.win32.relative('C:\\Test Space\\camoufox-upgrade', 'C:\\Test Space\\camoufox-upgrade\\Camoufox\\camoufox.exe'), 'Camoufox\\camoufox.exe');
  assert.equal(siteForURL(SITES.mdn + '#examples'), 'mdn');
  for (const url of ['https://www.zhipin.com/', 'https://playwright.dev.evil.test/docs/intro', 'https://playwright.dev/docs/intro?secret=1', 'https://u:p@playwright.dev/docs/intro', 'http://playwright.dev/docs/intro']) {
    assert.throws(() => siteForURL(url));
  }
  assert.deepEqual(metrics([]), { count: 0, p95_ms: null, max_ms: null });
  assert.deepEqual(metrics([10, 30, 20]), { count: 3, p95_ms: 30, max_ms: 30 });
  const previousDirectory = runDirectory;
  const previousMetadata = metadata;
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'goodhr-summary-self-test-'));
  try {
    runDirectory = temporary;
    metadata = { label: 'statistics-self-test', engine: 'fixture' };
    const options = { sessions: 1, rounds: 1, sites: ['heroku'] };
    const complete = [{ event: 'round_end', status: 'passed' }, { event: 'browser_closed', status: 'passed' },
      { event: 'session_end', status: 'passed' }, { event: 'child_exit', exit_code: 0, signal: null }];
    const cases = [complete, complete.map(event => event.event === 'browser_closed' ? { ...event, status: 'failed' } : event),
      complete.map(event => event.event === 'session_end' ? { ...event, status: 'failed' } : event),
      complete.map(event => event.event === 'child_exit' ? { ...event, exit_code: 1 } : event),
      complete.map(event => event.event === 'child_exit' ? { ...event, signal: 'SIGTERM' } : event),
      complete.filter(event => event.event !== 'child_exit'), [...complete, complete.at(-1)]];
    for (const [index, events] of cases.entries()) {
      fs.writeFileSync(path.join(temporary, 'events.jsonl'), events.map(event => JSON.stringify(event)).join('\n') + '\n');
      assert.equal((await writeSummary(options)).all_requested_rounds_passed, index === 0,
        '轮次完成后仍必须确认浏览器关闭、会话结束与子进程退出成功');
    }
  } finally {
    runDirectory = previousDirectory;
    metadata = previousMetadata;
    fs.rmSync(temporary, { recursive: true, force: true });
  }
  console.log('PUBLIC_STRESS_SELF_TEST_OK');
}

/** main 建立独立结果目录，依次执行全新会话并保存可审计汇总。 */
async function main() {
  const options = argumentsForRun(process.argv.slice(2));
  if (options.help) {
    console.log('node camoufox-public-stress.mjs --install-dir ABS --binary ABS --output-dir ABS --label VERSION [--sessions 3] [--rounds 30] [--site playwright|mdn|selenium|heroku|wikipedia] [--headless] [--humanize true|false] [--window-state visible|unchanged] [--hide-before-wheel-round 0] [--launch-timeout-ms 60000] [--cover-before-wheel-round 0 --cover-binary ABS --cover-seconds 30]');
    return;
  }
  if (options['self-test']) return selfTest();
  if (options.childSession) return childSession(options);
  const runID = randomUUID();
  const label = options.label.replace(/[^a-zA-Z0-9_.-]/g, '_').slice(0, 80);
  runDirectory = path.join(options.outputDir, `${label}-${new Date().toISOString().replace(/[:.]/g, '-')}-${runID.slice(0, 8)}`);
  await fsp.mkdir(path.join(runDirectory, 'screenshots'), { recursive: true });
  await fsp.mkdir(path.join(runDirectory, 'profiles'));
  const hash = createHash('sha256');
  for await (const chunk of fs.createReadStream(options.binary)) hash.update(chunk);
  metadata = { run_id: runID, label: options.label, engine: 'official', platform: process.platform, arch: process.arch,
    timeout_ms: options.timeout, launch_timeout_ms: options.launchTimeout, binary: options.binary,
    binary_sha256: hash.digest('hex'), install_directory: options.installDir };
  record({ event: 'run_start', sessions: options.sessions, rounds: options.rounds, sites: options.sites, pid: process.pid });
  console.log('PUBLIC_STRESS_RUN ' + JSON.stringify({ output_directory: runDirectory, ...metadata }));
  const controller = new AbortController();
  process.on('SIGTERM', () => controller.abort());
  process.on('SIGINT', () => controller.abort());
  try {
    for (let session = 1; session <= options.sessions && !controller.signal.aborted; session++) await monitorSession(options, session, controller.signal);
  } finally {
    const summary = await writeSummary(options);
    process.exitCode = controller.signal.aborted ? 130 : summary.all_requested_rounds_passed ? 0 : 1;
  }
}

main().catch(error => { console.error(error.stack ?? error.message); process.exitCode = 1; });
