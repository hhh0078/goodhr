<!-- 文件作用说明：记录Windows6.3.3专用工具打包、ZIP更新发布与验证边界。 -->

# Windows 6.3.3 发布验收

- 源码：最新 dev 固定提交 `a5803b72b2c965a14bcd6d619d5306f4e21d77be`，没有修改业务代码。工作区用户未提交的前端改动保留，另用干净目录构建。
- 入口：`goodhr5/local-agent-go-new/package-tools/package-windows.bat 6.3.3 build`。正式环境为 `https://goodhr.58it.cn`；成品版本6.3.3、Windows x64，`vcs.modified=false`。
- 内容：候选人断点续跑；前程无忧20人小批读取、稳定身份和滚动收尾；智联选岗加载稳定检测、未按下时重新定位及不确定结果禁止重试。

## 成品与下载格式

用户下载和程序更新入口均使用 [教程 ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.3-windows-x64-e96f088c.zip)，大小 `31553065` 字节，SHA256 为 `e96f088c17480d5ec37b633de916d820190359e3e4f3995d782579f0240ca549`。

ZIP 仅包含 `GoodHR-Local-Agent-Setup-v6.3.3-windows-x64.exe` 和 `安装教程图片.jpg`。内部安装器为 `32013221` 字节，SHA256 为 `412e0d74b2d4bdfe82d1e94e495e85b308ed6032d0eb076836663fbde1c57cae`，不把安装器摘要写入 ZIP 更新记录。本次只上传 ZIP，不单独发布 EXE。

## 验证

- `go test ./...` 通过，41个有测试包；`go vet ./...` 通过。Worker 366项：364通过、2跳过、0失败。
- Windows最终成品使用独立数据目录和50872/50873端口启动，健康接口返回6.3.3和ok，随后结束本轮进程。
- 真实发布ZIP的CRC、条目数和内部安装器逐字节核对通过；调用正式 `verifySHA256`、`prepareInstaller` 成功，解压后的安装器摘要一致且教程图片保留。临时测试已移除，没有运行安装器。
- Mac arm64、amd64交叉编译通过；不代表Mac真机安装或运行。上游真实平台验收见 `local-agent-go-new/docs/candidate-resume-and-zhaopin-job.md`，本轮未操作真实招聘账号，不把上游Mac证据视为Windows实测。
- 本机成品与证据：`D:/gh-pack/windows-633-20261111/`，构建与测试日志：`D:/gh-pack/win-633-*.log`。

## 配置备份与边界

- 主机：`ldclouda31539`。快照、发布SQL、精确回滚SQL及临时表发布/回滚演练：`/opt/goodhr-release/windows-6.3.3-a5803b72-20261111/`。
- 仅前插Windows6.3.3，保留Mac6.3.2、历史完整内容及顺序、其他onboarding配置与完整app_config行。沿用现有版本升级规则。
- 本轮不部署或重启云端，不删除旧成品、容器、镜像或数据卷。发布前根盘约5.9GiB、/www约2.9GiB，仅上传约32MB ZIP。
- 发布配置65条变66条，整行保留与临时表发布/回滚断言通过。公网完整下载为HTTP200，大小与ZIP SHA256均匹配；默认更新API和下载页已显示6.3.3 ZIP。前后端容器ID、镜像、启动时间不变；完成时根盘约5.9GiB、/www约2.9GiB。
