/** 文件作用：补齐 Edge/Chromium 92 缺少的标准方法，保留新版浏览器已有实现。 */
import "core-js/actual/array/at.js";
import "core-js/actual/array/find-last.js";
import "core-js/actual/array/find-last-index.js";
import "core-js/actual/object/has-own.js";
import "core-js/actual/promise/with-resolvers.js";

/**
 * installBrowserCrypto 仅在缺少 randomUUID 时用浏览器安全随机数生成标准 UUID，不降级随机数来源。
 * @param cryptoProvider 浏览器加密接口；未提供时读取当前运行环境的接口。
 * @returns 无返回值；原生实现或安全随机数不可用时保持原样。
 */
export function installBrowserCrypto(cryptoProvider: Crypto | undefined = globalThis.crypto): void {
  if (!cryptoProvider || typeof cryptoProvider.randomUUID === "function" || typeof cryptoProvider.getRandomValues !== "function") return;
  Object.defineProperty(cryptoProvider, "randomUUID", {
    configurable: true,
    writable: true,
    /** randomUUID 使用安全随机数生成符合 RFC 4122 的第四版 UUID，供请求去重和草稿标识使用。 */
    value: function randomUUID(): ReturnType<Crypto["randomUUID"]> {
      const bytes = cryptoProvider.getRandomValues(new Uint8Array(16));
      bytes[6] = (bytes[6] & 0x0f) | 0x40;
      bytes[8] = (bytes[8] & 0x3f) | 0x80;
      const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
      return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
    },
  });
}

installBrowserCrypto();
