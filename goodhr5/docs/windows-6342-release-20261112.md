<!-- 文件作用说明：记录Windows6.3.4.2压缩包上传与更新配置变更。 -->

# Windows 6.3.4.2 发布

- 源码：`b9208409620345d9cf393480d2e9860a75547f4a`，使用专用工具 `package-tools/package-windows.bat 6.3.4.2 build`，正式环境，`vcs.modified=false`，没有修改业务代码。
- 更新：取消当前视口起跑的同名回读，修复五个平台正文读取时限。沿用现有升级规则，6.3.4系列可跳过本补丁。
- ZIP：[下载6.3.4.2](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.2-windows-x64-aadfba1e.zip)，31568091字节，SHA256：`aadfba1edec1812488865d230f152fff782b7eaf4056e6ae995ae2ae8d368cfe`。包含安装器和教程图片，更新配置使用ZIP及ZIP摘要。
- 内部安装器：32026714字节，SHA256：`bc97a91e2fd6063494eca2d96b826ca6ce4019c05660886cf92981775f03c723`。
- 用户要求停止额外核对时，构建、Go全量41个测试包、静态检查、Worker369通过/2跳过、Mac双架构编译、Windows独立启动及真实ZIP解压验证已经完成；随后按要求直接写入更新配置，没有再请求更新API或下载页核对。
- 服务器 `ldclouda31539`：原有4条全部保留，前插新条目后5条；Mac6.3.4.1及其他onboarding字段、完整app_config行保持不变。本轮未部署或重启云端。
- 配置备份与回滚材料：`/opt/goodhr-release/windows-6.3.4.2-b9208409-20261112/`。上传只新增约32MB ZIP，没有删除旧包、容器或镜像。
- 本机产物与日志：`D:/gh-pack/windows-6342-20261112/`、`D:/gh-pack/win-6342-*.log`。未运行安装器或操作真实招聘账号。
