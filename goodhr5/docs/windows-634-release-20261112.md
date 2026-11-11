<!-- 文件作用说明：记录Windows6.3.4专用工具打包、ZIP更新、验证边界与回滚位置。 -->

# Windows 6.3.4 发布验收

- 源码：最新dev固定提交 `5abf8091967668efbc0e357124aa44bbb2c32a6e`，未修改业务代码。用户原工作区的前端未提交改动保留，独立干净目录构建。
- 使用项目专用入口 `goodhr5/local-agent-go-new/package-tools/package-windows.bat 6.3.4 build`，正式环境为 `https://goodhr.58it.cn`。最终Windows x64成品版本6.3.4，`vcs.modified=false`。
- 累计更新：推荐页从当前首张完整卡片开始、搜索页保留断点恢复，智联选岗刷新与搜索分页修复、前程无忧搜索起点和邀投递核对、Worker读取优化及诊断日志更新。

## 成品

- 用户下载及程序更新均使用 [6.3.4 ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.4-windows-x64-e44f0dae.zip)，大小31567706字节，SHA256：`e44f0dae1ef327c63be974b473bcd6f12ec8733dbc35db16bf90b449e9b8de3f`。
- ZIP只包含安装器和安装教程图片。内部安装器32026335字节，SHA256：`2fe9fd1eb6aa98588ffaf6a8c5fe340791a1abec98c9df62e2fc44f2506e64b6`。本次不单独发布EXE，更新记录使用ZIP摘要。
- 沿用已确认的现有升级规则；6.3.3系列到6.3.4属于前三段升高，需要升级，不另改最低门槛。

## 验证与限制

- `go test ./...`通过，41个有测试包；`go vet ./...`通过。Worker369项：367通过、2跳过、0失败。
- 最终Windows成品使用独立数据目录和51072/51073端口启动，健康接口返回6.3.4和ok，随后结束本轮进程。
- 真实ZIP的CRC、条目数及安装器逐字节一致验证通过；调用正式 `verifySHA256` 和 `prepareInstaller` 成功，解压后安装器摘要一致、教程图片保留。没有运行安装器，临时验证文件已清理。
- Mac arm64/amd64交叉编译通过，不代表Mac设备安装和运行验证。上游真页记录见 `local-agent-go-new/docs/candidate-resume-and-zhaopin-job.md`；本轮不操作真实招聘账号或发送消息，不把上游Mac证据称为本轮Windows实测。
- 本机成品和证据：`D:/gh-pack/windows-634-20261112/`，构建与测试日志：`D:/gh-pack/win-634-*.log`。

## 配置保留与回退

- 主机：`ldclouda31539`。配置快照、原子发布SQL、精确回滚SQL与临时表发布/回滚演练：`/opt/goodhr-release/windows-6.3.4-5abf8091-20261112/`。
- 仅新增Windows6.3.4，保留Mac6.3.3.1、完整历史顺序、其他onboarding字段和完整app_config行。本轮不部署或重启云端，不删除旧包、容器、镜像或数据卷。
- 发布前根盘约5.9GiB、/www约2.6GiB；本次只上传约32MB ZIP。
- 配置68条变69条，Mac及历史、其他配置整行保留断言和临时表发布/回滚演练通过。公网ZIP完整下载HTTP200、大小及SHA256一致；默认更新API与下载页已显示6.3.4 ZIP。发布后前后端容器ID、镜像和启动时间未变；完成时根盘约5.9GiB、/www约2.5GiB。
