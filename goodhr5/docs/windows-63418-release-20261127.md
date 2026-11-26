<!-- 文件作用说明：记录Windows6.3.4.18专用工具ZIP发布、验证范围与数据库配置回滚证据。 -->

# Windows 6.3.4.18 发布

- main源码：`9c69f3418c552e8ad6c860dc082286fa346b71cf`。包含AI解析、失败详情和请求耗时日志、主站接口恢复、账号重启恢复及WorkBuddy命令连接等更新。
- 使用`package-tools/package-windows.bat 6.3.4.18 build`，正式环境，`vcs.modified=false`；公共主站`https://goodhr.58it.cn`，命令通道`https://goodhr5.58it.cn`。
- ZIP：[下载6.3.4.18](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.18-windows-x64-107bf2ed.zip)，31661473字节，SHA256：`107bf2eddedb8d487f08481d360a6017b3cc57c98b275aca7bb68b87038911e6`。ZIP含安装器及教程图片，内部安装器32123310字节，SHA256：`dda371bf15b19b303d00a055c48c6b17262efa76fffded17142e0f2512abc556`。
- Go全量43个有测试包和go vet通过，Worker404通过、2跳过、0失败。Windows x64、Mac arm64/amd64编译通过；本轮没有Mac真机或真实招聘账号验证，交叉编译不代替安装运行验证。
- 最终Windows打包程序在独立51482/51483端口与数据目录启动，健康版本6.3.4.18；正式更新器验证真实ZIP摘要、解压、内部安装器摘要及教程图片通过，临时测试清理，未运行安装器。证据：`D:/gh-pack/windows-63418-20261127/`和`D:/gh-pack/win-63418-*.log`。
- 原子前插Windows6.3.4.18更新，25条增至26条。保留Mac6.3.4.18、所有历史顺序、其他字段及app_config整行，现有升级规则不变。
- 数据库配置快照、精确回滚SQL及临时表实际发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.18-9c69f341-20261127/`。公开API、下载页及完整ZIP的HTTP、大小、摘要、CRC已核验。
- 本轮仅上传约32MB分发ZIP并改更新配置，没有部署/重启服务、新增容器或镜像，没有额外导出源码、安装包或镜像作备份。生产前后端容器保持`d3be75dab246`和`38d78d77abdc`。根盘发布前后约11GiB，/www发布前7.9GiB、发布后7.8GiB可用。
