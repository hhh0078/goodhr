// Package preflight 按固定顺序执行任务启动前检查并返回可复用快照。
package preflight

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// Cloud 定义启动前检查使用的云端能力。
type Cloud interface {
	ValidateSession(context.Context, string) (cloud.UserSession, error)
	Subscription(context.Context, string) (cloud.Subscription, error)
	Position(context.Context, string, string) (cloud.PositionSnapshot, error)
	PlatformConfig(context.Context, string, string) (model.Config, error)
	EffectiveAI(context.Context, string) (cloud.AIConfig, error)
}

// Runtime 定义启动前检查使用的运行组件能力。
type Runtime interface {
	CheckNode() error
	CheckWorkerBuild() error
	EnsureWorker(context.Context) error
}

// Browser 定义启动前检查使用的 Worker 状态能力。
type Browser interface {
	Health(context.Context) error
	BrowserStatus(context.Context) (contract.BrowserStatus, error)
	RuntimeStatus(context.Context) (contract.WorkerRuntimeStatus, error)
}

// Storage 定义启动前检查使用的 SQLite 能力。
type Storage interface {
	Ready(context.Context) error
	TaskExists(context.Context, string) (bool, error)
}

// Profiles 定义启动前检查使用的 Profile 能力。
type Profiles interface {
	Path(string) (string, error)
	Available(string, string) error
}

// AI 定义启动前检查使用的 AI 配置校验能力。
type AI interface {
	Ready(cloud.AIConfig) error
}

// OCR 定义启动前检查使用的 OCR 状态能力。
type OCR interface {
	Ready() error
}

// Power 定义启动前检查使用的系统防睡眠能力。
type Power interface {
	Available() error
}

// StepResult 表示一个启动前检查步骤结果。
type StepResult struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	Optional   bool   `json:"optional"`
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
}

// Result 保存检查步骤和后续流程直接复用的数据。
type Result struct {
	Prepared shared.PreparedTask `json:"prepared"`
	Steps    []StepResult        `json:"steps"`
}

// Checker 组装全部启动前检查依赖。
type Checker struct {
	Cloud       Cloud
	Runtime     Runtime
	Browser     Browser
	Storage     Storage
	Profiles    Profiles
	AI          AI
	OCR         OCR
	Power       Power
	Directories []string
	Logger      shared.Logger
}

type checkStep struct {
	name     string
	optional bool
	run      func(context.Context, *shared.PreparedTask) error
}

// RunPreflightChecks 按顺序执行全部检查，必需步骤失败立即停止。
func (c *Checker) RunPreflightChecks(ctx context.Context, request shared.StartRequest) (Result, error) {
	prepared := shared.PreparedTask{Request: request}
	steps := c.steps()
	result := Result{Prepared: prepared, Steps: make([]StepResult, 0, len(steps))}
	for _, step := range steps {
		startedAt := time.Now()
		c.log(request.TaskID, step.name, "start", startedAt, nil)
		err := step.run(ctx, &prepared)
		stepResult := StepResult{
			Name:       step.name,
			Success:    err == nil,
			Optional:   step.optional,
			DurationMS: time.Since(startedAt).Milliseconds(),
		}
		if err != nil {
			stepResult.Message = err.Error()
			c.log(request.TaskID, step.name, "failed", startedAt, err)
			result.Steps = append(result.Steps, stepResult)
			result.Prepared = prepared
			if !step.optional {
				return result, fmt.Errorf("启动前检查 %s 失败：%w", step.name, err)
			}
			continue
		}
		c.log(request.TaskID, step.name, "success", startedAt, nil)
		result.Steps = append(result.Steps, stepResult)
	}
	result.Prepared = prepared
	return result, nil
}

// steps 返回固定顺序的启动前检查列表。
func (c *Checker) steps() []checkStep {
	return []checkStep{
		{name: "validate_request", run: validateRequest},
		{name: "check_local_program", run: c.checkLocalProgram},
		{name: "check_cloud_session", run: c.checkCloudSession},
		{name: "check_subscription", run: c.checkSubscription},
		{name: "load_position_snapshot", run: c.loadPosition},
		{name: "load_platform_config", run: c.loadPlatform},
		{name: "check_profile", run: c.checkProfile},
		{name: "check_task_conflict", run: c.checkTaskConflict},
		{name: "check_node_runtime", run: c.checkNode},
		{name: "check_worker_build", run: c.checkWorkerBuild},
		{name: "check_worker", run: c.checkWorker},
		{name: "check_cloakbrowser", run: c.checkCloakBrowser},
		{name: "check_local_storage", run: c.checkStorage},
		{name: "check_required_ai", run: c.checkAI},
		{name: "check_required_ocr", run: c.checkOCR},
		{name: "check_power_guard", optional: true, run: c.checkPower},
	}
}

// validateRequest 校验任务统一入口参数。
func validateRequest(_ context.Context, prepared *shared.PreparedTask) error {
	request := prepared.Request
	if strings.TrimSpace(request.TaskID) == "" {
		return fmt.Errorf("task_id 不能为空")
	}
	if strings.TrimSpace(request.PositionID) == "" {
		return fmt.Errorf("position_id 不能为空")
	}
	if request.TaskType != "greeting" && request.TaskType != "auto_reply" {
		return fmt.Errorf("task_type 只支持 greeting 或 auto_reply")
	}
	if strings.TrimSpace(request.Token) == "" {
		return fmt.Errorf("登录令牌不能为空")
	}
	return nil
}

// checkLocalProgram 检查本地目录是否存在且为目录。
func (c *Checker) checkLocalProgram(_ context.Context, _ *shared.PreparedTask) error {
	for _, directory := range c.Directories {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("本地目录不可用：%s", directory)
		}
	}
	return nil
}

// checkCloudSession 检查云端登录态。
func (c *Checker) checkCloudSession(ctx context.Context, prepared *shared.PreparedTask) error {
	session, err := c.Cloud.ValidateSession(ctx, prepared.Request.Token)
	prepared.Session = session
	return err
}

// checkSubscription 检查会员状态。
func (c *Checker) checkSubscription(ctx context.Context, prepared *shared.PreparedTask) error {
	subscription, err := c.Cloud.Subscription(ctx, prepared.Request.Token)
	prepared.Subscription = subscription
	return err
}

// loadPosition 读取并冻结岗位配置。
func (c *Checker) loadPosition(ctx context.Context, prepared *shared.PreparedTask) error {
	position, err := c.Cloud.Position(ctx, prepared.Request.Token, prepared.Request.PositionID)
	if err != nil {
		return err
	}
	prepared.Position = position
	if prepared.Request.ProfileID == "" {
		prepared.Request.ProfileID = position.ProfileID
	}
	return nil
}

// loadPlatform 读取平台配置并检查本地适配是否存在。
func (c *Checker) loadPlatform(ctx context.Context, prepared *shared.PreparedTask) error {
	cfg, err := c.Cloud.PlatformConfig(ctx, prepared.Request.Token, prepared.Position.PlatformID)
	if err != nil {
		return err
	}
	if _, err := platform.RuntimeFor(prepared.Position.PlatformID); err != nil {
		return err
	}
	prepared.Platform = cfg
	return nil
}

// checkProfile 创建并检查 Profile 路径。
func (c *Checker) checkProfile(_ context.Context, prepared *shared.PreparedTask) error {
	path, err := c.Profiles.Path(prepared.Request.ProfileID)
	prepared.ProfilePath = path
	return err
}

// checkTaskConflict 检查同 Profile 是否已经被其他任务占用。
func (c *Checker) checkTaskConflict(ctx context.Context, prepared *shared.PreparedTask) error {
	if err := c.Profiles.Available(prepared.Request.ProfileID, prepared.Request.TaskID); err != nil {
		return err
	}
	exists, err := c.Storage.TaskExists(ctx, prepared.Request.TaskID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("task_id 已经使用过，请为本次运行生成新的编号")
	}
	return nil
}

// checkNode 检查 Node.js 运行时。
func (c *Checker) checkNode(_ context.Context, _ *shared.PreparedTask) error {
	return c.Runtime.CheckNode()
}

// checkWorkerBuild 检查 TypeScript 编译产物。
func (c *Checker) checkWorkerBuild(_ context.Context, _ *shared.PreparedTask) error {
	return c.Runtime.CheckWorkerBuild()
}

// checkWorker 启动并检查 Browser Worker。
func (c *Checker) checkWorker(ctx context.Context, _ *shared.PreparedTask) error {
	if err := c.Runtime.EnsureWorker(ctx); err != nil {
		return err
	}
	return c.Browser.Health(ctx)
}

// checkCloakBrowser 通过 Worker 会话接口确认 CloakBrowser 模块可调用。
func (c *Checker) checkCloakBrowser(ctx context.Context, _ *shared.PreparedTask) error {
	if _, err := c.Browser.BrowserStatus(ctx); err != nil {
		return err
	}
	status, err := c.Browser.RuntimeStatus(ctx)
	if err != nil {
		return err
	}
	if !status.Installed {
		return fmt.Errorf("CloakBrowser 增强浏览器还没安装，请先执行 cloakbrowser install")
	}
	return nil
}

// checkStorage 检查 SQLite。
func (c *Checker) checkStorage(ctx context.Context, _ *shared.PreparedTask) error {
	return c.Storage.Ready(ctx)
}

// checkAI 仅在任务需要 AI 时校验配置。
func (c *Checker) checkAI(ctx context.Context, prepared *shared.PreparedTask) error {
	if !prepared.Position.RequiresAI && prepared.Request.TaskType != "auto_reply" {
		return nil
	}
	config, err := c.Cloud.EffectiveAI(ctx, prepared.Request.Token)
	if err != nil {
		return err
	}
	if prepared.Position.AIOptions.GreetScoreThreshold > 0 {
		config.ScoreThreshold = prepared.Position.AIOptions.GreetScoreThreshold
	}
	if strings.TrimSpace(prepared.Position.AIOptions.GreetPrompt) != "" {
		config.SystemPrompt = prepared.Position.AIOptions.GreetPrompt
	}
	if strings.TrimSpace(prepared.Position.AIOptions.ReplyPrompt) != "" {
		config.ReplySystemPrompt = prepared.Position.AIOptions.ReplyPrompt
	}
	prepared.Position.AI = config
	return c.AI.Ready(prepared.Position.AI)
}

// checkOCR 仅在岗位明确需要 OCR 时检查组件。
func (c *Checker) checkOCR(_ context.Context, prepared *shared.PreparedTask) error {
	if !prepared.Position.RequiresOCR {
		return nil
	}
	return c.OCR.Ready()
}

// checkPower 检查系统防睡眠能力，失败时允许任务降级继续。
func (c *Checker) checkPower(_ context.Context, _ *shared.PreparedTask) error {
	return c.Power.Available()
}

// log 输出启动前检查步骤日志。
func (c *Checker) log(taskID string, step string, status string, startedAt time.Time, err error) {
	if c.Logger != nil {
		c.Logger.Step(taskID, "preflight", step, status, startedAt, err)
	}
}
