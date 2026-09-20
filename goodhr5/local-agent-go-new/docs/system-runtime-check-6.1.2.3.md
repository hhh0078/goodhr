<!-- 文件作用：说明 6.1.2.3 的 Windows 运行库检查、用户提示和 Worker 退出处理。 -->
# 6.1.2.3 Windows 运行库检查

运行组件状态新增 `system_runtime`。Windows 先检查 `msvcp140.dll`、`vcruntime140.dll` 和 `vcruntime140_1.dll`；Node 与 Worker 已存在时，再用同一个内置 Node 实际加载 `impit`。实际加载不启动浏览器、不监听端口，也不访问招聘页面。

组件页面会把 Microsoft Visual C++ x64 运行库与 Node、Camoufox 一起展示。缺失或加载失败时，页面给出微软官方下载入口和“安装或修复后重新检查”按钮；Mac 不显示 Windows 运行库。运行库由用户安装或修复，本地程序不静默安装系统软件。

`EnsureWorker` 在启动 Worker 前复用同一检查，避免绕过页面。Worker 进程若在健康检查期间提前退出，立即返回“启动后立即退出”，不再等满十二秒误报启动超时。
