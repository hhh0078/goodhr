<!-- 文件作用说明：记录智联6.3.4.8现场定位故障、6.3.4.9最小修复范围及跨平台验证边界。 -->

# 智联经历字段范围修复

2026-11-19 在已登录智联推荐页观察到安装版6.3.4.8启动后，AI预分析成功，但第一次定位候选人就报`ELEMENT_NOT_FOUND`；连续三次相同错误后任务停止，打招呼数为0。

## 已确认的原因

`candidate_fields.education` 原选择器`.resume-item__content.resume-card-exp`在现场实际包住基本资料、经历、隐藏提示和响应式操作按钮。读取使用可见文字`innerText`，重新定位使用卡片`hasText`；隐藏文字混入节点文字后，6.3.4.8的空白兼容仍无法匹配完整经历串。

在未点击、未滚动和未发送消息的情况下，对当前真实推荐页逐字段检查：姓名、基本资料可匹配，旧经历字段不可匹配。改用实际经历区域后，通过正式Go配置加载、FindCandidates和CandidateSelector链路，20张卡片均恰好匹配一张。

## 修改边界

唯一生产改动为智联配置的`candidate_fields.education`：

- 推荐卡片：`:scope.recommend-resume-item__inner .resume-item__experience`，只读工作与教育经历。
- 搜索卡片：`:scope.search-resume-item__inner .resume-item__content.resume-card-exp`，保留原字段范围。
- 两个入口按当前卡片类型隔离，推荐页不会退回受到界面文字污染的外层。

不修改公共流程、Worker、协议、点击、截图、自动回复、其它平台配置及系统组件。BOSS测试文件只更新其中用于智联回归的合成卡片结构，不改BOSS生产代码。

## 验证

- 同一个现有Worker中新建本地合成测试页，关闭后恢复原招聘标签页，不新建Worker、不重启浏览器、不使用页面脚本。
- 修复前回归测试失败；修复后通过。旧配置仍能稳定复现`ELEMENT_NOT_FOUND`。
- 普通、块布局、flex布局；名单换位；同名不同经历；完全重复身份拒绝操作；智联搜索页旧字段范围一致，均通过真实Camoufox合成页验证。
- 当前真实账号推荐页20张卡片只读身份定位通过，未向真实候选人发送消息。
- `go test ./...`通过；Worker测试386通过、1跳过。
- 已验证其它平台配置与修改前逐字相同，智联除`education`外的配置也逐字相同。
- Windows x64交叉编译通过。macOS由正式`package-release.sh`生成arm64与amd64通用包，Go二进制、原生入口及两种架构原生依赖随包验证。

本次配置调整没有系统专属调用；Windows与Mac使用同一份Go嵌入配置和原有Worker。Mac实际浏览器验证已完成；Windows安装运行、Intel Mac真机和其它平台真实账号长跑未验证。

本次先提供6.3.4.9安装包供用户续跑岗位验收，不自动替换正在使用的程序、不修改线上自动更新配置。

## 本地回归命令

手动确认岗位停止后，才显式指定现有Worker执行以下测试。默认测试套件跳过这个现场测试，避免接管用户页面。

```sh
GOODHR_ZHAOPIN_EXPERIENCE_TEST_WORKER_URL=http://127.0.0.1:39881 \
go test -count=1 -v ./internal/platform/zhaopin -run TestZhaopinExperienceExistingWorker
```
