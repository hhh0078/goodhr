/* 文件作用：使用 Node.js 内置库生成同版本 Chrome/Firefox 开发目录、ZIP 与未签名临时 XPI。 */
"use strict";
const fs = require("node:fs"), path = require("node:path"), zlib = require("node:zlib"), crypto = require("node:crypto");
const ROOT = path.resolve(__dirname, ".."), OUT = path.join(ROOT, "dist");
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
function manifest(browser) {
  const common = {
    manifest_version: 3, name: "GoodHR 简历助手", version,
    description: "只读采集当前可见候选人，确认后添加到 GoodHR 简历库；不点击、滚动或修改招聘页面。",
    permissions: ["storage"],
    host_permissions: ["https://goodhr.58it.cn/*", "http://127.0.0.1/*", "https://lpt.liepin.com/*", "https://h.liepin.com/*", "https://rd6.zhaopin.com/*", "https://ehire.51job.com/*"],
    icons: { "32": "icons/32.png", "64": "icons/64.png", "128": "icons/128.png" },
    action: { default_title: "GoodHR 简历助手", default_popup: "src/ui.html", default_icon: { "32": "icons/32.png" } },
    content_scripts: [{ matches: ["https://lpt.liepin.com/*", "https://h.liepin.com/*", "https://rd6.zhaopin.com/*", "https://ehire.51job.com/*"], js: ["src/shared.js", "src/extraction.js", "src/content.js"], run_at: "document_idle", all_frames: false }],
    content_security_policy: { extension_pages: "script-src 'self'; object-src 'none';" }
  };
  if (browser === "chrome") return { ...common, minimum_chrome_version: "116", permissions: [...common.permissions, "sidePanel"], background: { service_worker: "src/background.js" }, side_panel: { default_path: "src/ui.html" } };
  return { ...common, background: { scripts: ["src/shared.js", "src/background.js"] }, sidebar_action: { default_title: "GoodHR 简历助手", default_panel: "src/ui.html", default_icon: "icons/32.png" }, browser_specific_settings: { gecko: { id: "resume-assistant@goodhr.58it.cn", strict_min_version: "140.0", data_collection_permissions: { required: ["authenticationInfo", "personallyIdentifyingInfo", "websiteContent", "personalCommunications"] } } } };
}

/** build 清理已确认的插件 dist 目录并输出开发包与校验摘要。 */
function build() {
  if (path.resolve(OUT) !== path.join(ROOT, "dist") || !OUT.startsWith(ROOT + path.sep)) throw new Error("构建目录超出插件范围，停止清理");
  if (!/^\d+\.\d+\.\d+$/.test(version)) throw new Error("版本号必须是三段数字");
  fs.rmSync(OUT, { recursive: true, force: true }); fs.mkdirSync(OUT, { recursive: true });
  const packages = [];
  for (const browser of ["chrome", "firefox"]) {
    const directory = path.join(OUT, browser); fs.mkdirSync(directory, { recursive: true }); fs.cpSync(path.join(ROOT, "src"), path.join(directory, "src"), { recursive: true });
    fs.mkdirSync(path.join(directory, "icons")); for (const size of [32,64,128]) fs.writeFileSync(path.join(directory, "icons", `${size}.png`), iconPNG(size));
    fs.writeFileSync(path.join(directory, "manifest.json"), JSON.stringify(manifest(browser), null, 2) + "\n");
    fs.copyFileSync(path.join(ROOT, "INSTALL.md"), path.join(directory, "INSTALL.md"));
    const archive = zipBuffer(filesUnder(directory)), name = `goodhr-resume-${browser}-${version}.zip`; fs.writeFileSync(path.join(OUT, name), archive);
    packages.push({ browser, file: name, bytes: archive.length, sha256: crypto.createHash("sha256").update(archive).digest("hex"), signed: false });
    if (browser === "firefox") { const xpi = `goodhr-resume-firefox-${version}-unsigned.xpi`; fs.writeFileSync(path.join(OUT, xpi), archive); packages.push({ browser, file: xpi, bytes: archive.length, sha256: crypto.createHash("sha256").update(archive).digest("hex"), signed: false, installation: "temporary-only" }); }
  }
  fs.writeFileSync(path.join(OUT, "checksums.json"), JSON.stringify({ description: "文件作用：记录同版本开发包的体积、SHA256 和未签名状态。", version, packages }, null, 2) + "\n");
  console.log(JSON.stringify({ version, packages }, null, 2));
}

if (require.main === module) build();
module.exports = { manifest, zipBuffer, crc32, build };
