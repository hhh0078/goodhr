/** 本文件负责整理新版后台运行组件安装配置和状态。 */

export type RequiredRuntimeComponent = {
	key: "node_runtime" | "cloakbrowser_wrapper" | "cloakbrowser";
	name: string;
	installed: boolean;
};

/** missingRuntimeNodeURL 判断当前本机平台是否缺少 Node 下载地址。 */
export function missingRuntimeNodeURL(config: any, platform: unknown) {
	const source = config?.runtime_components || config?.runtimeComponents || config?.local_runtime_components || config?.runtime || {};
	const node = source?.node_runtime || source?.nodeRuntime || source?.node || {};
	const key = String(platform || "").startsWith("darwin") ? "mac" : "win";
	const aliases = key === "mac" ? ["darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"] : ["win-x64", "windows-x64", "win", "windows"];
	return !aliases.some((alias) => String(node?.[alias]?.url || "").trim());
}

/** buildRuntimeInstallPayload 将系统组件配置转换为本地程序安装接口参数。 */
export function buildRuntimeInstallPayload(config: any) {
	const source = config?.runtime_components || config?.runtimeComponents || config?.local_runtime_components || config?.runtime || {};
	const aliases: Record<string, string[]> = { node_runtime: ["node_runtime", "nodeRuntime", "node"], ocr: ["ocr", "rapidocr", "rapidOCR"] };
	const platforms: Record<string, string[]> = { "win-x64": ["win-x64", "windows-x64", "win", "windows"], "darwin-arm64": ["darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"] };
	const manifest: Record<string, any> = {};
	for (const [component, componentAliases] of Object.entries(aliases)) {
		const componentConfig = componentAliases.map((key) => source?.[key]).find((value) => value && typeof value === "object") || {};
		manifest[component] = {};
		for (const [platform, platformAliases] of Object.entries(platforms)) {
			const asset = platformAliases.map((key) => componentConfig?.[key]).find((value) => value && typeof value === "object");
			if (asset?.url) manifest[component][platform] = { version: String(asset.version || ""), url: String(asset.url || ""), sha256: String(asset.sha256 || ""), note: String(asset.note || asset.changelog || asset.description || asset.release_note || "") };
		}
	}
	return { manifest };
}

/** requiredRuntimeComponents 返回必须安装的运行组件列表。 */
export function requiredRuntimeComponents(runtime: any): RequiredRuntimeComponent[] {
	return [
		{ key: "node_runtime", name: "Node 运行环境", installed: Boolean(runtime?.node_installed || runtime?.runtime?.node_installed) },
		{ key: "cloakbrowser_wrapper", name: "CloakBrowser 控制组件", installed: Boolean(runtime?.cloakbrowser_wrapper_installed || runtime?.runtime?.cloakbrowser_wrapper_installed) },
		{ key: "cloakbrowser", name: "最新版 Chromium", installed: Boolean(runtime?.cloakbrowser_installed || runtime?.runtime?.cloakbrowser_installed) },
	];
}

/** hasMissingRequiredRuntime 判断是否缺少必要运行组件。 */
export function hasMissingRequiredRuntime(runtime: any) {
	return requiredRuntimeComponents(runtime).some((item) => !item.installed);
}

/** formatRuntimeBytes 格式化运行组件下载进度字节数。 */
export function formatRuntimeBytes(bytes: number) {
	if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
	const units = ["B", "KB", "MB", "GB"];
	let value = bytes;
	let index = 0;
	while (value >= 1024 && index < units.length - 1) {
		value /= 1024;
		index += 1;
	}
	return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}
