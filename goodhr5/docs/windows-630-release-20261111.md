<!-- 文件作用说明：记录 Windows 6.3 专用工具打包、兼容修正、服务器发布及验证边界。 -->

# Windows 6.3 发布记录

- 日期：2026-11-11；源代码：dev `9dd6b73ba7530a63a5d0c26b4993a3c9ee38adcd` 加修正 `7a3c62bdc4cef9a376a363ed1501ea268973aebc`，主分支修正提交 `2008e6d1`。
- 按用户指定使用 `goodhr5/local-agent-go-new/package-tools/package-windows.bat 6.3 build`，没有另写打包流程。正式环境 `https://goodhr.58it.cn`，最终二进制 `vcs.modified=false`。
- 用户确认按现有规则发布 6.3：6.2 到 6.3 为必须升级。本轮不修改网站版本判断，不提高其他配置门槛。

## 成品

| 文件 | 字节 | SHA256 |
| --- | --- | --- |
| [Windows 教程 ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3-windows-x64-d7429008.zip) | 31533872 | `d74290082f9eb327348fb13c7d324fd76b703332ce1b9cb9bae91087e52b4352` |
| [安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3-windows-x64-9c13300c.exe) | 31992357 | `9c13300c2ef568ae5ff6536bd50a9dacd113c7de31bc3f11ddeaf1cf26adf0d5` |

ZIP 仅含安装器与安装教程图片，CRC 及内部安装器逐字节验证通过。更新记录使用 EXE 的 URL 和 SHA256。

## 验证与最小修正

- 首轮发现 Windows Winsock 连接拒绝错误码未分类，以及附件测试使用 Unix 文件 URL。补充 Windows 10061 分类，测试路径按当前系统生成，不改招聘动作。
- 修正后 `go test ./...` 通过（41 个有测试包）；`go vet ./...` 通过。Worker 366 项：364 通过、2 跳过、0 失败。
- Windows 最终包独立数据目录与 50672/50673 端口启动，`/health` 返回版本 6.3、状态 ok；随后结束本轮进程。没有覆盖安装或操作招聘账号。
- Mac arm64/amd64 主程序交叉编译通过；不是 Mac 实机运行或安装验证。
- 两份公网成品完整下载后大小和 SHA256 一致；正式更新 API 及下载页已显示 Windows 6.3。

## 配置保留与回退

- 主机：`ldclouda31539`。备份、原子发布 SQL、精确回滚 SQL 及临时表发布/回滚演练证据：`/opt/goodhr-release/windows-6.3-7a3c62bd-20261111/`。
- 初次发布前断言发现同期 Mac 已更新 6.3，未写入配置；重新读取后锁定完整原始行，只前插 Windows 条目，62 条变 63 条，原 Mac 6.3、全部历史顺序及其他配置原样保留，`system.app_config` 整行不变。
- 本轮未构建、部署或重启云端服务，未删除旧成品、容器或镜像。发布前根盘约 7.2 GiB、/www 4.8 GiB；完成时根盘约 7.2 GiB、/www 2.9 GiB，同期其他任务磁盘变化不归因于本轮约 64 MB 成品。
- 本机证据：`D:/gh-pack/windows-630-20261111/`，构建/测试日志：`D:/gh-pack/win-630-*.log`。
