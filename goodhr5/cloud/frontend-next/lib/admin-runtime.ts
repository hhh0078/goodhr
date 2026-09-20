/** 本文件负责整理新版后台运行组件安装配置和状态。 */

export type RequiredRuntimeComponent = {
	key: "system_runtime" | "node_runtime" | "camoufox";
	name: string;
	installed: boolean;
};

const requiredRuntimeAssets: Record<string, string> = {
	node_runtime: "Node 运行环境",
	camoufox: "Camoufox 浏览器",
};

/** missingRequiredRuntimeURLs 返回当前系统必需运行组件里缺少下载地址或校验值的项目。 */
export function missingRequiredRuntimeURLs(config: any, platform: string) {
	const source = config?.runtime_components || config?.runtimeComponents || config?.local_runtime_components || config?.runtime || {};
	const platforms: Record<string, string[]> = {
		"win-x64": ["win-x64", "windows-x64", "win", "windows"],
		"darwin-arm64": ["darwin-arm64", "mac-arm64", "macos-arm64", "mac", "macos", "darwin"],
	};
	return Object.entries(requiredRuntimeAssets)
		.filter(([key]) => {
			const item = source?.[key] || {};
			const asset = (platforms[platform] || []).map((alias) => item?.[alias]).find((value) => value && typeof value === "object");
			return !String(asset?.url || "").trim() || !String(asset?.sha256 || "").trim();
		})
		.map(([, name]) => name);
}

/** buildRuntimeInstallPayload 将系统组件配置转换为本地程序安装接口参数。 */
export function buildRuntimeInstallPayload(config: any) {
	const source = config?.runtime_components || config?.runtimeComponents || config?.local_runtime_components || config?.runtime || {};
	const aliases: Record<string, string[]> = {
		node_runtime: ["node_runtime", "nodeRuntime", "node"],
		camoufox: ["camoufox", "camofox", "camou_fox", "camouFox"],
		ocr: ["ocr", "rapidocr", "rapidOCR"],
	};
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
	const browserInstalled = Boolean(runtime?.camoufox_installed || runtime?.runtime?.camoufox_installed);
	const components: RequiredRuntimeComponent[] = [
		{ key: "node_runtime", name: "Node 运行环境", installed: Boolean(runtime?.node_installed || runtime?.runtime?.node_installed) },
		{ key: "camoufox", name: "Camoufox 浏览器", installed: browserInstalled },
	];
	const systemRuntime = runtime?.system_runtime || runtime?.runtime?.system_runtime;
	if (systemRuntime?.required) {
		components.unshift({
			key: "system_runtime",
			name: String(systemRuntime?.name || "Microsoft Visual C++ x64 运行库"),
			installed: Boolean(systemRuntime?.ready),
		});
	}
	return components;
}

/** hasMissingRequiredRuntime 判断是否缺少必要运行组件。 */
export function hasMissingRequiredRuntime(runtime: any) {
	return requiredRuntimeComponents(runtime).some((item) => !item.installed);
}

/** hasMissingDownloadableRuntime 判断是否还缺少由 GoodHR 自己下载的 Node 或浏览器组件。 */
export function hasMissingDownloadableRuntime(runtime: any) {
	return requiredRuntimeComponents(runtime).some((item) => item.key !== "system_runtime" && !item.installed);
}

/** waitForRuntimeInstall 等待本地异步安装真正完成，并把进度交给页面展示。 */
export async function waitForRuntimeInstall(
	readStatus: () => Promise<unknown>,
	onStatus: (status: Record<string, unknown>) => void,
) {
	const deadline = Date.now() + 30 * 60 * 1000;
	while (Date.now() < deadline) {
		const response = await readStatus();
		if (!response || typeof response !== "object") throw new Error("组件状态没拿到，请重试一次");
		const status = response as Record<string, unknown>;
		onStatus(status);
		const progress = status.install_progress && typeof status.install_progress === "object"
			? status.install_progress as Record<string, unknown> : {};
		if (!progress.running) {
			if (progress.stage === "failed") throw new Error(String(progress.message || "组件安装没成功，请重试一次"));
			if (progress.stage === "installed" && !hasMissingDownloadableRuntime(status)) return status;
		}
		await new Promise<void>((resolve) => setTimeout(resolve, 1000));
	}
	throw new Error("组件安装等太久了，请查看本地程序日志后重试");
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
