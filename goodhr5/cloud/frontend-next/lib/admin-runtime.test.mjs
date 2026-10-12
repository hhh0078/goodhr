/** 本文件验证 Intel Mac 运行组件使用独立下载资源，避免误用 Apple 芯片安装包。 */

import assert from "node:assert/strict";
import test from "node:test";
import { buildRuntimeInstallPayload, missingRequiredRuntimeURLs } from "./admin-runtime.ts";

const macArmAsset = { version: "arm", url: "https://example.test/mac-arm.zip", sha256: "a".repeat(64), note: "Apple 芯片" };
const macIntelAsset = { version: "intel", url: "https://example.test/mac-intel.zip", sha256: "b".repeat(64), note: "Intel 芯片" };
const macOCRAsset = { version: "ocr", url: "https://example.test/mac-universal.zip", sha256: "c".repeat(64), note: "Mac 通用" };

test("Intel Mac 清单保留独立的 Node、Camoufox 和 OCR 资源", () => {
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
	assert.deepEqual(manifest.ocr["darwin-x64"], macOCRAsset);
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
