<!-- 文件作用说明：记录Windows6.3.4.8专用工具ZIP发布、智联修复验证范围与数据库配置回滚材料。 -->

# Windows 6.3.4.8 发布

- main源码：`c46c644ba6a6086e606580c442d62ff13183076d`，修复智联完整身份定位对卡片分块和排版换行不兼容的问题，保留身份字段、顺序与唯一性保护。使用`package-tools/package-windows.bat 6.3.4.8 build`，正式环境，`vcs.modified=false`。
- ZIP：[下载6.3.4.8](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.8-windows-x64-217af24c.zip)，31608564字节，SHA256：`217af24cfdd8d7769ed97f61390180af4781d6d131d313383b8dfeb4c015bdcc`。
- ZIP内含安装器与教程图片。内部安装器32068904字节，SHA256：`7a77bd775097b190d2150273b0d5003f8d471fe74d1582c8a5b0040aefa294ad`。更新入口使用ZIP摘要。
- Go全量41个有测试包及go vet通过，Worker385通过、2跳过、0失败；Windows编译、Mac arm64/amd64交叉编译通过。Windows独立51476/51477端口和数据目录启动健康版本6.3.4.8；正式更新器验证真实ZIP校验、解压、内部安装器摘要与教程图片通过，临时测试已删除，未运行安装器。
- 未操作真实招聘账号，Mac真机及Intel运行未在本轮验证；上游合成页验证不能视为本轮Windows账号实测。证据：`D:/gh-pack/windows-6348-20261119/`与`D:/gh-pack/win-6348-*.log`。
- 发布前已有同版本Windows ZIP入口，本轮仅替换该记录的url_win和sha256，保留原文案、全部15条记录顺序、Mac、其他配置与app_config整行，升级规则不变。
- 数据库配置快照、回滚SQL及临时表实际发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.8-c46c644b-20261119/`。公开更新接口、下载页与完整ZIP的HTTP、字节数、摘要和CRC已验证。
- 只上传约32MB分发ZIP并更新配置；没有额外导出源码、安装包或Docker镜像作备份，没有新增云端容器或镜像，没有部署/重启服务。发布前后生产前后端容器ID为`171e66451ac2`和`7fe8948d3cd5`；根盘约19GiB，/www发布前5.4GiB、发布后5.3GiB可用。
