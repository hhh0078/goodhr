// 文件作用说明：用正式 Worker 启动配置和隔离的可见 Mac 浏览器验证中文文本、字体矩阵及 GPU 进程状态，不注入页面脚本。
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { brotliCompressSync, gzipSync } from 'node:zlib';

assert.equal(process.platform, 'darwin', '本脚本只验证 macOS');
assert.ok(process.env.CAMOUFOX_INSTALL_DIR?.trim(), '请指定隔离浏览器的 CAMOUFOX_INSTALL_DIR');
const worker = path.resolve(process.env.GOODHR_TEST_WORKER_ROOT ?? fileURLToPath(new URL('../worker', import.meta.url)));
const output = path.resolve(process.env.GOODHR_TEXT_OUTPUT ?? await fs.mkdtemp(path.join(os.tmpdir(), 'goodhr-mac-chinese-evidence-')));
const target = process.env.GOODHR_TEXT_TARGET;
assert.ok(!target || ['windows', 'macos'].includes(target), 'GOODHR_TEXT_TARGET 只能是 windows 或 macos');
assert.ok(!process.env.GOODHR_REUSE_PROFILE || target, '复用隔离 Profile 时请指定 GOODHR_TEXT_TARGET');
const publicURL = process.env.GOODHR_TEXT_PUBLIC_URL;
if (publicURL) assert.ok(['http:', 'https:'].includes(new URL(publicURL).protocol), '公开页必须是 HTTP 或 HTTPS 地址');
await fs.mkdir(output, { recursive: true });

const { FingerprintGenerator } = createRequire(path.join(worker, 'package.json'))('fingerprint-generator');
const { firefox } = await import(pathToFileURL(path.join(worker, 'node_modules/playwright-core/index.mjs')));
const { buildLaunchOptions } = await import(pathToFileURL(path.join(worker, 'dist/browser/session/launch-options.js')));
const sample = '招聘候选人简历你好中文测试，邓云川：人力资源工程师，薪资 20～30 万。';
const fonts = ['sans-serif', 'serif', 'monospace', '"PingFang SC"', '"Microsoft YaHei"', 'SimSun', '"Songti SC"', '"Noto Sans SC"'];
const html = `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><title>中文显示验证</title><style>body{margin:30px;background:white;color:#222}p{font-size:28px;line-height:1.5}small{font:18px sans-serif}</style></head><body><h1>中文显示验证</h1>${fonts.map(font => `<section><small>${font}</small><p style='font-family:${font}'>${sample}</p></section>`).join('')}</body></html>`;
const utf8 = Buffer.from(html);
const gb18030 = execFileSync('iconv', ['-f', 'UTF-8', '-t', 'GB18030'], { input: html.replace('UTF-8', 'GB18030') });
const responses = { '/utf8': utf8, '/gb': gb18030, '/br': brotliCompressSync(utf8), '/gzip': gzipSync(utf8) };
const evidence = { platform: process.platform, arch: process.arch, worker, output, runs: [], requests: [] };

/** serveFixture 返回本机静态中文页面及不同编码响应，不控制页面状态。 */
function serveFixture(request, response) {
  const route = request.url ?? '';
  evidence.requests.push({ route, accept_encoding: request.headers['accept-encoding'] ?? '' });
  const body = responses[route];
  if (!body) {
    response.writeHead(404);
    response.end();
    return;
  }
  const coding = route === '/br' ? 'br' : route === '/gzip' ? 'gzip' : '';
  response.writeHead(200, {
    'Content-Type': `text/html; charset=${route === '/gb' ? 'GB18030' : 'UTF-8'}`,
    'Cache-Control': 'no-store',
    ...(coding ? { 'Content-Encoding': coding } : {}),
  });
  response.end(body);
}

/** isolatedProfile 创建临时 Profile，或仅复用系统临时目录中的既有隔离 Profile。 */
async function isolatedProfile(osTarget) {
  if (!process.env.GOODHR_REUSE_PROFILE) return fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), `goodhr-mac-chinese-${osTarget}-`)));
  const profile = await fs.realpath(path.resolve(process.env.GOODHR_REUSE_PROFILE));
  const temporaryRoots = await Promise.all([fs.realpath(os.tmpdir()), fs.realpath('/tmp')]);
  assert.ok(temporaryRoots.some(root => profile.startsWith(root + path.sep)), '只能复用系统临时目录中的隔离 Profile');
  assert.ok((await fs.stat(profile)).isDirectory(), '隔离 Profile 必须是目录');
  return profile;
}

/** profileFingerprint 读取已有隔离指纹；没有记录时生成指定系统的测试指纹，不改写指纹文件。 */
async function profileFingerprint(profile, osTarget) {
  try {
    const fingerprint = JSON.parse(await fs.readFile(path.join(profile, 'goodhr-fingerprint.json'), 'utf8'));
    assert.equal(typeof fingerprint.navigator?.userAgent, 'string', '隔离 Profile 指纹缺少 UA');
    assert.ok(osTarget === 'windows' ? fingerprint.navigator.userAgent.includes('Windows') : fingerprint.navigator.userAgent.includes('Macintosh'), '已有隔离指纹与 GOODHR_TEXT_TARGET 不一致');
    return { fingerprint, source: 'existing_file' };
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    const fingerprint = new FingerprintGenerator({ browsers: ['firefox'], devices: ['desktop'], operatingSystems: [osTarget] }).getFingerprint().fingerprint;
    return { fingerprint, source: 'generated_fixture' };
  }
}

/** inspectProfileProcesses 精确匹配隔离 Profile 路径，避免把其他浏览器的 GPU 进程算进结果。 */
function inspectProfileProcesses(profile) {
  const escaped = profile.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const profileToken = new RegExp(`(?:^|\\s)${escaped}(?=\\s|$)`);
  const processes = execFileSync('ps', ['-axo', 'command'], { encoding: 'utf8' }).split('\n').filter(line => profileToken.test(line));
  assert.ok(processes.length > 0, '没有找到隔离 Profile 的浏览器进程，无法验证 GPU 状态');
  return { processes, gpu_processes: processes.filter(line => /\s\d+\s+gpu\s*$/.test(line)) };
}

const server = http.createServer(serveFixture);
await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
const address = server.address();
assert.ok(address && typeof address === 'object', '本机静态验证服务器没有启动');
try {
  for (const osTarget of target ? [target] : ['windows', 'macos']) {
    const profile = await isolatedProfile(osTarget);
    const { fingerprint, source } = await profileFingerprint(profile, osTarget);
    const context = { action: 'browser.start', trace_id: 'macos-chinese-smoke', started_at: Date.now() };
    const options = await buildLaunchOptions({ headless: false, locale: 'zh-CN', timezone: 'Asia/Shanghai' }, context, fingerprint);
    const browser = await firefox.launchPersistentContext(profile, { ...options, viewport: { width: 1300, height: 1200 } });
    const run = { target: osTarget, profile, fingerprint_source: source, user_agent: fingerprint.navigator.userAgent, headless: false, pages: [] };
    evidence.runs.push(run);
    try {
      const page = browser.pages()[0] ?? await browser.newPage();
      for (const route of Object.keys(responses)) {
        await page.goto(`http://127.0.0.1:${address.port}${route}`, { waitUntil: 'load' });
        assert.equal(await page.locator('p').count(), fonts.length);
        for (let index = 0; index < fonts.length; index++) assert.equal(await page.locator('p').nth(index).innerText(), sample, `${osTarget} ${route} ${fonts[index]} 文本错误`);
        const screenshot = path.join(output, `${osTarget}-${route.slice(1)}.png`);
        await page.screenshot({ path: screenshot, fullPage: true });
        run.pages.push({ route, fonts, text_ok: true, screenshot });
      }
      if (publicURL) {
        await page.goto(publicURL, { waitUntil: 'domcontentloaded' });
        run.public_page = { url: publicURL, screenshot: path.join(output, `${osTarget}-public.png`) };
        await page.screenshot({ path: run.public_page.screenshot, fullPage: true });
      }
      const processEvidence = inspectProfileProcesses(await fs.realpath(profile));
      run.processes = processEvidence.processes;
      run.gpu_processes = processEvidence.gpu_processes;
      assert.equal(run.gpu_processes.length, 0, '隔离浏览器仍有 GPU 进程，启动前字体修正未生效');
      console.log(JSON.stringify({ target: osTarget, profile, text_assertions: fonts.length * Object.keys(responses).length, screenshots: run.pages.map(item => item.screenshot), gpu_processes: 0 }));
    } finally {
      await browser.close();
    }
  }
  evidence.passed = true;
} finally {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await fs.writeFile(path.join(output, 'evidence.json'), JSON.stringify(evidence, null, 2));
  console.log(JSON.stringify({ evidence: path.join(output, 'evidence.json'), passed: evidence.passed === true }));
}
