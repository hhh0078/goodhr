<!-- 文件作用说明：记录Windows6.3.4.7专用工具打包、ZIP入口发布与回滚材料。 -->

# Windows 6.3.4.7 发布

- main源码：`7ffae478c6428f6d018b6c96f17a5d4b0cfddac0`，包含智联名单换位唯一定位、BOSS长截图40段及截图诊断更新。使用`package-tools/package-windows.bat 6.3.4.7 build`，正式环境，`vcs.modified=false`。
- ZIP：[下载6.3.4.7](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.7-windows-x64-82a1534e.zip)，31607162字节，SHA256：`82a1534ee85409a604192ebf1b054c7805dec80c97d1655b4b63c0cfe661e7f4`。公开更新使用ZIP及其摘要。
- ZIP含安装器与安装教程图片。内部安装器32067453字节，SHA256：`b1d9e08ff2fbb92abb462a7315b9d6d97b5d414e9b1032bbb795ef9d347023bf`。
- Go全量41个有测试包及go vet通过；Worker382通过、2跳过、0失败；Mac arm64和amd64交叉编译通过。本轮没有Mac真机安装验证、没有操作真实招聘账号。
- Windows打包产物独立51474/51475端口与数据目录启动，健康版本6.3.4.7；正式更新器验证真实ZIP摘要、解压及内部安装器摘要、教程图片通过，临时测试已删除，未运行安装器。证据：`D:/gh-pack/windows-6347-20261118/`及`D:/gh-pack/win-6347-*.log`。
- 发布前线上已有同版本Windows EXE和Mac PKG，本次仅替换Windows6.3.4.7的url_win和sha256，保留原文案、全部13条顺序、Mac、其他字段与app_config整行，现有升级规则不变。
- 备份、精确回滚SQL和实际临时表发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.7-7ffae478-20261118/`。公开更新API、下载页和完整ZIP摘要已验证。
- 本轮只上传约32MB ZIP并修改更新配置，没有部署或重启云端服务、删除旧文件/容器/镜像。生产前后端容器ID保持`4e6301b27cee`和`684e28d1f16d`；发布前后根盘约20GiB、/www约4.2GiB可用。
