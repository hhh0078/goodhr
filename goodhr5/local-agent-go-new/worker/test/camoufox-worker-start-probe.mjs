// 文件作用：用独立进程和独立 Profile 验证正式 Worker 在指定隔离 Camoufox 上的启动链路，只打开公开文档页并保存脱敏证据。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const SCRIPT = fileURLToPath(import.meta.url);
const PUBLIC_URL = 'https://playwright.dev/docs/intro';
const { values } = parseArgs({ options: {
  'install-dir': { type: 'string' }, binary: { type: 'string' }, 'output-dir': { type: 'string' },
  label: { type: 'string', default: 'worker-start' }, headless: { type: 'boolean', default: false },
  child: { type: 'boolean', default: false }, 'run-dir': { type: 'string' },
  'cover-scroll-binary': { type: 'string' }, 'self-test': { type: 'boolean', default: false },
  help: { type: 'boolean', default: false },
} });

/** isolatedPath 解析真实目录，拒绝正式 GoodHR 组件及隔离目录外的路径。 */
function isolatedPath(value, name) {
  assert.ok(value && path.isAbsolute(value), `${name} 必须是绝对路径`);
  const resolved = fs.realpathSync(value);
  const normalized = resolved.replaceAll('\\', '/').toLowerCase();
  assert.ok(normalized.split('/').includes('camoufox-upgrade'), `${name} 必须位于 camoufox-upgrade 隔离目录`);
  assert.ok(!['/library/application support/goodhr/', '/appdata/roaming/goodhr/', '/appdata/local/goodhr/']
    .some(part => (normalized + '/').includes(part)), '禁止使用用户正式运行目录');
  return resolved;
}

/** redact 保留结构化诊断与完整原因链，只隐藏凭据、用户主目录和网址查询参数。 */
function redact(value, seen = new WeakSet()) {
  if (typeof value === 'string') return value.replaceAll(os.homedir(), '<HOME>')
    .replace(/(bearer\s+|(?:password|secret|api[_-]?key)\s*[=:]\s*)[^\s&,;]+/gi, '$1[已隐藏]')
    .replace(/(https?:\/\/)[^\s/@]+:[^\s/@]+@/gi, '$1[已隐藏]@')
    .replace(/(https?:\/\/[^\s?"<>]+)\?[^\s"<>]*/g, '$1?[已隐藏]');
  if (!value || typeof value !== 'object') return value;
  if (seen.has(value)) return '[循环引用]';
  seen.add(value);
  if (Array.isArray(value)) return value.map(item => redact(item, seen));
  const result = {};
  for (const key of Object.getOwnPropertyNames(value)) {
    result[key] = /cookie|authorization|token|password|secret|api[_-]?key|proxy.*(user|pass)/i.test(key)
      ? '[已隐藏]' : redact(value[key], seen);
  }
  return result;
}

/** limited 给启动与清理设置独立限时，未完成的启动由监督进程统一收尾。 */
async function limited(operation, timeoutMS, step) {
  let timer;
  try {
    return await Promise.race([Promise.resolve().then(operation), new Promise((_, reject) => {
      timer = setTimeout(() => reject(Object.assign(new Error(`${step} 超过 ${timeoutMS} 毫秒`),
        { code: 'PROBE_TIMEOUT', step, timeout_ms: timeoutMS })), timeoutMS);
    })]);
  } finally { clearTimeout(timer); }
}

/** context 为正式封装能力创建独立追踪编号和动作总预算。 */
function context(action, timeoutMS) {
  const startedAt = Date.now();
  return { action, trace_id: 'isolated-worker-start-probe', request_id: randomUUID(),
    started_at: startedAt, deadline_at: startedAt + timeoutMS };
}

/** stage 保存每个正式封装调用的独立预算、耗时和错误，完整结果由报告对应字段保留。 */
async function stage(report, step, timeoutMS, operation) {
  const entry = { step, started_at: new Date().toISOString(), budget_ms: timeoutMS, status: 'running' };
  report.stages.push(entry);
  const started = performance.now();
  try {
    const result = await limited(operation, timeoutMS, step);
    entry.status = 'passed';
    return result;
  } catch (error) {
    entry.status = 'failed';
    entry.error = redact(error);
    throw error;
  } finally { entry.duration_ms = Math.round(performance.now() - started); }
}

/** probe 只使用本进程新建的正式 ActionService，不连接任何已运行的 Worker。 */
async function probe(options) {
  process.env.CAMOUFOX_INSTALL_DIR = options.installDir;
  process.env.CAMOUFOX_BINARY_PATH = options.binary;
  // Go 探针先离线安装内置 UBO；这里只使用当前隔离副本的扩展，不继承用户进程的插件路径。
  const addonPath = process.platform === 'darwin'
    ? path.join(options.installDir, 'Camoufox.app', 'Contents', 'Resources', 'addons', 'UBO')
    : path.join(options.installDir, 'addons', 'UBO');
  const addonRoot = isolatedPath(addonPath, 'bundled-ubo');
  const relativeAddon = path.relative(options.installDir, addonRoot);
  assert.ok(relativeAddon && !relativeAddon.startsWith('..') && !path.isAbsolute(relativeAddon),
    '内置 UBO 必须来自当前隔离浏览器副本');
  const manifest = path.join(addonRoot, 'manifest.json');
  assert.ok(fs.statSync(manifest).isFile(), '内置 UBO 的 manifest.json 不存在，请先运行 Go 兼容探针');
  assert.ok(JSON.parse(fs.readFileSync(manifest, 'utf8')).manifest_version, '内置 UBO 清单无法识别');
  process.env.GOODHR_UBO_EXTENSION_PATH = addonRoot;
  // 本探针没有执行 Go 资源适配；不能继承其他进程声称的磁盘检查状态。
  delete process.env.GOODHR_NATIVE_INPUT_SAFETY;
  const { ActionService } = await import('../dist/browser/actions/action-service.js');
  const actions = new ActionService();
  const startedAt = performance.now();
  const report = { label: options.label, timestamp: new Date().toISOString(), url: PUBLIC_URL,
    platform: `${process.platform}-${process.arch}`, timeout_ms: 60000,
    startup_and_navigation_budget_ms: 60000, cleanup_budget_ms: 10000, supervisor_budget_ms: options.coverScrollBinary ? 130000 : 80000,
    cover_scroll_enabled: Boolean(options.coverScrollBinary), stages: [],
    independent_worker: true, independent_profile: true, passed: false,
    runtime: actions.runtimeStatus(), health_before: actions.healthStatus() };
  try {
    report.browser = await stage(report, 'browser.start', 60000, () => actions.startBrowser({ headless: options.headless,
      user_data_dir: path.join(options.runDir, 'profile'), downloads_path: path.join(options.runDir, 'downloads'),
      url: PUBLIC_URL, wait_until: 'domcontentloaded', timeout_ms: 60000, extension_paths: [],
    }, context('browser.start', 60000)));
    report.start_duration_ms = Math.round(performance.now() - startedAt);
    report.passed = report.browser.running === true;
    if (options.coverScrollBinary) {
      assert.equal(report.passed, true, '正式 Worker 没有启动浏览器，不能进行遮挡滚轮验证');
      report.normal_scroll = await stage(report, 'scroll.before_cover', 45000, () =>
        actions.scroll({ distance: 240 }, context('page.scroll', 45000)));
      assert.equal(report.normal_scroll.scrolled, true, '遮挡前未确认普通真实滚轮移动，不进入遮挡实验');
      const helper = fileURLToPath(new URL('./camoufox-window-state.mjs', import.meta.url));
      report.cover = await stage(report, 'window.cover', 12000, () => {
        const output = execFileSync(process.execPath, [helper, '--session-pid', String(process.pid), '--state', 'covered',
          '--cover-binary', options.coverScrollBinary, '--cover-seconds', '60'], { encoding: 'utf8', timeout: 12000, maxBuffer: 1024 * 1024 });
        const covered = JSON.parse(output.trim());
        assert.ok(covered.ok && covered.ready && covered.covered && covered.cover_pid > 0, '未取得真实遮挡窗口证据');
        return covered;
      });
      report.covered_scroll = await stage(report, 'scroll.under_cover', 45000, () =>
        actions.scroll({ distance: 300, max_attempts: 6, wait_ms: 0 }, context('page.scroll', 45000)));
      assert.equal(report.covered_scroll.scrolled, true, '正式 Worker 遮挡后滚轮没有确认移动；不能标记通过');
      report.health_after_covered_scroll = actions.healthStatus();
    }
  } catch (error) {
    report.passed = false;
    report.error = redact(error);
    if (error.stdout) report.helper_stdout = redact(String(error.stdout));
    if (error.stderr) report.helper_stderr = redact(String(error.stderr));
    report.health_after_error = actions.healthStatus();
  }
  finally {
    report.start_duration_ms ??= Math.round(performance.now() - startedAt);
    report.health_after_start = actions.healthStatus();
    try {
      report.cleanup = await limited(() => actions.stopBrowser(context('browser.stop', 10000)), 10000, 'browser.stop');
    } catch (error) { report.cleanup_error = redact(error); report.passed = false; }
    report.health_after_cleanup = actions.healthStatus();
    report.total_duration_ms = Math.round(performance.now() - startedAt);
  }
  return redact(report);
}

/** descendantPIDs 只读取本探针子进程的后代编号，不保存其他用户进程的命令行。 */
function descendantPIDs(childPID) {
  if (process.platform === 'win32') return [];
  const output = spawnSync('ps', ['-axo', 'pid=,ppid='], { encoding: 'utf8' });
  if (output.status !== 0) return [];
  const rows = output.stdout.trim().split('\n').map(line => line.trim().split(/\s+/).map(Number));
  const found = new Set([childPID]);
  for (let changed = true; changed;) {
    changed = false;
    for (const [pid, ppid] of rows) if (found.has(ppid) && !found.has(pid)) { found.add(pid); changed = true; }
  }
  return [...found].reverse();
}

/** supervise 普通启动限八十秒、显式遮挡实验限一百三十秒，只清理本次进程树和遮挡夹具所在独立组。 */
async function supervise(options) {
  const child = spawn(process.execPath, [SCRIPT, ...process.argv.slice(2), '--child', '--run-dir', options.runDir],
    { detached: Boolean(options.coverScrollBinary) && process.platform !== 'win32', stdio: ['ignore', 'pipe', 'pipe', 'ipc'] });
  let report;
  let forced = false;
  let stderr = '';
  let stdout = '';
  let killed = false;
  /** cleanupCoverGroup 回收本次显式遮挡实验的新进程组，包含辅助退出后仍留在组内的定时遮挡窗。 */
  function cleanupCoverGroup() {
    if (!options.coverScrollBinary || !child.pid || process.platform === 'win32') return;
    try { process.kill(-child.pid, 'SIGKILL'); } catch (error) { if (error.code !== 'ESRCH') throw error; }
  }
  /** terminate 先记录进程亲缘，再退出子进程，避免误伤其他浏览器。 */
  function terminate() {
    if (killed || !child.pid) return;
    killed = true;
    forced = true;
    if (process.platform === 'win32') spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F']);
    else for (const pid of descendantPIDs(child.pid)) { try { process.kill(pid, 'SIGKILL'); } catch {} }
    cleanupCoverGroup();
  }
  const watchdog = setTimeout(terminate, options.coverScrollBinary ? 130000 : 80000);
  process.once('SIGTERM', terminate);
  process.once('SIGINT', terminate);
  child.stdout.on('data', chunk => { stdout += chunk; });
  child.stderr.on('data', chunk => { stderr += chunk; });
  child.on('message', message => { if (message?.type === 'probe_result') report = message.result; });
  await new Promise(resolve => { child.once('close', resolve); child.once('error', error => {
    report = { passed: false, error: redact(error) }; resolve();
  }); });
  clearTimeout(watchdog);
  cleanupCoverGroup();
  process.removeListener('SIGTERM', terminate);
  process.removeListener('SIGINT', terminate);
  report ??= { label: options.label, passed: false, error: { code: 'PROBE_CHILD_INCOMPLETE',
    message: '正式 Worker 启动探针未在硬上限内返回结果' } };
  report.supervisor_forced_cleanup = forced;
  if (forced) report.passed = false;
  await fsp.writeFile(path.join(options.runDir, 'worker-output.log'), redact(stdout + stderr), { mode: 0o600 });
  await fsp.writeFile(path.join(options.runDir, 'result.json'), JSON.stringify(redact(report), null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify({ passed: report.passed, forced_cleanup: forced, result: path.join(options.runDir, 'result.json') }));
  process.exitCode = report.passed ? 0 : 1;
}

/** selfTest 验证阶段异常不会被记成成功，并检查正式调用预算和脱敏，不启动浏览器或窗口。 */
async function selfTest() {
  assert.ok(fs.existsSync(fileURLToPath(new URL('./camoufox-window-state.mjs', import.meta.url))), '遮挡辅助路径必须可解析，不能覆盖 URL 构造器');
  const report = { stages: [] };
  await assert.rejects(() => stage(report, 'synthetic.worker_error', 100, () => { throw Object.assign(new Error('模拟正式 Worker 错误'), { code: 'WORKER_UNRESPONSIVE' }); }),
    error => error.code === 'WORKER_UNRESPONSIVE');
  assert.equal(report.stages[0].status, 'failed');
  assert.equal(report.stages[0].error.code, 'WORKER_UNRESPONSIVE');
  const deadline = context('page.scroll', 45000);
  assert.equal(deadline.deadline_at - deadline.started_at, 45000);
  assert.equal(redact({ authorization: 'secret' }).authorization, '[已隐藏]');
  console.log('WORKER_START_PROBE_SELF_TEST_OK');
}

if (values.help) {
  console.log('node test/camoufox-worker-start-probe.mjs --install-dir /.../camoufox-upgrade/browser --binary /.../camoufox --output-dir /.../camoufox-upgrade/results [--label 152-worker] [--headless] [--cover-scroll-binary /.../camoufox-window-cover]');
} else if (values['self-test']) {
  await selfTest();
} else {
  const installDir = isolatedPath(values['install-dir'], 'install-dir');
  const binary = isolatedPath(values.binary, 'binary');
  const relative = path.relative(installDir, binary);
  assert.ok(relative && !relative.startsWith('..') && !path.isAbsolute(relative), 'binary 必须在隔离安装目录内');
  assert.ok(values['output-dir'] && path.isAbsolute(values['output-dir']), 'output-dir 必须是绝对路径');
  const outputDir = isolatedPath(values['output-dir'], 'output-dir');
  const runDir = values.child ? isolatedPath(values['run-dir'], 'run-dir')
    : await fsp.mkdtemp(path.join(outputDir, 'worker-start-'));
  let coverScrollBinary;
  if (values['cover-scroll-binary']) {
    assert.ok(process.platform === 'darwin' && !values.headless, '正式 Worker 遮挡滚轮实验仅支持 Mac 有窗口模式');
    coverScrollBinary = isolatedPath(values['cover-scroll-binary'], 'cover-scroll-binary');
    assert.ok(fs.statSync(coverScrollBinary).isFile(), 'cover-scroll-binary 必须是隔离的已编译文件');
    fs.accessSync(coverScrollBinary, fs.constants.X_OK);
  }
  const options = { installDir, binary, outputDir, runDir, label: values.label, headless: values.headless, coverScrollBinary };
  if (values.child) {
    try {
      const result = await probe(options);
      process.send({ type: 'probe_result', result }, () => process.disconnect());
    } catch (error) { process.send({ type: 'probe_result', result: { passed: false, error: redact(error) } }, () => process.disconnect()); }
  } else await supervise(options);
}
