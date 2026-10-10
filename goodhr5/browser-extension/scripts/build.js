/* 文件作用：用内置库生成 Chrome/Firefox 开发包，支持 Chrome 单独发布和隔离的固定本地测试构建。 */
"use strict";
const fs = require("node:fs"), path = require("node:path"), zlib = require("node:zlib"), crypto = require("node:crypto");
const ROOT = path.resolve(__dirname, "..");
const LOCAL = Object.freeze({ api: "http://127.0.0.1:8084", web: "http://127.0.0.1:5173", agentPort: 43139, name: "GoodHR 简历助手（本地测试）" });
const { version } = JSON.parse(fs.readFileSync(path.join(ROOT, "package.json"), "utf8"));

/** crc32 计算 ZIP/PNG 标准校验值，不依赖外部包。 */
function crc32(buffer) { let crc = 0xffffffff; for (const byte of buffer) { crc ^= byte; for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0); } return (crc ^ 0xffffffff) >>> 0; }

/** pngChunk 构建有长度和校验值的 PNG 数据块。 */
function pngChunk(type, content) { const name = Buffer.from(type); const length = Buffer.alloc(4); length.writeUInt32BE(content.length); const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(Buffer.concat([name, content]))); return Buffer.concat([length, name, content, crc]); }

/** iconPNG 生成自然金色简历卡图标，所有内容在本地生成。 */
function iconPNG(size) {
  const header = Buffer.alloc(13); header.writeUInt32BE(size); header.writeUInt32BE(size, 4); header[8] = 8; header[9] = 6;
  const pixels = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) {
    const offset = y * (size * 4 + 1) + 1 + x * 4;
    const card = x > size * .22 && x < size * .77 && y > size * .15 && y < size * .86;
    const line = card && x > size * .32 && x < size * .65 && [0.36, .52, .68].some((point) => Math.abs(y / size - point) < .035);
    const color = line ? [138, 103, 33] : card ? [255, 255, 249] : [138, 103, 33];
    pixels[offset] = color[0]; pixels[offset + 1] = color[1]; pixels[offset + 2] = color[2]; pixels[offset + 3] = 255;
  }
  return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]), pngChunk("IHDR", header), pngChunk("IDAT", zlib.deflateSync(pixels)), pngChunk("IEND", Buffer.alloc(0))]);
}

/** filesUnder 按固定顺序枚举发布目录文件，排除目录和操作系统残留。 */
function filesUnder(directory, prefix = "") { return fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name)).flatMap((entry) => { const relative = path.posix.join(prefix, entry.name); return entry.isDirectory() ? filesUnder(path.join(directory, entry.name), relative) : [{ name: relative, content: fs.readFileSync(path.join(directory, entry.name)) }]; }); }

/** zipBuffer 生成确定性 UTF-8 ZIP，Firefox XPI 使用同一标准 ZIP 格式。 */
function zipBuffer(files) {
  const locals = [], centrals = []; let offset = 0;
  for (const file of files) {
    const name = Buffer.from(file.name), content = zlib.deflateRawSync(file.content), crc = crc32(file.content);
    const local = Buffer.alloc(30); local.writeUInt32LE(0x04034b50); local.writeUInt16LE(20, 4); local.writeUInt16LE(0x0800, 6); local.writeUInt16LE(8, 8); local.writeUInt16LE(23882, 12); local.writeUInt32LE(crc, 14); local.writeUInt32LE(content.length, 18); local.writeUInt32LE(file.content.length, 22); local.writeUInt16LE(name.length, 26);
    locals.push(local, name, content);
    const central = Buffer.alloc(46); central.writeUInt32LE(0x02014b50); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6); central.writeUInt16LE(0x0800, 8); central.writeUInt16LE(8, 10); central.writeUInt16LE(23882, 14); central.writeUInt32LE(crc, 16); central.writeUInt32LE(content.length, 20); central.writeUInt32LE(file.content.length, 24); central.writeUInt16LE(name.length, 28); central.writeUInt32LE(offset, 42); centrals.push(central, name);
    offset += local.length + name.length + content.length;
  }
  const directory = Buffer.concat(centrals), end = Buffer.alloc(22); end.writeUInt32LE(0x06054b50); end.writeUInt16LE(files.length, 8); end.writeUInt16LE(files.length, 10); end.writeUInt32LE(directory.length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** manifest 为两种浏览器分别生成正确的 MV3 后台和原生侧栏清单。 */
function manifest(browser, local = false) {
  const common = {
    manifest_version: 3, name: "GoodHR 简历助手", version,
    description: "从当前可见候选人读取资料，在隔离悬浮助手中确认后入库；不点击、解锁或滚动招聘页面。",
    permissions: ["storage"],
    host_permissions: ["https://goodhr.58it.cn/*", "http://127.0.0.1/*", "https://lpt.liepin.com/*", "https://h.liepin.com/*", "https://rd6.zhaopin.com/*", "https://ehire.51job.com/*", "https://*.lietou-static.com/*", "https://mypics.zhaopin.cn/*"],
    icons: { "32": "icons/32.png", "64": "icons/64.png", "128": "icons/128.png" },
    action: { default_title: "GoodHR 简历助手", default_popup: "src/ui.html", default_icon: { "32": "icons/32.png" } },
    content_scripts: [{ matches: ["https://lpt.liepin.com/*", "https://h.liepin.com/*", "https://rd6.zhaopin.com/*", "https://ehire.51job.com/*"], js: ["src/shared.js", "src/extraction.js", "src/floating.js", "src/content.js"], run_at: "document_idle", all_frames: false }],
    web_accessible_resources: [{ resources: ["src/ui.html", "src/ui.css", "src/ui.js", "src/shared.js", "src/privacy.html"], matches: ["https://lpt.liepin.com/*", "https://h.liepin.com/*", "https://rd6.zhaopin.com/*", "https://ehire.51job.com/*"] }],
    content_security_policy: { extension_pages: "script-src 'self'; object-src 'none';" }
  };
  if (local) {
    common.name = LOCAL.name;
    common.description = "本地测试：简历仅保存到 127.0.0.1:8084 的测试服务；只读招聘页面，不操作页面。";
    common.host_permissions = common.host_permissions.filter((host) => host !== "https://goodhr.58it.cn/*");
    common.action.default_title = LOCAL.name;
  }
  if (browser === "chrome") return { ...common, minimum_chrome_version: "116", permissions: [...common.permissions, "sidePanel"], background: { service_worker: "src/background.js" }, side_panel: { default_path: "src/ui.html" } };
  return { ...common, background: { scripts: ["src/shared.js", "src/avatar.js", "src/background.js"] }, sidebar_action: { default_title: common.name, default_panel: "src/ui.html", default_icon: "icons/32.png" }, browser_specific_settings: { gecko: { id: local ? "resume-assistant-local@goodhr.local" : "resume-assistant@goodhr.58it.cn", strict_min_version: "140.0", data_collection_permissions: { required: ["authenticationInfo", "personallyIdentifyingInfo", "websiteContent", "personalCommunications"] } } } };
}

/** replaceRequired 替换已知构建模板，源模板变化时停止，避免静默留下生产地址。 */
function replaceRequired(content, before, after, filename) {
  if (!content.includes(before)) throw new Error(`${filename} 本地构建模板已变化，请先核对固定地址`);
  return content.replaceAll(before, after);
}

/** prepareLocalOutput 仅修改复制后的本地包，生产源码、协议和正式包不会被改写。 */
function prepareLocalOutput(directory) {
  /** read 读取本次已复制的构建文件，不能写入正式源码目录。 */
  const read = (name) => fs.readFileSync(path.join(directory, "src", name), "utf8");
  /** write 将本地地址和提示写入本次测试产物。 */
  const write = (name, content) => fs.writeFileSync(path.join(directory, "src", name), content);
  write("shared.js", replaceRequired(read("shared.js"), 'const CLOUD_BASE = "https://goodhr.58it.cn";', `const CLOUD_BASE = ${JSON.stringify(LOCAL.api)};`, "shared.js"));
  write("ui.js", replaceRequired(read("ui.js"), "shared.CLOUD_BASE", JSON.stringify(LOCAL.web), "ui.js"));
  let background = replaceRequired(read("background.js"), '[shared.CLOUD_BASE, "http://127.0.0.1:5173"]', `[${JSON.stringify(LOCAL.web)}]`, "background.js");
  background = replaceRequired(background, 'shared.CLOUD_BASE + "/download#browser-extension"', `${JSON.stringify(LOCAL.web)} + "/download#browser-extension"`, "background.js");
  background = replaceRequired(background, "43129", String(LOCAL.agentPort), "background.js").replaceAll("GoodHR 简历助手", LOCAL.name);
  write("background.js", background);
  let ui = replaceRequired(read("ui.html"), "https://goodhr.58it.cn/login", LOCAL.web + "/login", "ui.html");
  ui = replaceRequired(ui, 'value="43129"', `value="${LOCAL.agentPort}"`, "ui.html").replaceAll("GoodHR 简历助手", LOCAL.name);
  ui = replaceRequired(ui, "  <main>", `  <main>\n    <p class="error"><strong>本地测试</strong> · 简历仅入测试库<br>API：${LOCAL.api}<br>网页：${LOCAL.web} · 本地程序：${LOCAL.agentPort}</p>`, "ui.html");
  write("ui.html", ui);
  let privacy = read("privacy.html").replaceAll("GoodHR 简历助手", LOCAL.name).replace("<title>GoodHR 插件数据说明</title>", "<title>GoodHR 本地测试插件数据说明</title>");
  privacy = replaceRequired(privacy, '<p>业务和公开版本检查仅连接 <a href="https://goodhr.58it.cn" target="_blank" rel="noreferrer">goodhr.58it.cn</a>；配对和摘要任务只连接127.0.0.1。头像图片域见上方说明；没有广告追踪或第三方分析服务。未登录的重新读取不向云端发送候选人资料。</p>', `<p><strong>此包仅用于本地测试。</strong>业务与版本检查仅连接 ${LOCAL.api}；候选人详情、登录协议和下载入口仅打开 <a href="${LOCAL.web}" target="_blank" rel="noreferrer">${LOCAL.web}</a>。不会请求生产 GoodHR 服务。配对和摘要默认连接 127.0.0.1:${LOCAL.agentPort}，请使用独立测试程序；招聘页面与头像白名单保持原样。未登录重新读取不会发送候选人资料。若主动生成AI总结，职业资料仍会交给测试程序所配置的AI服务商，可能计费。</p>`, "privacy.html");
  write("privacy.html", privacy);
}

/** parseBuildOptions 只允许标准、Chrome单独和固定本地模式，不接受自定义地址或环境覆盖。 */
function parseBuildOptions(argv) {
  if (!argv.length) return { local: false };
  if (argv.length === 1 && argv[0] === "--local") return { local: true };
  if (argv.length === 1 && argv[0] === "--chrome") return { local: false, chromeOnly: true };
  throw new Error("仅支持 npm run build、build:chrome 或 build:local；本地 API 与网页地址固定，不接受自定义 URL");
}

/** build 清理已确认的插件 dist 或 dist-local 目录，分别输出正式开发包与本地测试包。 */
function build(options = {}) {
  if (Object.keys(options).some((key) => !["local", "chromeOnly", "test"].includes(key)) || Object.values(options).some((value) => typeof value !== "boolean")) throw new Error("构建参数仅支持布尔值 local/chromeOnly/test");
  if (options.test && !options.local) throw new Error("隔离测试输出仅用于固定本地模式");
  const local = options.local === true, outputName = options.test ? path.join("artifacts", "build-local-test") : local ? "dist-local" : "dist", OUT = path.join(ROOT, outputName);
  if (path.resolve(OUT) !== path.join(ROOT, outputName) || !OUT.startsWith(ROOT + path.sep)) throw new Error("构建目录超出插件范围，停止清理");
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error("版本号必须是三段数字");
  if (!options.chromeOnly) fs.rmSync(OUT, { recursive: true, force: true }); fs.mkdirSync(OUT, { recursive: true });
  const packages = [];
  for (const browser of options.chromeOnly ? ["chrome"] : ["chrome", "firefox"]) {
    const directory = path.join(OUT, browser); if (options.chromeOnly) fs.rmSync(directory, { recursive: true, force: true }); fs.mkdirSync(directory, { recursive: true }); fs.cpSync(path.join(ROOT, "src"), path.join(directory, "src"), { recursive: true });
    if (local) prepareLocalOutput(directory);
    fs.mkdirSync(path.join(directory, "icons")); for (const size of [32,64,128]) fs.writeFileSync(path.join(directory, "icons", `${size}.png`), iconPNG(size));
    fs.writeFileSync(path.join(directory, "manifest.json"), JSON.stringify(manifest(browser, local), null, 2) + "\n");
    fs.copyFileSync(path.join(ROOT, local ? "LOCAL-INSTALL.md" : "INSTALL.md"), path.join(directory, "INSTALL.md"));
    const archive = zipBuffer(filesUnder(directory)), name = `goodhr-resume-${browser}-${local ? "local-" : ""}${version}.zip`; fs.writeFileSync(path.join(OUT, name), archive);
    packages.push({ browser, file: name, bytes: archive.length, sha256: crypto.createHash("sha256").update(archive).digest("hex"), signed: false });
    if (browser === "firefox") { const xpi = `goodhr-resume-firefox-${local ? "local-" : ""}${version}-unsigned.xpi`; fs.writeFileSync(path.join(OUT, xpi), archive); packages.push({ browser, file: xpi, bytes: archive.length, sha256: crypto.createHash("sha256").update(archive).digest("hex"), signed: false, installation: "temporary-only" }); }
  }
  fs.writeFileSync(path.join(OUT, "checksums.json"), JSON.stringify({ description: "文件作用：记录同版本开发包的体积、SHA256 和未签名状态。", version, packages }, null, 2) + "\n");
  console.log(JSON.stringify({ version, packages }, null, 2));
  return { version, packages };
}

if (require.main === module) build(parseBuildOptions(process.argv.slice(2)));
module.exports = { manifest, zipBuffer, crc32, iconPNG, build, parseBuildOptions };
