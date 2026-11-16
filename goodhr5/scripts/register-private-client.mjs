/** 文件作用：校验私有安装包的服务身份与 SHA256，将 Windows/Mac 安装包登记到内网下载目录。 */
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

/** registerClient 只登记明确由私有打包器生成、服务地址一致的安装包，不读取或上传任何业务数据。 */
export function registerClient(environmentFile, installer, buildInfo, target, version) {
  const project = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const environment = fs.readFileSync(environmentFile, "utf8");
  const configured = environment.match(/^GOODHR_PUBLIC_BASE_URL=(.+)$/m)?.[1]?.trim();
  const base = new URL(configured || "");
  if (base.protocol !== "https:" || base.username || base.password || base.pathname !== "/" || base.search || base.hash) throw new Error("请配置有效的内网 HTTPS 服务地址");
  if (!["win", "mac"].includes(target) || !/^\d+(?:\.\d+)*$/.test(version)) throw new Error("平台为 win 或 mac，版本号只包含数字和点");
  if (path.extname(installer).toLowerCase() !== (target === "mac" ? ".pkg" : ".exe")) throw new Error("请选择对应平台的安装器");
  const identity = JSON.parse(fs.readFileSync(buildInfo, "utf8").replace(/^\uFEFF/, ""));
  if (identity.environment !== "private" || identity.version !== version || new URL(identity.public_base_url).origin !== base.origin) throw new Error("安装包构建身份与私有环境不一致，请使用本轮私有构建的 build-info.json");
  const bytes = fs.readFileSync(installer);
  if (!bytes.length) throw new Error("安装包不能为空");
  const digest = crypto.createHash("sha256").update(bytes).digest("hex");
  const directory = path.join(project, "private-data", "assets");
  fs.mkdirSync(directory, { recursive: true });
  const name = `${digest}${target === "mac" ? ".pkg" : ".exe"}`;
  fs.copyFileSync(installer, path.join(directory, name));
  const manifestPath = path.join(directory, "clients.json");
  const previous = fs.existsSync(manifestPath) ? JSON.parse(fs.readFileSync(manifestPath, "utf8")) : [];
  if (!Array.isArray(previous)) throw new Error("已有安装包清单格式不正确");
  const release = { version, url_win: "", url_mac: "", [`url_${target}`]: `${base.origin}/downloads/private/${name}`, sha256: digest, note: target === "mac" ? "私有部署 macOS Apple/Intel 通用安装包" : "私有部署 Windows x64 安装包" };
  const result = [release, ...previous.filter(item => !item?.[`url_${target}`])];
  fs.writeFileSync(manifestPath+".tmp", JSON.stringify(result, null, 2)+"\n");
  fs.renameSync(manifestPath+".tmp", manifestPath);
  return release;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv.length !== 7) throw new Error("用法：node scripts/register-private-client.mjs 环境文件 安装器 build-info.json win或mac 版本号");
    const release = registerClient(...process.argv.slice(2));
    console.log(`已登记 ${release.version}，重启私有后端后下载页生效。`);
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
