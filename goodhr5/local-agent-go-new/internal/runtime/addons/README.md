<!-- 文件作用说明：记录随主程序内置的 UBO 扩展来源、版本、校验值和更新验证要求。 -->

# 内置 UBO 扩展

- 名称：uBlock Origin，作者 Raymond Hill & contributors。
- 版本：1.75.0（Firefox 版，要求 Firefox 115 或更高）。
- 官方文件：https://addons.mozilla.org/firefox/downloads/file/5034826/ublock_origin-1.75.0.xpi
- 官方校验来源：https://addons.mozilla.org/api/v5/addons/addon/ublock-origin/versions/1.75.0/
- SHA256：`5b74415860456370644bd80f16125e865b0e6c356bb5dfcfb84069967eaa5287`
- 对应源码：https://github.com/gorhill/uBlock/tree/1.75.0
- 许可证：GPL-3.0；原始 XPI 中的 LICENSE.txt 及第三方许可证全部保留，未修改扩展内容。

Go 使用 go:embed 把 XPI 带入主程序，Windows 安装器、ZIP 和 macOS 发布包均自动包含。
打开浏览器前逐文件检查大小与 CRC；缺失、损坏或版本不一致时，安全解压到临时目录，验证完成后替换。
旧 UBO 目录保存到同级 UBO-backup-*，不清理用户已有备份，不修改其他扩展和 Profile。
Worker 从 GOODHR_UBO_EXTENSION_PATH 加载本地扩展，并通过 exclude_addons 排除默认 UBO 下载。
这只取消扩展安装时的下载；扩展自身的过滤规则更新仍由 UBO 管理。

更新时从官方固定版本地址获取 XPI，核对 Mozilla SHA256，更新嵌入文件名、版本日志和测试。
必须验证全新目录离线安装、残缺目录修复、同大小文件损坏修复，以及打包后的真实浏览器启动。
