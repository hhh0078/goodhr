<!-- 文件作用说明：记录Windows 6.2.12.3暂停打招呼穿插自动回复的最小改动、安装文件、验证与回退材料。 -->

# Windows 6.2.12.3 暂停穿插自动回复

2026年11月2日，按用户明确要求，只注释本地程序打招呼任务进入自动回复检查的调用，直接运行既有主动打招呼流程，并发布Windows可选更新。

## 最小代码改动

构建来源为干净提交 `eefae920ca5d2a40d0448724ae772450946efef6`，基于上一版 `89036527`。业务源码仅修改 `local-agent-go-new/internal/flow/lifecycle/runner.go` 的greeting分支：注释 `r.runGreetingWithAutoReply(...)`，改用 `r.greeting.Run(...)`。

不再在每个候选人前执行自动回复检查，因此不会被“读取自动回复岗位列表失败：设备编号不能为空”连续三次阻断。没有补设备编号、重写自动回复或删除相关实现；独立auto_reply任务入口未改动。前程无忧推荐/搜索页适配、Worker、平台配置、依赖、安装器脚本均保持上一版。

修补已推送并合入dev。main本来已直接调用 `r.greeting.Run(...)`，无需把dev中尚未上线的其它功能整包合入main；本记录另合入main。

## 正式文件

| 文件 | 字节数 | SHA256 |
| --- | ---: | --- |
| [Windows安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.3-windows-x64-cbe54428.exe) | 31,517,598 | `cbe54428bd8cc2fe4eda3fe944149f3eeda4c8c418c82693ce4a446757290551` |
| [Windows ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.3-windows-x64-af0a7609.zip) | 40,650,418 | `af0a7609d59b4032d10206497e62165e329d7c2b0d478c531a282d7adeea9def` |

更新记录使用EXE安装器及其SHA256。ZIP是完整分发目录，不作为自动更新入口。主程序SHA256为 `a18c82fd641c385b9db4c6369ab75bbd93b3b5f9f90422b5da16d6d8281f6c36`。

复用 `scripts/package-windows.ps1 -Version 6.2.12.3 build`，npm使用 `registry.npmmirror.com`，Go使用 `goproxy.cn,direct`；没有升级依赖。正式环境地址为 `https://goodhr.58it.cn`，Go元数据为Windows amd64、CGO关闭、`vcs.modified=false`。

## 验证与边界

- Go全量测试与vet通过：39个测试包通过，含子用例1,197项通过、20项环境跳过，零失败。
- Worker测试234项：232通过、2项跳过，零失败；未修改Worker源码。
- Windows正式编译及macOS arm64/amd64交叉编译通过。只发布Windows，Mac真机运行本次未验证。
- ZIP全部2,247个文件CRC及与最终目录逐文件摘要比对通过；安装器版本、build-info和运行版本均为6.2.12.3，仍完整嵌入前程配置与书签资源。
- 最终包使用隔离数据目录和端口完成Agent、独立包内Worker健康、Go状态桥、书签生成和选择器数组校验；未绑定的浏览器检查门闩保持有效。全部测试进程与端口清理，原有Agent保持不变。
- 没有覆盖安装或操作真实招聘账号，也没有实发招呼消息。正常打招呼不再进入自动回复的结论来自唯一调用点变更和完整编译验证；不把隔离启动写成真实账号验收。
- 安装器与ZIP经公网完整下载核对大小、SHA256；更新接口和下载页回读通过。

## 更新策略与回退

仅前插一条Windows6.2.12.3更新记录，原56条更新记录内容和顺序全部保留，共57条。Mac仍为原6.2.12.2，整行app-config、其它onboarding字段和运行组件配置保持。

使用公开结果及真实前端判断验证：Windows6.2.12.0、6.2.12.1、6.2.12.2可忽略更新并继续启动岗位；6.2.12.3不重复提示；更旧版本既有6.2.12必要升级规则不变。

服务器发布目录为 `/opt/goodhr-release/windows-no-reply-6.2.12.3-eefae920-20261102/`。执行前保存配置快照和精确回滚SQL，在临时表执行同一发布及回滚并确认整行恢复；真实事务使用整行并发比较与锁，避免覆盖其它调整。历史安装包全部保留，前后端、PostgreSQL、Redis容器身份未变，没有重启服务或迁移数据库。

本机完整证据与产物保存在 `D:/gh-pack/windows-no-reply-62123-20261102/`，含 `artifacts.json`、`go-test.jsonl`、`go-vet.log`、`final-smoke/package-smoke.json`、`public-assets-verification.json` 和 `public-update-verification.json`。原工作目录中的未提交改动原样保留。
