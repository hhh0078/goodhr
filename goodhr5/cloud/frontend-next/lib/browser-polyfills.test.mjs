/** 文件作用：验证旧浏览器缺失方法的标准行为、安全 UUID 与新版浏览器原生实现保留。 */
import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { installBrowserCrypto } from "./browser-polyfills.ts";

test("安全 UUID 补丁保留原生实现，缺少安全随机数时不伪造 UUID", () => {
  const native = () => "native-uuid";
  const modern = { randomUUID: native, getRandomValues() { throw new Error("不应调用"); } };
  installBrowserCrypto(modern);
  assert.equal(modern.randomUUID, native);
  const unavailable = {};
  installBrowserCrypto(unavailable);
  installBrowserCrypto(undefined);
  assert.equal(unavailable.randomUUID, undefined);
});

test("Edge 92 的安全随机数生成标准 UUID，补丁可重复初始化", () => {
  const legacy = { getRandomValues(bytes) { bytes.fill(255); return bytes; } };
  installBrowserCrypto(legacy);
  assert.equal(legacy.randomUUID(), "ffffffff-ffff-4fff-bfff-ffffffffffff");
  assert.equal(Object.keys(legacy).includes("randomUUID"), false);
  const installed = legacy.randomUUID;
  installBrowserCrypto(legacy);
  assert.equal(legacy.randomUUID, installed);
});

test("新版浏览器的标准方法保持原实现", () => {
  execFileSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", `
    import assert from 'node:assert/strict';
    const before = [Array.prototype.at, Array.prototype.findLast, Array.prototype.findLastIndex, Object.hasOwn, Promise.withResolvers, crypto.randomUUID];
    await import(${JSON.stringify(new URL("./browser-polyfills.ts", import.meta.url).href)});
    assert.deepEqual([Array.prototype.at, Array.prototype.findLast, Array.prototype.findLastIndex, Object.hasOwn, Promise.withResolvers, crypto.randomUUID], before);
  `], { stdio: "pipe" });
});

test("缺少标准方法时同步补齐，并正确处理负索引、空数组和稀疏数组", () => {
  execFileSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", `
    import assert from 'node:assert/strict';
    delete Array.prototype.at;
    delete Array.prototype.findLast;
    delete Array.prototype.findLastIndex;
    delete Object.hasOwn;
    delete Promise.withResolvers;
    await import(${JSON.stringify(new URL("./browser-polyfills.ts", import.meta.url).href)});
    assert.equal([1, 2, 3].at(-1), 3);
    assert.equal([].at(-1), undefined);
    assert.equal([1].at(Infinity), undefined);
    assert.equal([1, 2, 3].findLast(n => n < 3), 2);
    assert.equal([].findLast(() => true), undefined);
    assert.equal(Array(2).findLastIndex(n => n === undefined), 1);
    assert.equal(Object.hasOwn(Object.create({ inherited: 1 }), 'inherited'), false);
    assert.equal(Object.hasOwn({ own: undefined }, 'own'), true);
    const { promise, resolve } = Promise.withResolvers(); resolve('完成');
    assert.equal(await promise, '完成');
  `], { stdio: "pipe" });
});
