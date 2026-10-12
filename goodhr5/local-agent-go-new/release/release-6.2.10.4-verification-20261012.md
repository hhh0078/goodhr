<!-- 文件作用说明：记录本地安装器自动准备 Chrome 插件与 Windows 6.2.10.4 可选发布的产物、验证和回滚证据。 -->

# Windows 6.2.10.4 浏览器插件安装支持

源码提交：`e8653494d8946d66fd06f57a8bc24e80cc47f2cf`，已推送功能分支并快进合并到 `main`。

## 功能范围

- Windows 安装器复制完本地程序后，会从 GoodHR 公开配置读取最新版 Chrome 插件，下载并校验 SHA256，再安全解压到当前用户的稳定目录。
- 同版本换包、已安装文件缺失、内容被修改或增加多余文件时会自动修复；插件未变化时幂等跳过。
- 插件准备失败不会阻断本地程序安装。交互安装和静默升级检测到新插件后都会打开 Chrome 扩展管理页及插件目录。
- 普通 Chrome 不允许本地程序静默确认未上架扩展，用户仍需在扩展管理页完成一次“加载已解压的扩展程序”或“重新加载”。
- 插件包校验固定产品名 `GoodHR 简历助手`、Manifest V3、后台入口、版本和完整目录摘要，并拒绝越界路径、符号链接、Windows 设备名和 ADS 路径。

## Windows 产物

- 正式下载：[GoodHR 6.2.10.4 Windows ZIP](https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.4-windows-x64-9b8909be.zip)
- ZIP：30,682,794 字节。
- ZIP SHA256：`9b8909be1d09040931683d070785e3229e04567ded5d26183ecd3dc53d145ab3`。
- 安装器：31,142,724 字节，SHA256 `c2924f4e37427c39a1c3b9f96065942daa323c8b7c6c5ccf9455a3210b44146c`。
- Agent：28,330,496 字节，SHA256 `d60d32c24d400d3a6bf7e520d4e70c35b30c3a7b4f4acd8ab5724311848e92f1`。
- 安装教程 SHA256：`a1f82022a6d79f6de675b41695223ae8dc6df47fb8843ed5755e331f58319db8`。
- 发布包只包含安装器、`安装教程图片.jpg` 和 `安装说明.txt`，ZIP CRC 与三项内嵌哈希均已核验。
- 从 OSS 公网重新下载的文件大小、SHA256 和包内三项内容与本地正式包一致。

## 测试

- `go test ./...`：通过。
- `go vet ./...`：通过。
- Browser Worker：199 项通过，0 失败。
- Inno Setup 使用正式脚本实际编译通过。
- 正式 Agent EXE 对线上插件配置完成真实冒烟：下载并准备 `1.0.7`，插件包 SHA256 为 `e849548a44d9d0f5c29fd6bc4b24730800182953d95a5d4c5230f3cb4340109d`，目录摘要为 `d93a706d29d83bd5774b553bca145ac3a3d0639a26a616a0c5106a9b5a916ace`；第二次执行没有改写收据。
- 禁止页面脚本注入和 Chrome 企业强制安装策略的静态检查通过。

## 可选更新验证

- 版本历史从 40 条增加到 41 条，首条为 `6.2.10.4`。
- `system.app_config.local_agent_version` 仍为 `6.2.10.1`，因此本版不会触发强制更新。
- 只前插 `system.onboarding_config.local_agent`；`system.app_config` 和 onboarding 的其他字段保持不变。
- 正式写入前已在事务中执行“前插、CAS 撤回、核对原值、ROLLBACK”的真实回滚演练。
- 第一次发布门禁在写入前发现旧历史中 `5.1.3` 已有两条并主动退出。只读核对后，第二版门禁仅允许这个已确认的既有重复项；没有清理、覆盖或改写旧记录。

## 服务器与恢复材料

- 目标主机：`ldclouda31539`。
- 私密备份：`/www/wwwroot/goodhr-production/backups/local-agent-6.2.10.4-20261012-1350-e8653494-r2`。
- 备份目录权限为 `700 root:root`，7 个私密文件权限均为 `600`。
- 配置备份 SHA256：`085adbeb67a408e561fd88a52f6bed725bcdd56ee62e4a6b455b3fc3cd420e05`。
- 回滚文件：`rollback-local-agent-6.2.10.4.sql`，只有版本数组仍完整匹配本次发布目标时才会撤回首条。
- 第一次门禁退出的证据保留在 `/www/wwwroot/goodhr-production/backups/local-agent-6.2.10.4-20261012-1346-e8653494`；退出发生在任何配置写入前。

## 正式验收

- `https://goodhr.58it.cn/api/system/local-agent-updates` 返回 41 条，首条版本、URL 和 SHA256 均与正式包一致。
- `https://goodhr.58it.cn/download` 已展示 `6.2.10.4` 和目标下载链接。
- OSS 文件返回 HTTP 200、`application/zip` 和 30,682,794 字节。
- 前端、后端和 PostgreSQL 容器 ID 与发布前相同，均保持运行，本次没有重启容器。
