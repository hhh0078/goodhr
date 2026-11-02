<!-- 文件作用说明：记录Windows 6.2.12.2前程无忧可选更新的源码、正式文件、验证结果和配置回退材料。 -->

# Windows 6.2.12.2 前程无忧可选更新

北京时间2026年11月2日10:13，按用户要求同步最新代码，完成Windows x64正式打包和可选更新发布。本次没有修改业务源码或部署云端服务。

## 源码与功能

- 精确构建源：`origin/dev` 的干净提交 `890365278744e54a31797193ca15ea1f07f36124`，Go元数据为 `vcs.modified=false`。
- 与已发布Mac 6.2.12.2的 `f1737c16` 比较，本地程序的internal、Worker、scripts、assets和packaging代码完全一致；后续仅增加Mac验收文档。主分支当时只包含云端精选发布，尚无完整前程本地适配，因此未使用main构建，也未将开发中的自动回复功能合并进main。
- 包含前程无忧人才推荐、搜索人才页面的主动招呼适配，智联搜索人才、支持页面复用与宽窄窗口兼容，以及前程默认书签。
- 前程独立自动回复、附件下载尚未开放。当前云端已发布的平台入口继续使用，本次不执行数据库迁移。
- 更新后请完整退出并重新打开GoodHR和浏览器，不能只关闭后台网页。

## 正式文件

| 文件 | 大小（字节） | SHA256 |
| --- | ---: | --- |
| [Windows安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.2-windows-x64-95f301e1.exe) | 31,523,219 | `95f301e125a3a2819444eb4b5e1d169ce042c27b80e71c7d5466ad9490b8ff0d` |
| [Windows ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.2-windows-x64-40ce49ea.zip) | 40,654,407 | `40ce49eaee01f735e4112a7236956053cdbbe7ed997f8dd5ab8c866aa79d27ff` |

线上更新记录指向EXE安装器，SHA256对应EXE本身。ZIP为主程序、Worker及依赖的完整分发包，不作为本次自动更新入口。主程序SHA256为 `34e8a4c60f1fff588460f8eafdfbda045b2cfe076f6172d239b8074bad9e7ac6`。

使用既有 `scripts/package-windows.ps1 -Version 6.2.12.2 build`；npm镜像为 `https://registry.npmmirror.com`，Go代理为 `https://goproxy.cn,direct`，依赖版本未升级。构建使用Go 1.27.0、Windows amd64、CGO关闭，正式地址为 `https://goodhr.58it.cn`。

## 验证

- Go全量测试和 `go vet ./...` 通过。39个测试包通过，4个包无测试；顶层704项通过、20项环境跳过，零失败；含子用例1197项通过。前程job51包34项全部通过。
- Worker测试234项：232通过、2项环境跳过，零失败；书签回归2项通过。
- ZIP全部2,247个文件通过CRC和与最终目录逐文件摘要比对。安装器ProductVersion、build-info及运行版本均为 `6.2.12.2`，主程序完整嵌入前程config和最新书签脚本。
- 最终主程序使用独立数据目录和端口启动，Agent健康、版本回报通过；精确生成的书签包含前程入口，书签扩展只有bookmarks权限，生成脚本的两项回归通过。
- 未绑定环境下，Go启动Worker和打开浏览器仍按原检查门闩拒绝。独立启动包内Worker后，其健康、PID与入口、Go状态桥及选择器数组校验通过；没有绕过运行组件检查。
- 本次测试进程与端口全部清理，未影响已有程序。没有执行覆盖安装、真实浏览器启动、真实招聘账号操作或消息实发，也没有重新打包或声称重新验证Mac。
- 本机、服务器、公网独立完整下载的安装器和ZIP大小、SHA256全部一致。公开下载页与更新接口均200，下载页包含新Windows和原Mac安装器；正式后端内部健康正常。

## 可选更新与配置保留

本次只前插一条Windows记录，更新记录从55条增加为56条；原55条内容与顺序、其它onboarding字段、整行app-config和Mac最新记录全部保持。

使用实际公开更新结果、现有前端函数及弹框源码验证：

| 当前Windows版本 | 本次提示 | 可忽略 | 岗位启动 |
| --- | --- | --- | --- |
| 6.2.12.0 / 6.2.12.1 | 可选更新 | 是 | 放行 |
| 6.2.12.2 | 无重复提示 | 无需忽略 | 放行 |
| 6.2.11.3 | 保留原有6.2.12必要升级规则 | 否 | 保留原有拦截 |

没有新增强制升级门槛。app-config最低版本值仍为 `6.2.10.1`；其整行摘要保持 `115005fb5e3dc25049ddeb8323dadcb2`。onboarding配置MD5由 `bfcf22d84adfd7a9a6412541c3ef4b74` 变为 `da14b70b65525504144d9872016ec0d2`。

## 备份与证据

服务器：`ldclouda31539`。私有发布目录：`/opt/goodhr-release/windows-platforms-6.2.12.2-89036527-20261102/`，含完整配置前后快照、精确发布SQL、并发保护回滚SQL、临时表发布及回滚演练结果、执行脚本和清单。

真实发布前在临时表执行同一发布和回滚SQL，验证两行完整恢复；正式事务对onboarding整行加锁比较，同时保护app-config。回滚仅在当前整行仍等于本次结果时恢复，防止覆盖后续调整。文件使用带摘要的唯一名称，没有覆盖或清理旧包。

正式前后后端、前端、PostgreSQL和Redis的容器ID、镜像及启动时间完全一致，没有重启服务或清理数据库。

本机证据：`D:/gh-pack/windows-platforms-62122-20261102/`，包括 `artifacts.json`、`go-validation-summary.json`、`final-smoke/package-smoke.json`、`public-assets-verification.json`、`public-update-verification.json` 和 `publication.json`。原工作目录中的未提交前端文件及未跟踪目录原样保留。
