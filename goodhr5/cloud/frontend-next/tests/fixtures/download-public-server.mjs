/** 文件作用说明：为下载页回归提供合成公开配置，不读取生产账号，也不访问真实安装包。 */
import { createServer } from "node:http";

const port = Number(process.env.DOWNLOAD_FIXTURE_PORT || 5239);
const localAgent = Array.from({ length: 40 }, (_, index) => ({
  version: `synthetic-${40 - index}`, note: `合成更新记录 ${40 - index}`,
  url_win: index === 0 ? "https://example.invalid/goodhr-windows.zip" : "",
  url_mac: index === 1 ? "https://example.invalid/goodhr-mac.zip" : "", sha256: "",
}));
let firefoxReleaseMode = "signed";

/** browserExtensionRelease 根据测试模式返回已签名包或应被隐藏的未签名包。 */
function browserExtensionRelease() {
  const firefoxSigned = firefoxReleaseMode === "signed";
  return {
    version: "1.0.3",
    note: "合成插件更新说明",
    url_chrome: "/downloads/goodhr-extension-chrome-1.0.3.zip",
    sha256_chrome: "",
    url_firefox: firefoxSigned ? "https://downloads.example.invalid/goodhr-extension-firefox-1.0.3.xpi" : "https://downloads.example.invalid/goodhr-extension-firefox-1.0.3.zip",
    sha256_firefox: firefoxSigned ? "a".repeat(64) : "",
    firefox_signed: firefoxSigned,
  };
}

/** servePublicFixture 只返回无身份信息的下载配置和公开统计。 */
function servePublicFixture(request, response) {
  const path = new URL(request.url, `http://127.0.0.1:${port}`).pathname;
  if (path === "/__test__/firefox-release") {
    firefoxReleaseMode = new URL(request.url, `http://127.0.0.1:${port}`).searchParams.get("mode") === "unsigned" ? "unsigned" : "signed";
    response.writeHead(204);
    response.end();
    return;
  }
  const data = path === "/api/system/local-agent-updates" ? { local_agent: localAgent }
    : path === "/api/system/browser-extension" ? { release: browserExtensionRelease() }
    : path === "/api/public/stats/today" ? { processed_resume_count: 0, today_registered_count: 0 } : {};
  response.writeHead(200, { "Content-Type": "application/json" });
  response.end(JSON.stringify(data));
}

createServer(servePublicFixture).listen(port, "127.0.0.1", () => process.stdout.write(`download fixture ready ${port}\n`));
