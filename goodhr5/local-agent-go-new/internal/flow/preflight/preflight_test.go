// Package preflight 文件作用：验证启动前检查会按任务需要使用云端业务能力和本地平台配置。
package preflight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// subscriptionCloudStub 只覆盖会员接口，其余云端能力不会在当前单元测试中调用。
type subscriptionCloudStub struct {
	Cloud
	subscription cloud.Subscription
}

type deviceBindingCloudStub struct {
	Cloud
	request cloud.AgentBindRequest
}

// BindAgent 记录启动前检查提交的设备绑定信息。
func (s *deviceBindingCloudStub) BindAgent(_ context.Context, _ string, request cloud.AgentBindRequest) (cloud.AgentBinding, error) {
	s.request = request
	return cloud.AgentBinding{MachineID: request.MachineID, BindStatus: "active"}, nil
}

// Subscription 返回测试指定的会员权限。
func (s subscriptionCloudStub) Subscription(context.Context, string) (cloud.Subscription, error) {
	return s.subscription, nil
}

// TestFreeKeywordOCRSkipsAIAndSubscription 验证只使用关键词和 OCR 时不访问 AI 或会员客户端。
func TestFreeKeywordOCRSkipsAIAndSubscription(t *testing.T) {
	checker := &Checker{}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "greeting"},
		Position: cloud.PositionSnapshot{RequiresAI: false, RequiresOCR: true},
	}
	if err := checker.checkSubscription(context.Background(), prepared); err != nil {
		t.Fatalf("免费关键词 OCR 任务不应检查会员：%v", err)
	}
	if err := checker.checkAI(context.Background(), prepared); err != nil {
		t.Fatalf("免费关键词 OCR 任务不应检查 AI：%v", err)
	}
}

// TestCheckDeviceBindingUsesStableMachineID 验证任务启动前会再次确认当前稳定设备绑定。
func TestCheckDeviceBindingUsesStableMachineID(t *testing.T) {
	cloudStub := &deviceBindingCloudStub{}
	checker := &Checker{
		Cloud: cloudStub,
		AgentBind: cloud.AgentBindRequest{
			MachineID: "goodhr-device-v1-test", AgentVersion: "6", LocalPort: 43129,
		},
	}
	prepared := &shared.PreparedTask{Request: shared.StartRequest{Token: "browser-token"}}
	if err := checker.checkDeviceBinding(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if cloudStub.request.MachineID != "goodhr-device-v1-test" || cloudStub.request.LocalPort != 43129 {
		t.Fatalf("binding request = %+v", cloudStub.request)
	}
	if prepared.MachineID != cloudStub.request.MachineID {
		t.Fatal("绑定成功的设备编号没有传入后续流程快照")
	}
}

// TestPreflightBindingCredentialsReachAutoReplyHTTP 验证正式检查顺序和HTTP客户端把真实绑定编号带到后续自动回复请求，不依赖手填PreparedTask。
func TestPreflightBindingCredentialsReachAutoReplyHTTP(t *testing.T) {
	t.Setenv(shared.AutoReplyTargetEnvironment, "")
	for _, bindingOK := range []bool{true, false} {
		t.Run(map[bool]string{true: "绑定成功", false: "绑定失败"}[bindingOK], func(t *testing.T) {
			const machineID = "synthetic-preflight-machine"
			positionReads := 0
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/auth/me":
					_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "synthetic-owner"}})
				case "/api/agents/bind":
					var request cloud.AgentBindRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.MachineID != machineID {
						t.Error("绑定没有携带实际设备编号", err)
					}
					if !bindingOK {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"error":"合成绑定拒绝"}`))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"agent": map[string]string{"machine_id": machineID}})
				case "/api/auto-reply/agent/positions":
					positionReads++
					if r.Header.Get("X-GoodHR-Machine-ID") != machineID || r.Header.Get("Authorization") != "Bearer synthetic-token" {
						t.Error("自动回复请求丢失启动前检查的设备或账号凭证")
					}
					_, _ = w.Write([]byte(`{"positions":[]}`))
				default:
					// 主动在后续岗位加载停止，前面的真实检查快照仍应完整返回；不启动任何浏览器。
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"合成岗位边界停止"}`))
				}
			}))
			defer remote.Close()
			client := cloud.New(remote.URL)
			checker := &Checker{Cloud: client, AgentBind: cloud.AgentBindRequest{MachineID: machineID}}
			result, err := checker.RunPreflightChecks(context.Background(), shared.StartRequest{
				TaskID: "synthetic-task", PositionID: "synthetic-position", TaskType: "auto_reply", Token: "synthetic-token", PlatformAccountID: "合成平台账号",
			})
			if err == nil {
				t.Fatal("合成边界没有停止检查")
			}
			if !bindingOK {
				if result.Prepared.MachineID != "" || !strings.Contains(err.Error(), "设备绑定") {
					t.Fatal("绑定失败仍授权了设备快照", err)
				}
				return
			}
			if result.Prepared.MachineID != machineID || !strings.Contains(err.Error(), "岗位配置") {
				t.Fatal("设备编号未保留到正式检查结果", err)
			}
			_, err = client.AutoReplySnapshots(context.Background(), cloud.AgentCredentials{Token: result.Prepared.Request.Token, MachineID: result.Prepared.MachineID}, "liepin")
			if err != nil || positionReads != 1 {
				t.Fatalf("检查快照无法发起后续岗位读取：calls=%d err=%v", positionReads, err)
			}
		})
	}
}

// TestLoadPlatformUsesBundledConfig 验证平台配置无需云端客户端也能从本地加载。
func TestLoadPlatformUsesBundledConfig(t *testing.T) {
	checker := &Checker{}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "greeting"},
		Position: cloud.PositionSnapshot{PlatformID: "zhaopin"},
	}
	if err := checker.loadPlatform(context.Background(), prepared); err != nil {
		t.Fatalf("读取智联本地平台配置失败：%v", err)
	}
	selector := prepared.Platform.Selectors["position.open"]
	if len(selector.Target.Selectors) == 0 ||
		selector.Target.Selectors[0].Value != "a[zp-stat-id='talent_more_jobs']" {
		t.Fatalf("启动前检查没有使用智联本地选择器：%+v", selector)
	}
}

// TestLoadPlatformAllowsZhaopinAutoReplyNewPageDocument 验证智联真实 PDF 新标签页能力可以通过启动检查。
func TestLoadPlatformAllowsZhaopinAutoReplyNewPageDocument(t *testing.T) {
	checker := &Checker{}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "auto_reply"},
		Position: cloud.PositionSnapshot{PlatformID: "zhaopin"},
	}
	if err := checker.loadPlatform(context.Background(), prepared); err != nil {
		t.Fatalf("智联真实附件能力不应再被启动检查拦截：%v", err)
	}
	if prepared.Platform.Behavior.AttachmentMode != "new_page_document" {
		t.Fatalf("智联启动检查没有加载新标签页附件模式：%s", prepared.Platform.Behavior.AttachmentMode)
	}
}

// TestAutoReplySkipsMembershipPermission 验证会员限制暂停后预检查不再拦截Plus，灰度在执行入口单独检查。
func TestAutoReplySkipsMembershipPermission(t *testing.T) {
	checker := &Checker{Cloud: subscriptionCloudStub{subscription: cloud.Subscription{
		Active: true, AllowAI: true, MemberType: "plus", AllowAutoReply: false,
	}}}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "auto_reply"},
		Position: cloud.PositionSnapshot{RequiresAI: true},
	}
	if err := checker.checkSubscription(context.Background(), prepared); err != nil {
		t.Fatalf("会员检查已暂停，灰度继续由独立入口控制：%v", err)
	}
}

// TestAutoReplyAllowsMaxPermission 验证 Max 权限通过自动回复会员检查。
func TestAutoReplyAllowsMaxPermission(t *testing.T) {
	checker := &Checker{Cloud: subscriptionCloudStub{subscription: cloud.Subscription{
		Active: true, AllowAI: true, MemberType: "max", AllowAutoReply: true,
	}}}
	prepared := &shared.PreparedTask{
		Request:  shared.StartRequest{TaskType: "auto_reply"},
		Position: cloud.PositionSnapshot{RequiresAI: true},
	}
	if err := checker.checkSubscription(context.Background(), prepared); err != nil {
		t.Fatalf("Max 会员应通过自动回复权限检查：%v", err)
	}
}

// TestValidateGreetingSetting 验证仅检查任务能启动，自动回复不能借用关闭打招呼参数绕过发送边界。
func TestValidateGreetingSetting(t *testing.T) {
	enabled, disabled := true, false
	for _, test := range []struct {
		name, taskType string
		setting        *bool
		wantError      bool
	}{
		{name: "greeting_default", taskType: "greeting"},
		{name: "greeting_enabled", taskType: "greeting", setting: &enabled},
		{name: "greeting_check_only", taskType: "greeting", setting: &disabled},
		{name: "reply_default", taskType: "auto_reply"},
		{name: "reply_enabled", taskType: "auto_reply", setting: &enabled},
		{name: "reply_check_only_rejected", taskType: "auto_reply", setting: &disabled, wantError: true},
		{name: "missing_type_rejected", setting: &disabled, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared := &shared.PreparedTask{Request: shared.StartRequest{
				TaskID: "task-1", PositionID: "position-1", Token: "fixture",
				TaskType: test.taskType, EnableGreet: test.setting, PlatformAccountID: "fixture-account",
			}}
			err := validateRequest(context.Background(), prepared)
			if (err != nil) != test.wantError {
				t.Fatalf("发送开关边界不符，错误：%v", err)
			}
		})
	}
}

// checkOnlyPositionCloudStub 提供包含消息和资料索要配置的岗位快照。
type checkOnlyPositionCloudStub struct{ Cloud }

// Position 返回测试岗位，不能覆盖本次请求明确关闭的发送开关。
func (checkOnlyPositionCloudStub) Position(context.Context, string, string) (cloud.PositionSnapshot, error) {
	return cloud.PositionSnapshot{ProfileID: "profile-1", GreetMessage: "测试消息", RequestPhone: true, RequestWechat: true, RequestResume: true}, nil
}

// TestLoadPositionPreservesDisabledGreeting 验证冻结岗位配置时不会让明确关闭的发送开关失效。
func TestLoadPositionPreservesDisabledGreeting(t *testing.T) {
	disabled := false
	prepared := &shared.PreparedTask{Request: shared.StartRequest{
		TaskID: "task-1", PositionID: "position-1", TaskType: "greeting",
		Token: "fixture", EnableGreet: &disabled,
	}}
	checker := &Checker{Cloud: checkOnlyPositionCloudStub{}}
	if err := checker.loadPosition(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Request.EnableGreet == nil || *prepared.Request.EnableGreet || prepared.Request.TaskType != "greeting" || prepared.Request.ProfileID != "profile-1" {
		t.Fatal("岗位快照覆盖了任务请求的只检查设置")
	}
}

// TestAutoReplyRequiresExplicitAccount 验证所有自动回复启动入口都必须明确绑定当前招聘账号。
func TestAutoReplyRequiresExplicitAccount(t *testing.T) {
	prepared := &shared.PreparedTask{Request: shared.StartRequest{TaskID: "task", PositionID: "position", TaskType: "auto_reply", Token: "token"}}
	if err := validateRequest(context.Background(), prepared); err == nil {
		t.Fatal("自动回复不应接受缺失账号绑定")
	}
	prepared.Request.PlatformAccountID = "账号甲"
	if err := validateRequest(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if shared.AutoReplyAccountKey("boss", "账号甲") == shared.AutoReplyAccountKey("boss", "账号乙") || shared.AutoReplyAccountKey("boss", "账号甲") == shared.AutoReplyAccountKey("liepin", "账号甲") {
		t.Fatal("账号隔离键未包含平台或账号")
	}
}
