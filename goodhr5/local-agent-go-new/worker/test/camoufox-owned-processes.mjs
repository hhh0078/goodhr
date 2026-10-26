// 文件作用：仅回收公开浏览器测试已确认的进程后代，以启动时间和执行路径防止误杀复用 PID，不操作窗口或网页。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { spawnSync } from 'node:child_process';

const WINDOWS_TABLE = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$rows = @(Get-CimInstance -ClassName Win32_Process | Where-Object { $_.ExecutablePath -and $_.CreationDate } | ForEach-Object {
  @{ pid = [int]$_.ProcessId; ppid = [int]$_.ParentProcessId;
     started = $_.CreationDate.ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss.fffZ'); executable = [string]$_.ExecutablePath }
})
ConvertTo-Json -InputObject $rows -Compress
`;
const WINDOWS_TERMINATE = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
# executableIdentity 仅规范化 Windows 大小写、分隔符和长路径前缀，不按进程名称匹配。
function executableIdentity([string]$value) {
  $value = $value.Replace('/', '\\')
  if ($value.StartsWith('\\\\?\\')) { $value = $value.Substring(4) }
  return $value.ToUpperInvariant()
}
$targets = @(ConvertFrom-Json -InputObject $env:GOODHR_TEST_CLEANUP_TARGETS)
$results = @(foreach ($target in $targets) {
  $opened = $null
  try {
    $opened = [System.Diagnostics.Process]::GetProcessById([int]$target.pid)
    # 缓存系统进程句柄后检查身份，后续 Kill 针对该对象，避免只凭后来复用的 PID 操作。
    $null = $opened.Handle
    if ($opened.HasExited) { @{ pid = $target.pid; status = 'missing' }; continue }
    $started = $opened.StartTime.ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss.fffZ')
    $executable = $opened.MainModule.FileName
    if ($started -ne $target.started -or (executableIdentity $executable) -ne (executableIdentity $target.executable)) {
      @{ pid = $target.pid; status = 'identity_changed' }; continue
    }
    # Windows 没有 POSIX 信号；始终终止已经核对并缓存句柄的测试对象，不向复用 PID 的窗口发送消息。
    $opened.Kill(); @{ pid = $target.pid; status = 'killed'; force_requested = ($env:GOODHR_TEST_CLEANUP_FORCE -eq 'true') }
  } catch [System.ArgumentException] {
    @{ pid = $target.pid; status = 'missing' }
  } catch {
    $failureType = $_.Exception.GetType().FullName
    $gone = $false
    if ($null -ne $opened) { try { $gone = $opened.HasExited } catch {} }
    if ($gone) { @{ pid = $target.pid; status = 'missing' } }
    else { @{ pid = $target.pid; status = 'failed'; error_type = $failureType } }
  } finally { if ($null -ne $opened) { $opened.Dispose() } }
})
ConvertTo-Json -InputObject $results -Compress
`;
let windowsQueryError;

const WINDOWS_ARGUMENTS = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$targetPID = [int]$env:GOODHR_TEST_PROCESS_ARGUMENT_PID
$row = Get-CimInstance -ClassName Win32_Process -Filter ('ProcessId = ' + $targetPID)
@{ pid = [int]$row.ProcessId; command = [string]$row.CommandLine } | ConvertTo-Json -Compress
`;

/** windowsPowerShell 使用固定脚本和独立环境参数，不经 shell 拼接路径或 PID。 */
function windowsPowerShell(script, environment = {}) {
  return spawnSync('powershell.exe', ['-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', Buffer.from(script, 'utf16le').toString('base64')], {
    encoding: 'utf8', timeout: 15000, maxBuffer: 8 * 1024 * 1024, windowsHide: true, env: { ...process.env, ...environment },
  });
}

/** parseWindowsRows 校验 CIM 返回的身份字段，缺失或受保护的进程绝不进入回收列表。 */
function parseWindowsRows(output) {
  const decoded = JSON.parse(output.trim().replace(/^\uFEFF/, ''));
  return (Array.isArray(decoded) ? decoded : [decoded]).filter(row => row && Number.isSafeInteger(row.pid) && row.pid > 0 &&
    Number.isSafeInteger(row.ppid) && row.ppid >= 0 && typeof row.started === 'string' && Number.isFinite(Date.parse(row.started)) &&
    typeof row.executable === 'string' && row.executable.length > 0);
}

/** executableIdentity 在 Windows 按大小写无关路径比较，Mac 保留现有精确路径规则。 */
function executableIdentity(value, platform = process.platform) {
  if (platform !== 'win32') return value;
  return value.replaceAll('/', '\\').replace(/^\\\\\?\\/, '').toLowerCase();
}

/** readProcessIdentities 公开只读的进程身份快照，供独立窗口夹具复用同一平台实现。 */
export function readProcessIdentities() { return processTable(); }

/** readProcessArguments 只读取指定已核对进程的参数，不保存其他进程命令行。 */
export function readProcessArguments(pid) {
  assert.ok(Number.isSafeInteger(pid) && pid > 0, '只允许读取正整数 PID 的参数');
  if (process.platform === 'win32') {
    const result = windowsPowerShell(WINDOWS_ARGUMENTS, { GOODHR_TEST_PROCESS_ARGUMENT_PID: String(pid) });
    if (result.status !== 0) throw new Error('指定测试进程参数无法读取');
    const row = JSON.parse(result.stdout.trim().replace(/^\uFEFF/, ''));
    assert.equal(row.pid, pid, '指定测试进程已经退出');
    return row.command;
  }
  const result = spawnSync('/bin/ps', ['-p', String(pid), '-o', 'args='], { encoding: 'utf8', timeout: 5000, maxBuffer: 1024 * 1024 });
  if (result.status !== 0) throw new Error('指定测试进程参数无法读取');
  return result.stdout.trim();
}

/** exactArgument 按完整参数边界核对脚本路径，不接受同名前缀或备份文件。 */
function exactArgument(command, argument) {
  const escaped = argument.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replaceAll('\\\\', '[\\\\/]');
  return new RegExp(`(?:^|\\s|["'])${escaped}(?=\\s|["']|$)`, 'i').test(command);
}

/** parseProcessTable 读取 PID、父 PID、启动时间和执行路径，不记录其他进程的命令行参数。 */
function parseProcessTable(output) {
  return output.split('\n').flatMap(line => {
    const match = line.match(/^\s*(\d+)\s+(\d+)\s+(\w{3}\s+\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}\s+\d{4})\s+(.+?)\s*$/);
    return match ? [{ pid: Number(match[1]), ppid: Number(match[2]), started: match[3].replace(/\s+/g, ' '), executable: match[4] }] : [];
  });
}

/** processTable 获取只含身份字段的当前进程快照，失败时不猜测归属或执行广泛终止。 */
function processTable() {
  if (process.platform === 'win32') {
    const result = windowsPowerShell(WINDOWS_TABLE);
    try {
      if (result.status !== 0) throw new Error(result.error?.code ?? `CIM_EXIT_${result.status}`);
      const rows = parseWindowsRows(result.stdout);
      windowsQueryError = undefined;
      return rows;
    } catch (error) { windowsQueryError = error.message; return []; }
  }
  const result = spawnSync('/bin/ps', ['-axo', 'pid=,ppid=,lstart=,comm='], {
    encoding: 'utf8', timeout: 5000, maxBuffer: 8 * 1024 * 1024, env: { ...process.env, LC_ALL: 'C' },
  });
  return result.status === 0 ? parseProcessTable(result.stdout) : [];
}

/** sameProcess 允许已退出父进程的浏览器被系统收养，但拒绝 PID、启动时间或执行路径变化。 */
function sameProcess(current, captured, platform = process.platform) {
  return Boolean(current && captured && current.pid === captured.pid &&
    current.started === captured.started && executableIdentity(current.executable, platform) === executableIdentity(captured.executable, platform));
}

/** descendants 包括会话本身并递归寻找其后代，独立进程组不会影响父子关系判断。 */
function descendants(rows, parentPID) {
  const owned = new Set([parentPID]);
  for (let changed = true; changed;) {
    changed = false;
    for (const row of rows) if (owned.has(row.ppid) && !owned.has(row.pid)) { owned.add(row.pid); changed = true; }
  }
  return rows.filter(row => owned.has(row.pid));
}

/** captureOwnedProcesses 在启动和终止前合并本会话后代，遮挡夹具还须匹配已确认的浏览器和明确执行路径。 */
export function captureOwnedProcesses(child, captured, cover) {
  if (!child?.pid) return;
  const rows = processTable();
  const root = rows.find(row => row.pid === child.pid);
  if (root && (!captured.has(child.pid) || sameProcess(root, captured.get(child.pid))) &&
      executableIdentity(root.executable) === executableIdentity(process.execPath)) {
    for (const row of descendants(rows, child.pid)) captured.set(row.pid, row);
  }
  if (cover && captured.has(cover.browserPID)) {
    const row = rows.find(item => item.pid === cover.pid);
    let expected;
    try { expected = fs.realpathSync(cover.binary); } catch { return; }
    const browser = rows.find(item => item.pid === cover.browserPID);
    if (!sameProcess(browser, captured.get(cover.browserPID))) return;
    if (process.platform === 'win32') {
      if (!row || cover.sessionPID !== child.pid || row.ppid !== cover.helperPID || !cover.executable ||
          executableIdentity(row.executable) !== executableIdentity(cover.executable)) return;
      try {
        const command = readProcessArguments(row.pid);
        if (exactArgument(command, expected) && new RegExp(`(?:^|\\s)-BrowserPid\\s+${cover.browserPID}(?:\\s|$)`, 'i').test(command)) captured.set(row.pid, row);
      } catch { /* 证据缺失时不把 powershell.exe 当作可回收夹具。 */ }
    } else if (row?.executable === expected) captured.set(row.pid, row);
  }
}

/** verifiedProcesses 只返回仍匹配历史身份的已捕获进程，不把同 PID 的后来进程当成测试后代。 */
function verifiedProcesses(rows, captured) {
  return rows.filter(row => sameProcess(row, captured.get(row.pid))).reverse();
}

/** terminateOwnedProcesses 按已确认身份逐个终止，包括 Playwright 创建的独立浏览器进程组；不使用全局名称匹配。 */
export function terminateOwnedProcesses(child, captured, force = false) {
  if (!child?.pid) return;
  if (process.platform === 'win32') {
    const targets = verifiedProcesses(processTable(), captured);
    if (windowsQueryError || captured.size === 0) return { platform: 'win32', status: 'failed', confirmed_targets: 0,
      error_message: windowsQueryError ?? '未确认测试进程身份，未执行回收' };
    const result = windowsPowerShell(WINDOWS_TERMINATE, { GOODHR_TEST_CLEANUP_TARGETS: JSON.stringify(targets), GOODHR_TEST_CLEANUP_FORCE: String(force) });
    try {
      if (result.status !== 0) throw new Error(result.error?.code ?? `CLEANUP_EXIT_${result.status}`);
      const decoded = JSON.parse(result.stdout.trim().replace(/^\uFEFF/, ''));
      const outcomes = Array.isArray(decoded) ? decoded : [decoded];
      return { platform: 'win32', status: outcomes.some(item => item.status === 'failed') ? 'failed' : 'passed', confirmed_targets: targets.length, outcomes };
    } catch (error) { return { platform: 'win32', status: 'failed', confirmed_targets: targets.length, error_message: error.message }; }
  }
  const signal = force ? 'SIGKILL' : 'SIGTERM';
  const current = verifiedProcesses(processTable(), captured);
  // 首先通知 Node 自己清理，再通知其已确认的独立浏览器和其他测试后代。
  const root = current.find(row => row.pid === child.pid);
  const ordered = root ? [root, ...current.filter(row => row.pid !== child.pid)] : current;
  for (const row of ordered) {
    try { process.kill(row.pid, signal); } catch (error) { if (error.code !== 'ESRCH') throw error; }
  }
}

/** processCleanupSelfTest 仅用伪造表格验证后代和身份边界，不读取系统进程或发送信号。 */
export function processCleanupSelfTest() {
  const rows = parseProcessTable('100 1 Mon Oct 26 18:00:00 2026 /opt/node\n101 100 Mon Oct 26 18:00:01 2026 /tmp/camoufox-upgrade/Camoufox\n102 101 Mon Oct 26 18:00:02 2026 /tmp/helper\n200 1 Mon Oct 26 18:00:03 2026 /tmp/other\n');
  const owned = descendants(rows, 100);
  assert.deepEqual(owned.map(row => row.pid), [100, 101, 102]);
  const captured = new Map(owned.map(row => [row.pid, row]));
  assert.deepEqual(verifiedProcesses(rows, captured).map(row => row.pid), [102, 101, 100]);
  assert.equal(sameProcess({ ...rows[1], ppid: 1 }, rows[1]), true);
  assert.equal(sameProcess({ ...rows[1], started: 'Mon Oct 26 19:00:00 2026' }, rows[1]), false);
  assert.equal(sameProcess({ ...rows[1], executable: '/Applications/user-browser' }, rows[1]), false);
  assert.deepEqual(verifiedProcesses([{ ...rows[1], started: 'later' }, rows[3]], captured), []);
  const windows = parseWindowsRows(JSON.stringify([{ pid: 100, ppid: 1, started: '2026-10-26T10:00:00.123Z', executable: 'C:\\Test Space\\node.exe' },
    { pid: 101, ppid: 100, started: '2026-10-26T10:00:00.124Z', executable: 'C:\\camoufox-upgrade\\camoufox.exe' },
    { pid: 200, ppid: 1, started: null, executable: null }]));
  assert.equal(windows.length, 2);
  assert.deepEqual(descendants(windows, 100).map(row => row.pid), [100, 101]);
  assert.equal(sameProcess({ ...windows[0], executable: 'c:/test space/NODE.EXE' }, windows[0], 'win32'), true);
  assert.equal(sameProcess({ ...windows[0], executable: '\\\\?\\C:\\Test Space\\node.exe' }, windows[0], 'win32'), true);
  assert.equal(sameProcess({ ...windows[0], started: '2026-10-26T10:00:00.999Z' }, windows[0], 'win32'), false);
  assert.throws(() => parseWindowsRows('not json'));
  assert.equal(exactArgument('powershell.exe -File "C:\\Public Test\\cover.ps1" -BrowserPid 101', 'C:\\Public Test\\cover.ps1'), true);
  assert.equal(exactArgument('powershell.exe -File "C:\\Public Test\\cover.ps1.bak" -BrowserPid 101', 'C:\\Public Test\\cover.ps1'), false);
}
