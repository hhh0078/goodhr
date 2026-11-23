<!-- 文件作用说明：记录Windows6.3.4.11专用工具ZIP发布、验证范围与数据库配置回滚证据。 -->

# Windows 6.3.4.11 发布

- main源码：`5937ad0cc7b693fd7ba8e5b9b58bb94984f697cc`。包含猎聘猎头端按原行简历编号定位、当前可见候选人起点、空屏备用起点及长列表贴底分页修复。
- 使用`package-tools/package-windows.bat 6.3.4.11 build`，正式环境，`vcs.modified=false`。ZIP：[下载6.3.4.11](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.11-windows-x64-e3eee3cb.zip)，31612101字节，SHA256：`e3eee3cb4e78e1ac84a076cf25e90e6f0af8ac96224ef84cd2d41c84b01b6d90`。
- ZIP含安装器与教程图片；内部安装器32072568字节，SHA256：`5dc6eb0a5b27feaaf0068b8ca337eabc2767b9035fb6cb153cc82e2a19671357`。公开更新配置使用ZIP与ZIP摘要。
- Go全量41个有测试包和go vet通过；Worker385通过、2跳过、0失败；Windows x64与Mac arm64/amd64编译通过。本轮没有真实招聘账号或Mac真机验证，交叉编译不能代替Mac安装运行。
- Windows最终包独立51480/51481端口和数据目录启动，健康版本6.3.4.11。正式更新器验证真实ZIP摘要、解压、内部安装器摘要及教程图片通过，临时测试已删除，未运行安装器。本机证据：`D:/gh-pack/windows-63411-20261123/`和`D:/gh-pack/win-63411-*.log`。
- 原子前插Windows6.3.4.11更新，从19条变为20条，保留Mac6.3.4.11、所有历史顺序、其他配置及app_config整行；现有升级规则不变。
- 数据库配置快照、精确回滚SQL及临时表实际发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.11-5937ad0c-20261123/`。公开API、下载页及完整ZIP的HTTP、大小、摘要与CRC已验证。
- 本轮仅上传约32MB分发ZIP并改配置，没有额外导出源码、安装包或Docker镜像作备份，没有部署/重启服务、新增容器或镜像。前后端容器保持`fc25ae226117`和`27ba794a0a39`；发布前根盘约17GiB、/www约3.6GiB可用。
