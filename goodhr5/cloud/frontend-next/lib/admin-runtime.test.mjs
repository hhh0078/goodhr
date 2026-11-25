/** 本文件验证 Mac 双架构资源选择及必要组件安装终态，避免装错架构或把单组件完成当成整套可用。 */

import assert from "node:assert/strict";
import test from "node:test";
import { buildRuntimeInstallPayload, missingRequiredRuntimeURLs, waitForRuntimeInstall } from "./admin-runtime.ts";

const macArmAsset = { version: "arm", url: "https://example.test/mac-arm.zip", sha256: "a".repeat(64), note: "Apple 芯片" };
const macIntelAsset = { version: "intel", url: "https://example.test/mac-intel.zip", sha256: "b".repeat(64), note: "Intel 芯片" };
const macOCRAsset = { version: "ocr", url: "https://example.test/mac-universal.zip", sha256: "c".repeat(64), note: "Mac 通用" };

test("Intel Mac保留独立Node和Camoufox，旧OCR资源不再进入安装请求", () => {
	const config = {
		runtime_components: {
			node_runtime: { mac: macArmAsset, "darwin-x64": macIntelAsset },
			camoufox: { mac: macArmAsset, "darwin-x64": macIntelAsset },
			ocr: { mac: macOCRAsset, "darwin-x64": macOCRAsset },
		},
	};
	const { manifest } = buildRuntimeInstallPayload(config);

	assert.deepEqual(missingRequiredRuntimeURLs(config, "darwin-x64"), []);
	assert.deepEqual(manifest.node_runtime["darwin-x64"], macIntelAsset);
	assert.deepEqual(manifest.camoufox["darwin-x64"], macIntelAsset);
	assert.equal(manifest.ocr, undefined);
	assert.deepEqual(manifest.node_runtime["darwin-arm64"], macArmAsset);
});

test("只有 Apple 芯片资源时 Intel Mac 明确报告缺失", () => {
	const config = {
		runtime_components: {
			node_runtime: { mac: macArmAsset },
			camoufox: { mac: macArmAsset },
		},
	};
	const { manifest } = buildRuntimeInstallPayload(config);

	assert.deepEqual(missingRequiredRuntimeURLs(config, "darwin-x64"), ["Node 运行环境", "Camoufox 浏览器"]);
	assert.equal(manifest.node_runtime["darwin-x64"], undefined);
	assert.equal(manifest.camoufox["darwin-x64"], undefined);
});

test("浏览器补丁完成仍缺Node时立即提示继续安装，不等待三十分钟或误报成功", async () => {
	const status = { node_installed: false, camoufox_installed: true, install_progress: { component: "camoufox", stage: "installed", running: false, percent: 100 } };
	let reads = 0;
	await assert.rejects(waitForRuntimeInstall(async () => { reads++; return status; }, () => {}), /还差Node 运行环境.*安装必要组件/);
	assert.equal(reads, 1);
});

test("单组件installed但整轮仍running时继续等待，全部组件齐全后才完成", async () => {
	const pending = { node_installed: false, camoufox_installed: true, install_progress: { component: "camoufox", stage: "installed", running: true, percent: 100 } };
	const complete = { node_installed: true, camoufox_installed: true, install_progress: { stage: "installed", running: false, percent: 100 } };
	const seen = [];
	let reads = 0;
	assert.equal(await waitForRuntimeInstall(async () => ++reads === 1 ? pending : complete, (status) => seen.push(status)), complete);
	assert.deepEqual(seen, [pending, complete]);
});

test("必要下载组件齐全但Windows运行库缺失时返回真实状态交由安装框继续引导", async () => {
	const status = { node_installed: true, camoufox_installed: true, system_runtime: { required: true, ready: false }, install_progress: { stage: "installed", running: false } };
	assert.equal(await waitForRuntimeInstall(async () => status, () => {}), status);
});
