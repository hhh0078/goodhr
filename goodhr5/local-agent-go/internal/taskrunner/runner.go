// Package taskrunner 负责管理 Go 版本本地任务启动、停止和运行锁。
package taskrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/ocr"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/power"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const defaultScanRounds = 3
const defaultMaxItemsPerRound = 0
const defaultScrollDistance = 720
const defaultScrollDistanceJitter = 160
const defaultCandidatePipelineConcurrency = 5
const stopGracefulTimeout = 10 * time.Second
const stopPollInterval = 500 * time.Millisecond
const candidateTotalTimeout = 3 * time.Minute
const aiPrecheckTimeout = 60 * time.Second
const detailFetchTimeout = 60 * time.Second
const ocrRecognizeTimeout = 60 * time.Second
const aiDetailTimeout = 120 * time.Second
const aiScoreTimeout = 60 * time.Second
const greetActionTimeout = 30 * time.Second
const cloudCandidateSyncTimeout = 30 * time.Second
const cloudStatsSyncTimeout = 15 * time.Second
const detailCloseTimeout = 10 * time.Second
const overlayActionTimeout = 5 * time.Second
const pendingAIVisionOutputTimeout = 3 * time.Minute
const pendingAIVisionDecisionKey = "_pending_ai_vision_decision"
const sleepMonitorInterval = 30 * time.Second
const sleepResumeThreshold = 2 * time.Minute

// pageEntryCheckAttempts 是入口页面加载检查的最大次数。
var pageEntryCheckAttempts = 10

// pageEntryCheckDelay 是每次入口页面检查前的等待时间。
var pageEntryCheckDelay = time.Second

// currentPositionCheckAttempts 是当前岗位名称读取的最大次数。
var currentPositionCheckAttempts = 10

// currentPositionCheckDelay 是每次读取当前岗位名称前的等待时间。
var currentPositionCheckDelay = time.Second

// BrowserWorker 表示任务运行器需要的浏览器 Worker 能力。
type BrowserWorker interface {
	Start(ctx context.Context) (browser.WorkerStatus, error)
	Call(ctx context.Context, path string, payload any) (map[string]any, error)
}

// OCRRecognizer 表示任务运行器需要的 OCR 能力。
type OCRRecognizer interface {
	Recognize(ctx context.Context, imagePath string) (ocr.Result, error)
}

// Runner 是本地任务运行器。
type Runner struct {
	db             *localdb.DB
	worker         BrowserWorker
	ocr            OCRRecognizer
	profilesDir    string
	downloadsDir   string
	screenshotsDir string
	audioDir       string
	cloudAPIBase   string
	mu             sync.Mutex
	running        map[string]*runState
	userStopped    map[string]bool
	powerGuard     power.Inhibitor
	sleepCancel    context.CancelFunc
}

// runState 保存单个运行任务的控制句柄。
type runState struct {
	cancel         context.CancelFunc
	progress       Progress
	emailForNotify string // 失败通知邮箱
	options        StartOptions
	cancelReason   string
	runGreeted     int // 本次运行已打招呼数量
	// 摸鱼休息状态
	restMaxTimes  int
	restUsed      int
	restNextAfter int
	restSinceLast int
}

// Progress 表示任务运行进度。
type Progress struct {
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Round       int    `json:"round"`
	TotalRounds int    `json:"total_rounds"`
	UpdatedAt   string `json:"updated_at"`
}

// TaskRuntimeSnapshot 保存一次任务运行开始时从云端读取到的完整快照。
type TaskRuntimeSnapshot struct {
	Task           localdb.Task
	Options        StartOptions
	PlatformConfig cloudapi.PlatformConfig
	Preferences    map[string]any
	AIConfig       localdb.AIConfig
}

// candidateVisibleRuntime 表示平台支持把候选人卡片滚动到可见区域。
type candidateVisibleRuntime interface {
	// EnsureCandidateVisible 确保指定候选人卡片滚动到当前可见区域。
	EnsureCandidateVisible(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error
}

// platformExecutor 适配平台 runtime 调用 Worker 和写任务日志。
type platformExecutor struct {
	runner *Runner
	taskID string
}

// Post 调用浏览器 Worker。
// ctx 为请求上下文，path 为 Worker 路径，payload 为请求体。
func (e platformExecutor) Post(ctx context.Context, path string, payload any) (map[string]any, error) {
	return e.runner.worker.Call(ctx, path, payload)
}

// Log 写入任务日志。
// level 为日志级别，message 为日志内容。
func (e platformExecutor) Log(level string, message string) {
	e.runner.taskLog(e.taskID, level, message)
}

// Delay 按业务动作等待指定秒数。
// ctx 为请求上下文，label 为动作名称，seconds 为等待秒数。
func (e platformExecutor) Delay(ctx context.Context, label string, seconds float64) error {
	if seconds <= 0 {
		return nil
	}
	e.runner.taskLog(e.taskID, "info", fmt.Sprintf("%s等待 %.1f 秒", label, seconds))
	return sleepWithContext(ctx, time.Duration(seconds*float64(time.Second)))
}

// StartOptions 表示本地任务启动参数（含模拟人工操作的各类延时）。
type StartOptions struct {
	CloudAPIBase   string
	Token          string
	AIConfig       localdb.AIConfig
	EnableGreet    bool
	GreetDelayMin  float64
	GreetDelayMax  float64
	GreetRetries   int
	ScanRounds     int
	MaxItems       int
	ScrollDistance int
	PageReadyDelay int
	// 以下为模拟人工操作的延时配置（随机范围）
	ScrollDelayMin           int // 两次滚动之间的延时（秒）
	ScrollDelayMax           int
	ListViewDelayMin         float64 // 查看候选人列表后的停留（秒）
	ListViewDelayMax         float64
	DetailViewDelayMin       float64 // 查看候选人详情后的停留（秒）
	DetailViewDelayMax       float64
	DetailOpenProbability    int     // 打开详情概率（0-100）
	detailOpenProbabilitySet bool    // 是否已从个人配置读取打开详情概率
	DetailOpenDelayMin       float64 // 打开详情前的延时（秒）
	DetailOpenDelayMax       float64
	DetailCloseDelayMin      float64 // 关闭详情前的延时（秒）
	DetailCloseDelayMax      float64
	GreetBeforeDelayMin      float64 // 打招呼前点击按钮的延时（秒）
	GreetBeforeDelayMax      float64
	RestAfterCandidatesMin   int // 处理多少候选人后摸鱼休息
	RestAfterCandidatesMax   int
	RestTimesMin             int // 整个任务最多摸鱼休息几次
	RestTimesMax             int
	RestDurationMin          float64 // 每次摸鱼休息多少分钟
	RestDurationMax          float64
	// 提示音和通知
	EnableSound    bool   `json:"enable_sound"`     // 是否开启提示音
	EmailForNotify string `json:"email_for_notify"` // 失败通知邮箱
}

// New 创建本地任务运行器。
// db 为本地 SQLite 数据库，worker 为浏览器 Worker 管理器，profilesDir、downloadsDir 和 screenshotsDir 为本机浏览器目录。
func New(db *localdb.DB, worker BrowserWorker, ocr OCRRecognizer, profilesDir string, downloadsDir string, screenshotsDir string, audioDir string, cloudAPIBase string) *Runner {
	return &Runner{db: db, worker: worker, ocr: ocr, profilesDir: profilesDir, downloadsDir: downloadsDir, screenshotsDir: screenshotsDir, audioDir: audioDir, cloudAPIBase: cloudAPIBase, running: map[string]*runState{}, userStopped: map[string]bool{}}
}

// safePathName 清理文件夹名中的危险字符。
// value 为原始名称，返回适合本机文件系统使用的名称。
func safePathName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var builder strings.Builder
	for _, item := range value {
		if unicode.IsLetter(item) || unicode.IsDigit(item) || item == '-' || item == '_' || item == '.' {
			builder.WriteRune(item)
			continue
		}
		builder.WriteRune('_')
	}
	result := strings.Trim(builder.String(), "._ ")
	if result == "" {
		return "default"
	}
	if len(result) > 80 {
		return result[:80]
	}
	return result
}

// batchProcessResult 表示一批候选人的流水线处理结果。
type batchProcessResult struct {
	Saved   int
	Skipped int
	Greeted int
	Failed  int
}

// pendingAIDecisionResult 表示后台等待完整 AI 输出的结果。
type pendingAIDecisionResult struct {
	Decision localai.Decision
	Err      error
}

// candidatePipelineResult 表示单个候选人后台处理结果。
type candidatePipelineResult struct {
	Index          int
	Candidate      map[string]any
	Skipped        int
	Err            error
	DetailDecision *localai.Decision
}

// keywordMatchState 保存一次关键词匹配结果。
type keywordMatchState struct {
	Keywords []string
	Excludes []string
	Matched  []string
	Excluded []string
	Text     string
	AndMode  bool
}

// sleepWithContext 带停止信号地等待。
// ctx 为请求上下文，duration 为等待时长。
func sleepWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// maxInt 返回两个整数中的较大值。
// a 和 b 为参与比较的整数。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 返回两个整数中的较小值。
// a 和 b 为参与比较的整数。
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// scanRounds 返回旧版扫描进度总数。
// options 为任务启动参数，保留该函数用于兼容前端旧进度字段。
func scanRounds(options StartOptions) int {
	if options.ScanRounds <= 0 {
		return defaultScanRounds
	}
	if options.ScanRounds > 20 {
		return 20
	}
	return options.ScanRounds
}

// emptyLoadLimit 返回连续未加载到新候选人的停止阈值。
// options 为任务启动参数，沿用 ScanRounds 字段作为空加载保护次数。
func emptyLoadLimit(options StartOptions) int {
	return scanRounds(options)
}

// maxItemsPerLoad 返回每次最多提取候选人数，0 表示读取当前 DOM 中全部候选人。
// options 为任务启动参数。
func maxItemsPerLoad(options StartOptions) int {
	if options.MaxItems <= 0 {
		return defaultMaxItemsPerRound
	}
	return options.MaxItems
}

// maxItemsPerRound 返回旧版每轮最多提取候选人数。
// options 为任务启动参数，保留该函数用于兼容旧测试和旧调用。
func maxItemsPerRound(options StartOptions) int {
	return maxItemsPerLoad(options)
}

// scrollDistance 返回每轮滚动距离。
// options 为任务启动参数。
func scrollDistance(options StartOptions) int {
	if options.ScrollDistance <= 0 {
		return defaultScrollDistance
	}
	if options.ScrollDistance > 3000 {
		return 3000
	}
	return options.ScrollDistance
}

// detailOpenProbability 返回关键词详情阶段的打开概率。
// options 为任务启动参数，未读取到个人配置时默认 100，避免旧任务突然不打开详情。
func detailOpenProbability(options StartOptions) int {
	if !options.detailOpenProbabilitySet && options.DetailOpenProbability <= 0 {
		return 100
	}
	if options.DetailOpenProbability < 0 {
		return 0
	}
	if options.DetailOpenProbability > 100 {
		return 100
	}
	return options.DetailOpenProbability
}

// shouldOpenDetailByProbability 判断本次是否按个人概率打开详情。
// options 为任务启动参数，返回 true 表示继续打开候选人详情。
func shouldOpenDetailByProbability(options StartOptions) bool {
	probability := detailOpenProbability(options)
	if probability >= 100 {
		return true
	}
	if probability <= 0 {
		return false
	}
	return rand.Intn(100) < probability
}

// randomFloatRange 从浮点范围中随机一个值。
// minValue 为最小值，maxValue 为最大值。
func randomFloatRange(minValue float64, maxValue float64) float64 {
	if minValue >= maxValue || maxValue <= 0 {
		return minValue
	}
	return minValue + float64(rand.Intn(int((maxValue-minValue)*100+1)))/100.0
}

// randomIntRange 从整数范围中随机一个值。
// minValue 为最小值，maxValue 为最大值。
func randomIntRange(minValue int, maxValue int) int {
	if minValue >= maxValue || maxValue <= 0 {
		return minValue
	}
	return minValue + rand.Intn(maxValue-minValue+1)
}

// delayRandomRange 随机等待指定范围秒数，写任务日志让前端可见。
// ctx 为运行上下文，taskID 为任务 ID，label 为动作名称，minSeconds 和 maxSeconds 为秒数范围。
// r 为 Runner，传 nil 时不写日志。
func (r *Runner) delayRandomRange(ctx context.Context, taskID string, label string, minSeconds float64, maxSeconds float64) error {
	if maxSeconds <= 0 {
		return nil
	}
	seconds := randomFloatRange(minSeconds, maxSeconds)
	if seconds <= 0 {
		return nil
	}
	if r != nil && taskID != "" {
		r.taskLog(taskID, "info", fmt.Sprintf("模拟人工操作：%s，等待 %.1f 秒", label, seconds))
	}
	return sleepWithContext(ctx, time.Duration(seconds*float64(time.Second)))
}

// randomScrollDistance 返回带随机抖动的滚动距离。
// options 为任务启动参数，默认围绕 720 像素上下随机，避免每轮滚动完全一致。
func randomScrollDistance(options StartOptions) int {
	base := scrollDistance(options)
	minDistance := maxInt(120, base-defaultScrollDistanceJitter)
	maxDistance := base + defaultScrollDistanceJitter
	if maxDistance <= minDistance {
		return minDistance
	}
	return minDistance + rand.Intn(maxDistance-minDistance+1)
}

// pageReadyDelay 返回提取候选人前等待页面稳定的时间。
// options 为任务启动参数。
func pageReadyDelay(options StartOptions) time.Duration {
	if options.PageReadyDelay > 0 {
		return time.Duration(options.PageReadyDelay) * time.Millisecond
	}
	return 10 * time.Second
}

// statusMessage 返回任务状态中文说明。
// status 为任务状态。
func statusMessage(status string) string {
	switch status {
	case "pending":
		return "任务等待开始"
	case "running":
		return "任务正在运行"
	case "completed":
		return "任务已完成"
	case "failed":
		return "任务运行失败"
	case "stopped":
		return "任务已停止"
	default:
		return "任务状态未知"
	}
}

// isTerminalStage 判断进度阶段是否已经结束。
// stage 为当前进度阶段。
func isTerminalStage(stage string) bool {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "completed", "failed", "stopped":
		return true
	default:
		return false
	}
}

// boolFromMap 从 map 中读取布尔值。
// item 为原始字典，key 为字段名。
func boolFromMap(item map[string]any, key string) bool {
	if item == nil {
		return false
	}
	if value, ok := item[key].(bool); ok {
		return value
	}
	return false
}

// mapList 将任意值转换为 map 列表。
// value 为原始值。
func mapList(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if candidate, ok := item.(map[string]any); ok {
			result = append(result, candidate)
		}
	}
	return result
}

// stringFromMap 从 map 中读取字符串。
// item 为原始字典，key 为字段名。
func stringFromMap(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	value, _ := item[key].(string)
	return strings.TrimSpace(value)
}

// mapValue 将任意值转换为 map。
// value 为原始值。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok && item != nil {
		return item
	}
	return map[string]any{}
}

// mapFromAny 将任意值转换为 map。
// value 为原始值。
func mapFromAny(value any) map[string]any {
	return mapValue(value)
}

// workerDataMap 从 Worker 返回中读取 data 字典。
// result 为 Worker 返回 JSON。
func workerDataMap(result map[string]any) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	if data, ok := result["data"].(map[string]any); ok {
		return data
	}
	return result
}

// firstNonEmptyString 返回第一个非空字符串。
// values 为候选字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// firstStringFromAny 从任意数组中读取第一个字符串。
// value 为原始数组值。
func firstStringFromAny(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			return text
		}
	}
	return ""
}

// logTextPreview 返回适合写入日志的文本摘要。
// text 为原始文本，limit 为最大字符数。
func logTextPreview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "空"
	}
	lines := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\r' || r == '\n' || r == '\t'
	})
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if item := strings.TrimSpace(line); item != "" {
			parts = append(parts, item)
		}
	}
	preview := strings.Join(parts, " / ")
	if limit <= 0 {
		limit = 800
	}
	runes := []rune(preview)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return preview
}

// intFromMap 从 map 中读取整数。
// item 为原始字典，key 为字段名。
func intFromMap(item map[string]any, key string) int {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

// intFromMapOr 从 map 中读取整数，空值使用默认值。
// item 为原始字典，key 为字段名，fallback 为默认值。
func intFromMapOr(item map[string]any, key string, fallback int) int {
	if item == nil {
		return fallback
	}
	if _, ok := item[key]; !ok {
		return fallback
	}
	value := intFromMap(item, key)
	if value == 0 {
		return fallback
	}
	return value
}

// floatFromMapOr 从 map 中读取浮点数，空值使用默认值。
// item 为原始字典，key 为字段名，fallback 为默认值。
func floatFromMapOr(item map[string]any, key string, fallback float64) float64 {
	if item == nil {
		return fallback
	}
	value, ok := item[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	}
	return fallback
}

// floatFromMap 从 map 中读取浮点数。
// item 为原始字典，key 为字段名。
// float64Value 从任意值中读取 float64，为空时返回默认值。
func float64Value(value any, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func floatFromMap(item map[string]any, key string) float64 {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		parsed, _ := value.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed
	default:
		return 0
	}
}

// clampInt 将整数限制在指定范围内。
// value 为原始数值，min 和 max 为上下限。
func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// parseLooseInt 从字符串中读取整数。
// value 为原始字符串，解析失败返回 0。
func parseLooseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}
