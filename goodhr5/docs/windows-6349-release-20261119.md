<!-- 文件作用说明：记录Windows6.3.4.9专用工具打包、正式ZIP发布与配置回滚材料。 -->

# Windows 6.3.4.9 发布

- main源码：`8e6c8b043966b22d4f9bcf545d293c94d0b0951e`。包含智联推荐页经历字段混入界面文字导致定位失败的修复，推荐页与搜索页的字段读取范围分开维护，保留完整身份与唯一性保护。
- 使用`package-tools/package-windows.bat 6.3.4.9 build`，正式环境，`vcs.modified=false`。ZIP：[下载6.3.4.9](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.9-windows-x64-c0828add.zip)，31606814字节，SHA256：`c0828addc478b7a153e7d7d7f7ce25cd0e68534f5e393039558b9e314bc96279`。
- ZIP含安装器和教程图片；内部安装器32067197字节，SHA256：`3c7fc8a9b0835d67d57cd9903a008bb546a03bb9a6291c01133173e6c51db343`。公开更新入口使用ZIP及ZIP摘要。
- Go全量41个有测试包和go vet通过，Worker385通过、2跳过、0失败。Windows x64编译、Mac arm64和amd64交叉编译通过；本轮没有Mac真机或真实招聘账号运行验证，上游账号只读验证不能视为本轮Windows账号实测。
- Windows最终打包目录独立51478/51479端口和数据目录启动，健康版本6.3.4.9；正式更新器校验真实ZIP、解压内部安装器和教程图片通过，未运行安装器，临时测试已删除。本机证据：`D:/gh-pack/windows-6349-20261119/`和`D:/gh-pack/win-6349-*.log`。
- 事务前插Windows6.3.4.9记录，从15条变为16条，原有历史顺序、Mac6.3.4.8、其他配置和app_config整行保留，现有升级规则不变。
- 数据库配置快照、精确回滚SQL及临时表实际发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.9-8e6c8b04-20261119/`。公开更新接口、下载页及完整ZIP的HTTP、大小、摘要、CRC已验证。
- 本轮仅上传约32MB分发ZIP并改配置，没有额外导出源码、安装包或Docker镜像作备份，没有部署或重启服务，没有新增云端容器或镜像。生产前后端容器保持`171e66451ac2`和`7fe8948d3cd5`；发布前后根盘约19GiB、/www约5.3GiB可用。
