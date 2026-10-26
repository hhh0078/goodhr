// 文件作用：在 macOS 仅按公开压力测试进程树隐藏、显示或用独立窗口遮挡隔离 Camoufox；不执行页面脚本或操作其他浏览器。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync, spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const STRESS_SCRIPT = fileURLToPath(new URL('./camoufox-public-stress.mjs', import.meta.url));
const WORKER_PROBE_SCRIPT = fileURLToPath(new URL('./camoufox-worker-start-probe.mjs', import.meta.url));
const APPKIT_SCRIPT = `
ObjC.import('AppKit');
/** run 仅对已验证的测试应用 PID 执行 AppKit 窗口状态操作，并再次校验可执行路径。 */
function run(argv) {
  const pid = Number(argv[0]);
  const state = argv[1];
  const expected = argv[2];
  const app = $.NSRunningApplication.runningApplicationWithProcessIdentifier(pid);
  if (!app || Number(app.processIdentifier) !== pid) throw new Error('测试浏览器已经退出，未操作其他窗口');
  const actual = ObjC.unwrap(app.executableURL.path.stringByResolvingSymlinksInPath);
  if (actual !== expected) throw new Error('测试浏览器 PID 或执行路径发生变化，已拒绝操作');
  const before = { active: Boolean(app.isActive), hidden: Boolean(app.isHidden) };
  let accepted = true;
  if (state === 'hidden') accepted = Boolean(app.hide);
  if (state === 'visible') {
    accepted = Boolean(app.unhide);
    accepted = Boolean(app.activateWithOptions(2)) && accepted;
  }
  if (state !== 'status') $.NSThread.sleepForTimeInterval(0.3);
  return JSON.stringify({ browser_pid: pid, executable: actual, state, accepted, before,
    active: Boolean(app.isActive), hidden: Boolean(app.isHidden) });
}
`;

/** parseProcessTable 解析标准 ps 的 PID、父 PID 和完整程序路径，不依赖窗口标题或应用名称。 */
function parseProcessTable(output) {
  return output.split('\n').flatMap(line => {
    const match = line.match(/^\s*(\d+)\s+(\d+)\s+(.+?)\s*$/);
    return match ? [{ pid: Number(match[1]), ppid: Number(match[2]), command: match[3] }] : [];
  });
}

/** descendants 找出指定会话进程的所有后代；不搜索或操作其他 Node 会话。 */
function descendants(processes, sessionPID) {
  const owned = new Set([sessionPID]);
  let previousSize;
  do {
    previousSize = owned.size;
    for (const item of processes) if (owned.has(item.ppid)) owned.add(item.pid);
  } while (owned.size !== previousSize);
  return processes.filter(item => item.pid !== sessionPID && owned.has(item.pid));
}

/** assertIsolatedBinary 要求实际路径处于 camoufox-upgrade，并明确拒绝用户正式 GoodHR 安装目录。 */
function assertIsolatedBinary(binary) {
  const normalized = binary.replaceAll('\\', '/').toLowerCase();
  assert.ok(path.isAbsolute(binary) && normalized.split('/').includes('camoufox-upgrade'), '浏览器必须来自 camoufox-upgrade 隔离副本');
  assert.ok(!normalized.includes('/library/application support/goodhr/'), '禁止操作用户正式 GoodHR 浏览器');
  assert.match(normalized, /\/camoufox\.app\/contents\/macos\/camoufox$/, '只允许独立 Camoufox 应用主进程');
}

/** processArguments 获取单个已选进程的命令行，仅用于确认身份，不输出其他用户进程内容。 */
function processArguments(pid) {
  return execFileSync('/bin/ps', ['-p', String(pid), '-o', 'args='], { encoding: 'utf8', timeout: 5000, maxBuffer: 1024 * 1024 }).trim();
}

/** approvedSessionCommand 只接受完整脚本参数和对应子会话标志；正式 Worker 探针仅允许 covered。 */
function approvedSessionCommand(command, state) {
  /** hasScript 按完整参数边界核对脚本路径，拒绝同名前缀或备份文件。 */
  const hasScript = script => new RegExp(`(?:^|\\s|["'])${script.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=\\s|["']|$)`).test(command);
  return (hasScript(STRESS_SCRIPT) && /(?:^|\s)--child-session(?:\s+|=)\d+(?:\s|$)/.test(command)) ||
    (state === 'covered' && hasScript(WORKER_PROBE_SCRIPT) && /(?:^|\s)--child(?:\s|$)/.test(command));
}

/** selectBrowser 验证压力测试子会话身份，要求其进程树内恰好一个隔离浏览器主进程。 */
function selectBrowser(sessionPID, state) {
  const processes = parseProcessTable(execFileSync('/bin/ps', ['-axo', 'pid=,ppid=,comm='], {
    encoding: 'utf8', timeout: 5000, maxBuffer: 8 * 1024 * 1024,
  }));
  const session = processes.find(item => item.pid === sessionPID);
  assert.ok(session && /^node(?:js)?$/i.test(path.basename(session.command)), '指定 PID 不是仍在运行的 Node 测试会话');
  const sessionArgs = processArguments(sessionPID);
  assert.ok(approvedSessionCommand(sessionArgs, state), '指定 Node PID 不属于允许的独立测试子会话');
  const candidates = descendants(processes, sessionPID).filter(item => path.basename(item.command).toLowerCase() === 'camoufox')
    .filter(item => !/(?:^|\s)-(?:contentproc|gpu|utility)(?:\s|$)/.test(processArguments(item.pid)));
  assert.equal(candidates.length, 1, `测试会话内匹配到 ${candidates.length} 个浏览器主进程，无法唯一确认，未操作窗口`);
  const browser = candidates[0];
  assertIsolatedBinary(browser.command);
  const binary = fs.realpathSync(browser.command);
  assertIsolatedBinary(binary);
  // AppKit 操作前再次确认同一 PID 的路径，降低进程已退出或 PID 变化时的误操作风险。
  const current = parseProcessTable(execFileSync('/bin/ps', ['-p', String(browser.pid), '-o', 'pid=,ppid=,comm='], { encoding: 'utf8', timeout: 5000 }))[0];
  assert.ok(current && current.command === browser.command && current.ppid === browser.ppid, '浏览器进程身份发生变化，未操作窗口');
  return { pid: browser.pid, binary };
}

/** startCover 启动继承测试进程组的独立遮挡窗，收到真实可见确认后让辅助进程退出。 */
async function startCover(browser, binary, seconds) {
  assert.ok(binary && path.isAbsolute(binary), 'covered 必须明确指定已编译 cover-binary 的绝对路径');
  const executable = fs.realpathSync(binary);
  assert.ok(fs.statSync(executable).isFile(), 'cover-binary 不是可执行文件');
  fs.accessSync(executable, fs.constants.X_OK);
  assert.ok(Number.isInteger(seconds) && seconds > 0 && seconds <= 300, 'cover-seconds 必须为 1 到 300 的整数');
  const child = spawn(executable, ['--browser-pid', String(browser.pid), '--seconds', String(seconds)], {
    detached: false, stdio: ['ignore', 'pipe', 'pipe'],
  });
  return new Promise((resolve, reject) => {
    let output = '';
    let errorOutput = '';
    let ready = false;
    /** stop 只终止本辅助启动的夹具，绝不触碰浏览器或其他应用。 */
    const stop = error => { if (ready) return; clearTimeout(timer); child.kill('SIGTERM'); reject(error); };
    const timer = setTimeout(() => stop(new Error('遮挡窗未在 5 秒内确认显示')), 5000);
    child.stderr.on('data', chunk => { errorOutput = (errorOutput + chunk.toString()).slice(-8192); });
    child.once('error', stop);
    child.once('exit', code => { if (!ready) stop(new Error(`遮挡夹具提前退出 code=${code} ${errorOutput}`)); });
    child.stdout.on('data', chunk => {
      output += chunk.toString();
      if (output.length > 32768) return stop(new Error('遮挡夹具输出超出预期'));
      if (!output.includes('\n')) return;
      try {
        const result = JSON.parse(output.split('\n')[0]);
        assert.ok(result.ready === true && result.covered === true && result.browser_pid === browser.pid && result.cover_pid === child.pid,
          result.error_message ?? '遮挡夹具没有返回可验证的窗口证据');
        ready = true;
        clearTimeout(timer);
        // 不 detached，仍受 stress 会话进程组管理；解除句柄与管道等待，允许本辅助正常退出。
        child.unref();
        child.stdout.destroy();
        child.stderr.destroy();
        resolve({ ok: true, state: 'covered', ...result });
      } catch (error) { stop(error); }
    });
  });
}

/** selfTest 只验证进程树和路径边界，不读取窗口、不执行 AppKit 操作。 */
function selfTest() {
  assert.equal(approvedSessionCommand(`node ${STRESS_SCRIPT} --child-session 1`, 'covered'), true);
  assert.equal(approvedSessionCommand(`node ${STRESS_SCRIPT} --child-session 1`, 'visible'), true);
  assert.equal(approvedSessionCommand(`node ${WORKER_PROBE_SCRIPT} --child`, 'covered'), true);
  assert.equal(approvedSessionCommand(`node ${WORKER_PROBE_SCRIPT} --child`, 'visible'), false);
  assert.equal(approvedSessionCommand(`node ${WORKER_PROBE_SCRIPT}.bak --child`, 'covered'), false);
  assert.equal(approvedSessionCommand(`node ${WORKER_PROBE_SCRIPT} --child-extra`, 'covered'), false);
  assert.equal(approvedSessionCommand(`node ${WORKER_PROBE_SCRIPT}`, 'covered'), false);
  const processes = parseProcessTable('100 1 /opt/node\n101 100 /tmp/camoufox-upgrade/158/Camoufox.app/Contents/MacOS/camoufox\n102 101 /tmp/helper\n200 1 /tmp/other\n');
  assert.deepEqual(descendants(processes, 100).map(item => item.pid), [101, 102]);
  assert.deepEqual(descendants(processes, 999), []);
  assert.doesNotThrow(() => assertIsolatedBinary('/tmp/camoufox-upgrade/158/Camoufox.app/Contents/MacOS/camoufox'));
  assert.throws(() => assertIsolatedBinary('/Applications/Camoufox.app/Contents/MacOS/camoufox'));
  assert.throws(() => assertIsolatedBinary('/Users/test/Library/Application Support/GoodHR/camoufox-upgrade/Camoufox.app/Contents/MacOS/camoufox'));
  assert.throws(() => assertIsolatedBinary('/tmp/camoufox-upgrade/158/Camoufox.app/Contents/MacOS/plugin-container'));
  console.log(JSON.stringify({ ok: true, self_test: 'passed', window_operations: 0 }));
}

/** main 在 macOS 验证唯一测试进程后调用 AppKit，其他系统明确拒绝窗口操作。 */
async function main() {
  const { values } = parseArgs({ options: { 'session-pid': { type: 'string' }, state: { type: 'string', default: 'status' },
    'cover-binary': { type: 'string' }, 'cover-seconds': { type: 'string', default: '30' },
    'self-test': { type: 'boolean', default: false }, help: { type: 'boolean', default: false } } });
  if (values.help) {
    console.log('node camoufox-window-state.mjs --session-pid POSITIVE_PID --state hidden|visible|status|covered [--cover-binary ABS] [--cover-seconds 30]');
    return;
  }
  if (values['self-test']) return selfTest();
  assert.equal(process.platform, 'darwin', '此辅助仅支持 macOS AppKit；当前系统不支持，未操作窗口');
  assert.ok(/^\d+$/.test(values['session-pid'] ?? ''), 'session-pid 必须是压力测试子会话的正整数 PID');
  const sessionPID = Number(values['session-pid']);
  assert.ok(Number.isSafeInteger(sessionPID) && sessionPID > 0, 'session-pid 必须是正整数');
  assert.ok(['hidden', 'visible', 'status', 'covered'].includes(values.state), 'state 只能是 hidden、visible、status、covered');
  const browser = selectBrowser(sessionPID, values.state);
  if (values.state === 'covered') {
    const result = await startCover(browser, values['cover-binary'], Number(values['cover-seconds']));
    console.log(JSON.stringify({ session_pid: sessionPID, ...result }));
    return;
  }
  const output = execFileSync('/usr/bin/osascript', ['-l', 'JavaScript', '-e', APPKIT_SCRIPT, String(browser.pid), values.state, browser.binary], {
    encoding: 'utf8', timeout: 10000, maxBuffer: 1024 * 1024,
  });
  const state = JSON.parse(output.trim());
  console.log(JSON.stringify({ ok: true, session_pid: sessionPID, ...state }));
  if ((values.state === 'hidden' && !state.hidden) || (values.state === 'visible' && state.hidden)) process.exitCode = 1;
}

main().catch(error => { console.error(JSON.stringify({ ok: false, error_message: error.message })); process.exitCode = 1; });
