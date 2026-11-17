<!-- 文件作用说明：记录Windows6.3.4.6专用工具ZIP打包、同版本入口更新及回滚位置。 -->

# Windows 6.3.4.6 发布

- main源码：`9e7350dd156877b1bdac9a1006c17d117a32f851`。更新长名单起点定位及前程无忧小批读取。专用工具：`package-tools/package-windows.bat 6.3.4.6 build`，正式环境，`vcs.modified=false`，未修改业务代码。
- ZIP：[下载6.3.4.6](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.6-windows-x64-78d2fcd6.zip)，31604913字节，SHA256：`78d2fcd65c55c9585b0320ed4538c84985235c6dee639758b47918a71be9b3da`。更新入口使用ZIP和ZIP摘要。
- ZIP含安装器和教程图片。内部安装器32064991字节，SHA256：`423936280eae6e94de9907a5b302c8d0d376690376e7ec316efdbb1d94ba97d4`。
- Go全量41个有测试包、go vet、Worker375通过/2跳过、Mac arm64及amd64编译均通过。Windows最终包独立51472/51473端口及数据目录启动，健康版本6.3.4.6；正式ZIP校验、解压、内部安装器摘要与教程图片验证通过，没有运行安装器，临时测试清理。
- 本轮未操作真实招聘账号，不能把上游Mac现场复现视作本轮Windows实测。本机证据：`D:/gh-pack/windows-6346-20261117/`、`D:/gh-pack/win-6346-*.log`。
- 发布前已有Windows6.3.4.6 EXE入口，本轮不重复添加版本，仅把该条的url_win和sha256替换为本次ZIP；保留原文案、Mac6.3.4.6、全部11条顺序及其他配置，app_config整行不变。旧EXE不删除，升级规则不变。
- 备份、回滚SQL及临时表发布/回滚演练：`/opt/goodhr-release/windows-6.3.4.6-9e7350dd-20261117/`。公网完整下载HTTP200、大小与摘要一致，默认更新API和下载页已指向ZIP。
- 本轮不手动部署或重启云端，不删除容器、镜像或旧包；发布前根盘约19GiB、/www约4.1GiB，只上传约32MB ZIP。
