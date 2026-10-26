// 文件作用：按独立测试进程树定位隔离 Camoufox；Mac 用 AppKit 隐藏、显示或遮挡，Windows 只用专属 PowerShell 浮动窗遮挡；不执行页面脚本。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync, spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { readProcessIdentities, readProcessArguments } from './camoufox-owned-processes.mjs';

const STRESS_SCRIPT = fileURLToPath(new URL('./camoufox-public-stress.mjs', import.meta.url));
const WORKER_PROBE_SCRIPT = fileURLToPath(new URL('./camoufox-worker-start-probe.mjs', import.meta.url));
const WINDOWS_WINDOW_OWNERS = `
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
// GoodHRVisibleWindowOwners 只读取指定候选 PID 的可见窗口元数据，不创建窗口、不读取标题。
public static class GoodHRVisibleWindowOwners {
  [StructLayout(LayoutKind.Sequential)] private struct Rect { public int Left, Top, Right, Bottom; }
  private delegate bool Visitor(IntPtr handle, IntPtr parameter);
  [DllImport("user32.dll")] private static extern bool EnumWindows(Visitor visitor, IntPtr parameter);
  [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr handle, out uint owner);
  [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr handle, out Rect rectangle);
  [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr handle);
  [DllImport("user32.dll")] private static extern bool IsIconic(IntPtr handle);
  // Find 只返回确实拥有可见正常尺寸窗口的候选 PID；空输入直接返回，不枚举系统窗口。
  public static int[] Find(int[] candidates) {
    HashSet<int> allowed = new HashSet<int>(candidates), found = new HashSet<int>();
    if (allowed.Count == 0) return new int[0];
    EnumWindows(delegate(IntPtr handle, IntPtr parameter) {
      uint owner; GetWindowThreadProcessId(handle, out owner); Rect rectangle;
      if (allowed.Contains((int)owner) && IsWindowVisible(handle) && !IsIconic(handle) && GetWindowRect(handle, out rectangle) &&
          rectangle.Right - rectangle.Left > 100 && rectangle.Bottom - rectangle.Top > 100) found.Add((int)owner);
      return true;
    }, IntPtr.Zero);
    int[] result = new int[found.Count]; found.CopyTo(result); return result;
  }
}
'@
$candidates = @((ConvertFrom-Json -InputObject $env:GOODHR_TEST_WINDOW_CANDIDATES) | Where-Object { $null -ne $_ -and $_ -gt 0 })
$visible = @([GoodHRVisibleWindowOwners]::Find([int[]]$candidates))
ConvertTo-Json -InputObject $visible -Compress
`;
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
function assertIsolatedBinary(binary, platform = process.platform) {
  const normalized = binary.replaceAll('\\', '/').toLowerCase();
  assert.ok((platform === 'win32' ? path.win32.isAbsolute(binary) : path.isAbsolute(binary)) && normalized.split('/').includes('camoufox-upgrade'), '浏览器必须来自 camoufox-upgrade 隔离副本');
  assert.ok(!['/library/application support/goodhr/', '/appdata/local/goodhr/', '/appdata/roaming/goodhr/'].some(part => normalized.includes(part)), '禁止操作用户正式 GoodHR 浏览器');
  assert.match(normalized, platform === 'win32' ? /\/camoufox\.exe$/ : /\/camoufox\.app\/contents\/macos\/camoufox$/, '只允许独立 Camoufox 应用主进程');
}

/** processArguments 获取单个已选进程的命令行，仅用于确认身份，不输出其他用户进程内容。 */
function processArguments(pid) {
  if (process.platform === 'win32') return readProcessArguments(pid);
  return execFileSync('/bin/ps', ['-p', String(pid), '-o', 'args='], { encoding: 'utf8', timeout: 5000, maxBuffer: 1024 * 1024 }).trim();
}

/** approvedSessionCommand 只接受完整脚本参数和对应子会话标志；正式 Worker 探针仅允许 covered。 */
function approvedSessionCommand(command, state) {
  /** hasScript 按完整参数边界核对脚本路径，拒绝同名前缀或备份文件。 */
  const hasScript = script => new RegExp(`(?:^|\\s|["'])${script.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=\\s|["']|$)`).test(command);
  return (hasScript(STRESS_SCRIPT) && /(?:^|\s)--child-session(?:\s+|=)\d+(?:\s|$)/.test(command)) ||
    (state === 'covered' && hasScript(WORKER_PROBE_SCRIPT) && /(?:^|\s)--child(?:\s|$)/.test(command));
}

/** visibleWindowsCandidatePIDs 只在已证明的浏览器后代中核对真实窗口所有者，排除 Windows 启动器及无窗口辅助进程。 */
function visibleWindowsCandidatePIDs(candidates) {
  const runner = fs.realpathSync(path.join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe'));
  const output = execFileSync(runner, ['-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', Buffer.from(WINDOWS_WINDOW_OWNERS, 'utf16le').toString('base64')], {
    encoding: 'utf8', timeout: 15000, maxBuffer: 1024 * 1024, windowsHide: true,
    env: { ...process.env, GOODHR_TEST_WINDOW_CANDIDATES: JSON.stringify(candidates) },
  });
  const decoded = JSON.parse(output.trim().replace(/^\uFEFF/, ''));
  const visible = (Array.isArray(decoded) ? decoded : decoded == null ? [] : [decoded]).filter(pid => Number.isSafeInteger(pid) && candidates.includes(pid));
  return [...new Set(visible)];
}

/** selectBrowser 验证压力测试子会话身份，要求其进程树内恰好一个隔离浏览器主进程。 */
function selectBrowser(sessionPID, state) {
  const processes = process.platform === 'win32' ? readProcessIdentities().map(row => ({ ...row, command: row.executable })) : parseProcessTable(execFileSync('/bin/ps', ['-axo', 'pid=,ppid=,comm='], {
    encoding: 'utf8', timeout: 5000, maxBuffer: 8 * 1024 * 1024,
  }));
  const session = processes.find(item => item.pid === sessionPID);
  assert.ok(session && /^node(?:js)?(?:\.exe)?$/i.test(path.basename(session.command)), '指定 PID 不是仍在运行的 Node 测试会话');
  const sessionArgs = processArguments(sessionPID);
  assert.ok(approvedSessionCommand(sessionArgs, state), '指定 Node PID 不属于允许的独立测试子会话');
  let candidates = descendants(processes, sessionPID).filter(item => /^camoufox(?:\.exe)?$/i.test(path.basename(item.command)))
    .filter(item => !/(?:^|\s|["'])-(?:contentproc|gpu|utility)(?=\s|["']|$)/.test(processArguments(item.pid)));
  if (process.platform === 'win32') {
    const visible = visibleWindowsCandidatePIDs(candidates.map(item => item.pid));
    candidates = candidates.filter(item => visible.includes(item.pid));
  }
  assert.equal(candidates.length, 1, `测试会话内匹配到 ${candidates.length} 个浏览器主进程，无法唯一确认，未操作窗口`);
  const browser = candidates[0];
  assertIsolatedBinary(browser.command);
  const binary = fs.realpathSync(browser.command);
  assertIsolatedBinary(binary);
  // AppKit 操作前再次确认同一 PID 的路径，降低进程已退出或 PID 变化时的误操作风险。
  const current = process.platform === 'win32' ? readProcessIdentities().map(row => ({ ...row, command: row.executable })).find(row => row.pid === browser.pid)
    : parseProcessTable(execFileSync('/bin/ps', ['-p', String(browser.pid), '-o', 'pid=,ppid=,comm='], { encoding: 'utf8', timeout: 5000 }))[0];
  assert.ok(current && current.command === browser.command && current.ppid === browser.ppid &&
    (process.platform !== 'win32' || current.started === browser.started), '浏览器进程身份发生变化，未操作窗口');
  return { pid: browser.pid, binary };
}

/** startCover 启动继承测试进程组的独立遮挡窗，收到真实可见确认后让辅助进程退出。 */
async function startCover(browser, binary, seconds) {
  assert.ok(binary && path.isAbsolute(binary), 'covered 必须明确指定已编译 cover-binary 的绝对路径');
  const executable = fs.realpathSync(binary);
  assert.ok(fs.statSync(executable).isFile(), 'cover-binary 不是可执行文件');
  fs.accessSync(executable, process.platform === 'win32' ? fs.constants.R_OK : fs.constants.X_OK);
  assert.ok(Number.isInteger(seconds) && seconds > 0 && seconds <= 300, 'cover-seconds 必须为 1 到 300 的整数');
  const isWindows = process.platform === 'win32';
  if (isWindows) assert.equal(path.extname(executable).toLowerCase(), '.ps1', 'Windows 遮挡夹具必须是明确的 PowerShell 脚本');
  const runner = isWindows ? fs.realpathSync(path.join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe')) : executable;
  const args = isWindows ? ['-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', executable, '-BrowserPid', String(browser.pid), '-Seconds', String(seconds)]
    : ['--browser-pid', String(browser.pid), '--seconds', String(seconds)];
  const child = spawn(runner, args, {
    detached: false, windowsHide: isWindows, stdio: ['ignore', 'pipe', 'pipe'],
  });
  return new Promise((resolve, reject) => {
    let output = '';
    let errorOutput = '';
    let ready = false;
    /** stop 只终止本辅助启动的夹具，绝不触碰浏览器或其他应用。 */
    const stop = error => { if (ready) return; clearTimeout(timer); child.kill('SIGTERM'); reject(error); };
    const timer = setTimeout(() => stop(new Error('遮挡窗未在夹具初始化预算内确认显示')), isWindows ? 20000 : 5000);
    child.stderr.on('data', chunk => { errorOutput = (errorOutput + chunk.toString()).slice(-8192); });
    child.once('error', stop);
    child.once('exit', code => { if (!ready) stop(new Error(`遮挡夹具提前退出 code=${code} ${errorOutput}`)); });
    child.stdout.on('data', chunk => {
      output += chunk.toString();
      if (output.length > 32768) return stop(new Error('遮挡夹具输出超出预期'));
      if (!output.includes('\n')) return;
      try {
        const result = JSON.parse(output.split('\n')[0]);
        if (!(result.ready === true && result.covered === true && result.browser_pid === browser.pid && result.cover_pid === child.pid)) {
          throw Object.assign(new Error(result.error_message ?? '遮挡夹具没有返回可验证的窗口证据'), { fixture_diagnostic: result });
        }
        ready = true;
        clearTimeout(timer);
        // 不 detached，仍受 stress 会话进程组管理；解除句柄与管道等待，允许本辅助正常退出。
        child.unref();
        child.stdout.destroy();
        child.stderr.destroy();
        resolve({ ok: true, state: 'covered', fixture_parent_pid: process.pid, helper_parent_pid: process.ppid, fixture_executable: runner, ...result });
      } catch (error) { stop(error); }
    });
  });
}

/** selfTest 只验证进程树和路径边界，不读取窗口、不执行 AppKit 操作。 */
function selfTest() {
  if (process.platform === 'win32') assert.deepEqual(visibleWindowsCandidatePIDs([]), [], '空候选只编译窗口元数据探针，不能读取或创建窗口');
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
  assert.doesNotThrow(() => assertIsolatedBinary('/tmp/camoufox-upgrade/158/Camoufox.app/Contents/MacOS/camoufox', 'darwin'));
  assert.throws(() => assertIsolatedBinary('/Applications/Camoufox.app/Contents/MacOS/camoufox'));
  assert.throws(() => assertIsolatedBinary('/Users/test/Library/Application Support/GoodHR/camoufox-upgrade/Camoufox.app/Contents/MacOS/camoufox'));
  assert.throws(() => assertIsolatedBinary('/tmp/camoufox-upgrade/158/Camoufox.app/Contents/MacOS/plugin-container'));
  assert.doesNotThrow(() => assertIsolatedBinary('D:\\Public Test\\camoufox-upgrade\\browser\\camoufox.exe', 'win32'));
  assert.throws(() => assertIsolatedBinary('C:\\Users\\test\\AppData\\Local\\GoodHR\\camoufox-upgrade\\camoufox.exe', 'win32'));
  console.log(JSON.stringify({ ok: true, self_test: 'passed', window_operations: 0 }));
}

/** main 核对唯一测试进程；Mac 保留 AppKit，Windows 仅允许显式的 PowerShell 遮挡夹具。 */
async function main() {
  const { values } = parseArgs({ options: { 'session-pid': { type: 'string' }, state: { type: 'string', default: 'status' },
    'cover-binary': { type: 'string' }, 'cover-seconds': { type: 'string', default: '30' },
    'self-test': { type: 'boolean', default: false }, help: { type: 'boolean', default: false } } });
  if (values.help) {
    console.log('node camoufox-window-state.mjs --session-pid POSITIVE_PID --state hidden|visible|status|covered [--cover-binary ABS] [--cover-seconds 30]');
    return;
  }
  if (values['self-test']) return selfTest();
  assert.ok(process.platform === 'darwin' || (process.platform === 'win32' && values.state === 'covered'), '此系统仅支持 Windows 遮挡或 macOS AppKit；未操作窗口');
  assert.ok(/^\d+$/.test(values['session-pid'] ?? ''), 'session-pid 必须是压力测试子会话的正整数 PID');
  const sessionPID = Number(values['session-pid']);
  assert.ok(Number.isSafeInteger(sessionPID) && sessionPID > 0, 'session-pid 必须是正整数');
  assert.ok(['hidden', 'visible', 'status', 'covered'].includes(values.state), 'state 只能是 hidden、visible、status、covered');
  const browser = selectBrowser(sessionPID, values.state);
  if (values.state === 'covered') {
    assert.equal(process.ppid, sessionPID, '遮挡辅助必须由指定独立测试会话直接创建，拒绝不明确归属');
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

main().catch(error => { console.error(JSON.stringify({ ok: false, error_message: error.message, fixture_diagnostic: error.fixture_diagnostic })); process.exitCode = 1; });
