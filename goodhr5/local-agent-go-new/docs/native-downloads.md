<!-- 文件作用说明：解释 Camoufox 原生下载链路、资源兼容检查、目录确认和验收方法。 -->

# 浏览器原生下载

## 下载链路

1. Go 运行组件管理器在 `omni.ja` 中安装原生下载观察资源。Windows/Linux 使用组件根目录，macOS 支持 `Camoufox.app/Contents/Resources`。
2. Worker 为唯一浏览器会话创建随机本地状态目录，通过 `firefoxUserPrefs` 传入路径与会话编号。
3. Firefox `Browser.enable` 应用首选项后，注册 `Downloads.ALL` 观察器；启用 `goodhr.downloads.native` 时 Juggler 不拦截下载。
4. Firefox 自己保存文件、记录原生下载历史。观察器仅将状态写入原子 JSON 快照，不控制页面或另存文件。
5. Worker 校验会话编号、字段、绝对路径和文件大小限制，使用既有 `/api/v1/downloads` 协议供 Go 同步。
6. Go 继续保存 SQLite 历史和显示完成提醒。关闭浏览器后先读最终快照，再删除随机状态目录。

## 目录与记录

- 初始目录沿用 GoodHR 设置，默认是当前用户的下载目录。
- 目录切换请求串行处理；浏览器确认设置版本号和实际目录后才返回成功。
- 文件名、重名处理和 PDF 打开行为由 Firefox 完成，不再补后缀或重命名浏览器下载。
- Worker 最近记录最多 100 条，清空 GoodHR 记录不触碰原生历史和文件。
- 同一原生下载在多次读取和重启后保持稳定编号，避免重复提醒；同路径的新下载按开始时间区分。
- 浏览器代码缓存通过 `-purgecaches` 刷新，不清除账号资料。
- 原资源备份只创建一次；重复安装不重复修改，未知资源结构直接拒绝。
- 旧版 GoodHR 下载文件和 SQLite 历史继续保留，旧记录不补写到 Firefox 历史。

## 验收

使用独立 Profile 和本地测试 PDF，不操作用户招聘账号：

1. 下载中文名称 PDF，确认真实文件名和内容完整。
2. 检查下载面板，以及 `Ctrl+J` 打开的“我的足迹 → 下载”。
3. 双击记录打开 PDF，使用“在文件夹中显示”定位文件。
4. 关闭并重新启动同一 Profile，检查原生历史仍在、文件仍可打开。
5. 切换 GoodHR 下载目录，确认后续下载使用新目录，原文件保持原位置。
6. 清空 GoodHR 记录，确认浏览器历史和文件仍保留。

回归测试：Worker 的 `test/native-downloads.test.mjs`；Go 的 `internal/runtime/native_downloads_test.go`。
真实浏览器资源检查仅在明确提供隔离目录时启用：`GOODHR_NATIVE_DOWNLOAD_TEST_RUNTIME`，不得指向正在使用的浏览器组件。

## Windows 打包

在 `goodhr5/local-agent-go-new` 目录执行：

```powershell
.\scripts\package-windows.ps1 -Version 6-native-download
```

输出安装器：`release/GoodHR-Local-Agent-Setup-v6-native-download-windows-x64.exe`。
安装后退出并重新启动本地程序和浏览器，新下载即可进入原生下载历史。
