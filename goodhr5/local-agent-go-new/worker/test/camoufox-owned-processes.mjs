// 文件作用：仅回收公开浏览器测试已确认的进程后代，以启动时间和执行路径防止误杀复用 PID，不操作窗口或网页。
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { spawnSync } from 'node:child_process';

/** parseProcessTable 读取 PID、父 PID、启动时间和执行路径，不记录其他进程的命令行参数。 */
function parseProcessTable(output) {
  return output.split('\n').flatMap(line => {
    const match = line.match(/^\s*(\d+)\s+(\d+)\s+(\w{3}\s+\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}\s+\d{4})\s+(.+?)\s*$/);
    return match ? [{ pid: Number(match[1]), ppid: Number(match[2]), started: match[3].replace(/\s+/g, ' '), executable: match[4] }] : [];
  });
}

/** processTable 获取只含身份字段的当前进程快照，失败时不猜测归属或执行广泛终止。 */
function processTable() {
  const result = spawnSync('/bin/ps', ['-axo', 'pid=,ppid=,lstart=,comm='], {
    encoding: 'utf8', timeout: 5000, maxBuffer: 8 * 1024 * 1024, env: { ...process.env, LC_ALL: 'C' },
  });
  return result.status === 0 ? parseProcessTable(result.stdout) : [];
}

/** sameProcess 允许已退出父进程的浏览器被系统收养，但拒绝 PID、启动时间或执行路径变化。 */
function sameProcess(current, captured) {
  return Boolean(current && captured && current.pid === captured.pid &&
    current.started === captured.started && current.executable === captured.executable);
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
  if (process.platform === 'win32' || !child?.pid) return;
  const rows = processTable();
  const root = rows.find(row => row.pid === child.pid);
  if (root && (!captured.has(child.pid) || sameProcess(root, captured.get(child.pid))) &&
      root.executable === process.execPath) {
    for (const row of descendants(rows, child.pid)) captured.set(row.pid, row);
  }
  if (cover && captured.has(cover.browserPID)) {
    const row = rows.find(item => item.pid === cover.pid);
    let expected;
    try { expected = fs.realpathSync(cover.binary); } catch { return; }
    const browser = rows.find(item => item.pid === cover.browserPID);
    if (sameProcess(browser, captured.get(cover.browserPID)) && row?.executable === expected) captured.set(row.pid, row);
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
    if (child.exitCode === null && child.signalCode === null) spawnSync('taskkill',
      ['/PID', String(child.pid), '/T', ...(force ? ['/F'] : [])], { windowsHide: true, timeout: 5000 });
    return;
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
}
