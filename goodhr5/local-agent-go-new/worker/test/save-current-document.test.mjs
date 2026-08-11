// 文件作用说明：验证当前文档页通过 BrowserContext 请求保存时的上下文复用、大小上限、跳转、超时和文件签名。

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { BrowserSession } from "../dist/browser/session/browser-session.js";
import { parseSaveCurrentDocumentRequest } from "../dist/validation/action-requests.js";

/** createAPIResponse 创建可记录释放状态的最小 Playwright APIResponse。 */
function createAPIResponse(options = {}) {
  const state = options.state ?? { disposed: 0 };
  return {
    status: () => options.status ?? 200,
    headers: () => options.headers ?? { "content-type": "application/pdf" },
    url: () => options.url ?? "https://example.com/resume.pdf",
    async body() {
      if (options.bodyDelayMS) {
        await new Promise((resolve) => setTimeout(resolve, options.bodyDelayMS));
      }
      return Buffer.from(options.body ?? "%PDF-1.7\nGoodHR");
    },
    async dispose() {
      state.disposed += 1;
    },
  };
}

/** createSession 创建指向当前文档页并记录 BrowserContext 请求参数的最小会话。 */
async function createSession(pageURL, requestHandler) {
  const logger = { info() {}, warn() {}, error() {}, failure() {} };
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-document-"));
  const requests = [];
  const session = new BrowserSession(logger);
  session.currentPage = { isClosed: () => false, url: () => pageURL };
  session.context = {
    request: {
      async get(url, options) {
        requests.push({ url, options });
        if (requestHandler) {
          return requestHandler(url, options);
        }
        return createAPIResponse({ url });
      },
    },
  };
  await session.downloadManager.prepare(directory);
  return { directory, requests, session };
}

/** saveCurrentDocument 调用当前文档保存能力并复用统一请求参数。 */
function saveCurrentDocument(session, overrides = {}) {
  return session.saveCurrentDocument(
    {
      max_bytes: 1024,
      allowed_content_types: ["application/pdf"],
      suggested_filename: "../黄明超:简历",
      timeout_ms: 3000,
      ...overrides,
    },
    {
      trace_id: "trace-save",
      action: "page.save_current_document",
      started_at: Date.now(),
    },
  );
}

/** 验证文档保存请求保持强类型字段并拒绝越界大小。 */
test("当前文档保存请求校验大小和内容类型", () => {
  const request = parseSaveCurrentDocumentRequest(
    {
      max_bytes: 1024,
      allowed_content_types: ["Application/PDF", "application/pdf"],
      suggested_filename: "候选人简历.pdf",
      timeout_ms: 3000,
    },
    "trace-test",
    "page.save_current_document",
  );
  assert.equal(request.max_bytes, 1024);
  assert.deepEqual(request.allowed_content_types, ["application/pdf"]);
  assert.equal(request.suggested_filename, "候选人简历.pdf");
  assert.equal(request.timeout_ms, 3000);
  assert.throws(() =>
    parseSaveCurrentDocumentRequest(
      { max_bytes: 104_857_601 },
      "trace-test",
      "page.save_current_document",
    ),
  );
  assert.throws(() =>
    parseSaveCurrentDocumentRequest(
      { allowed_content_types: ["text/html", 1] },
      "trace-test",
      "page.save_current_document",
    ),
  );
});

/** 验证 PDF 只通过 BrowserContext 请求，并使用安全文件名和脱敏地址写入统一记录。 */
test("当前 PDF 通过浏览器上下文保存到统一下载记录", async () => {
  const state = { disposed: 0 };
  const pageURL = "https://jobs.example.com/resume?token=secret#private";
  const finalURL = "https://files.example.com/resume.pdf?signature=secret";
  const content = Buffer.from("%PDF-1.7\nGoodHR");
  const { directory, requests, session } = await createSession(pageURL, async () =>
    createAPIResponse({
      state,
      url: finalURL,
      body: content,
      headers: {
        "content-type": "application/pdf; charset=binary",
        "content-length": String(content.length),
      },
    }),
  );
  try {
    const record = await saveCurrentDocument(session);
    assert.equal(record.status, "saved");
    assert.equal(record.filename, "黄明超_简历.pdf");
    assert.equal(path.dirname(record.file_path), directory);
    assert.equal(record.url, "https://files.example.com/resume.pdf");
    assert.equal(record.page_url, "https://jobs.example.com/resume");
    assert.equal(record.size, content.length);
    assert.equal((await fs.readFile(record.file_path)).subarray(0, 5).toString(), "%PDF-");
    assert.equal(session.listDownloads().downloads[0].file_path, record.file_path);
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, pageURL);
    assert.deepEqual(requests[0].options.headers, {
      accept: "application/pdf",
      "accept-encoding": "identity",
    });
    assert.equal(requests[0].options.failOnStatusCode, false);
    assert.equal(requests[0].options.maxRedirects, 5);
    assert.equal(requests[0].options.timeout, 3000);
    assert.equal(state.disposed, 1);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证响应头已经超限时不创建文件并及时释放响应。 */
test("错误 Content-Length 超限时立即拒绝", async () => {
  const state = { disposed: 0 };
  const { directory, session } = await createSession(
    "https://jobs.example.com/large-header",
    async (url) => createAPIResponse({
      state,
      url,
      headers: { "content-type": "application/pdf", "content-length": "2048" },
    }),
  );
  try {
    await assert.rejects(
      saveCurrentDocument(session),
      (error) => error.code === "DOWNLOAD_FAILED",
    );
    assert.deepEqual(await fs.readdir(directory), []);
    assert.equal(state.disposed, 1);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证没有可信长度头时仍会按实际正文拦截超限文件。 */
test("文档实际大小超限时不会留下文件", async () => {
  const { directory, session } = await createSession(
    "https://jobs.example.com/large-body",
    async (url) => createAPIResponse({
      url,
      body: Buffer.from(`%PDF-${"a".repeat(1200)}`),
    }),
  );
  try {
    await assert.rejects(
      saveCurrentDocument(session),
      (error) => error.code === "DOWNLOAD_FAILED",
    );
    assert.deepEqual(await fs.readdir(directory), []);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证 BrowserContext 请求和文件落盘共同受总时限约束。 */
test("读取正文不能超过文档保存总时限", async () => {
  const state = { disposed: 0 };
  const { directory, session } = await createSession(
    "https://jobs.example.com/slow-body",
    async (url) => createAPIResponse({ state, url, bodyDelayMS: 40 }),
  );
  try {
    await assert.rejects(
      saveCurrentDocument(session, { timeout_ms: 10 }),
      (error) => error.code === "DOWNLOAD_FAILED",
    );
    assert.deepEqual(await fs.readdir(directory), []);
    assert.equal(state.disposed, 1);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证浏览器上下文负责跳转且最多只允许五次。 */
test("文档跳转由浏览器上下文统一处理", async () => {
  const pageURL = "https://jobs.example.com/redirect?token=secret";
  const finalURL = "https://files.example.com/resume.pdf?signature=secret";
  const { directory, requests, session } = await createSession(pageURL, async () =>
    createAPIResponse({ url: finalURL }),
  );
  try {
    const record = await saveCurrentDocument(session);
    assert.equal(record.status, "saved");
    assert.equal(requests[0].options.maxRedirects, 5);
    assert.equal(record.url, "https://files.example.com/resume.pdf");
    assert.equal(record.page_url, "https://jobs.example.com/redirect");
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证内容类型伪装成 PDF 时仍会在落盘前检查文件签名并清理临时文件。 */
test("伪造 PDF 响应不会留下文件", async () => {
  const state = { disposed: 0 };
  const { directory, session } = await createSession(
    "https://jobs.example.com/fake-pdf",
    async (url) => createAPIResponse({
      state,
      url,
      body: "<html>登录失效</html>",
      headers: { "content-type": "application/pdf" },
    }),
  );
  try {
    await assert.rejects(
      saveCurrentDocument(session),
      (error) => error.code === "DOWNLOAD_FAILED",
    );
    assert.deepEqual(await fs.readdir(directory), []);
    assert.equal(state.disposed, 1);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});
