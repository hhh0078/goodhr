// Package taskrunner 文件作用：按职责承载本地任务运行流程的拆分实现。
package taskrunner

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"goodhr5/local-agent-go/internal/browser"
	"goodhr5/local-agent-go/internal/cloudapi"
	"unicode/utf16"
)

// taskLog 写入本地任务日志。
// taskID 为任务 ID，level 为日志等级，msg 为日志内容。
func (r *Runner) taskLog(taskID string, level string, msg string) {
	taskID = strings.TrimSpace(taskID)
	level = strings.TrimSpace(level)
	msg = strings.TrimSpace(msg)
	if level == "" {
		level = "info"
	}
	if msg == "" {
		return
	}
	if r.db != nil && taskID != "" {
		_, _ = r.db.AddTaskLog(taskID, level, msg)
	}
}

// playSound 播放提示音文件。
// soundName 为文件名（如 success.wav），taskID 为任务 ID（用于日志）。
func (r *Runner) playSound(soundName string, taskID string) {
	filePath := filepath.Join(r.audioDir, soundName)
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		r.taskLog(taskID, "warning", "音频文件不存在或为空："+filePath)
		playCmd, cmdErr := fallbackSoundCommand()
		if cmdErr != nil {
			r.taskLog(taskID, "warning", "播放系统提示音失败："+cmdErr.Error())
			return
		}
		r.startSoundCommand(playCmd, taskID, "系统提示音")
		return
	}
	playCmd, err := soundPlayCommand(filePath)
	if err != nil {
		r.taskLog(taskID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.startSoundCommand(playCmd, taskID, soundName)
}

// startSoundCommand 启动提示音命令并记录启动后失败日志。
// cmd 为播放命令，taskID 为任务 ID，label 为提示音名称。
func (r *Runner) startSoundCommand(cmd *exec.Cmd, taskID string, label string) {
	hideCommandWindow(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		r.taskLog(taskID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.taskLog(taskID, "info", "提示音播放命令已启动："+label)
	// 非阻塞——不等待播放结束，避免卡主流程
	go func() {
		if err := cmd.Wait(); err != nil {
			detail := strings.TrimSpace(output.String())
			if detail != "" {
				r.taskLog(taskID, "warning", "提示音播放进程异常："+err.Error()+"，输出："+detail)
				return
			}
			r.taskLog(taskID, "warning", "提示音播放进程异常："+err.Error())
			return
		}
		r.taskLog(taskID, "info", "提示音播放成功："+label)
	}()
}

// soundPlayCommand 根据当前系统创建音频播放命令。
// filePath 为本地音频文件路径，返回可执行命令。
func soundPlayCommand(filePath string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("afplay"); err != nil {
			return nil, fmt.Errorf("系统未找到 afplay 播放器")
		}
		return exec.Command("afplay", filePath), nil
	case "windows":
		powershell, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
		if err != nil {
			return nil, fmt.Errorf("系统未找到 PowerShell 播放器")
		}
		escapedPath := strings.ReplaceAll(filePath, "'", "''")
		script := fmt.Sprintf(`$player = New-Object System.Media.SoundPlayer; $player.SoundLocation = '%s'; $player.PlaySync()`, escapedPath)
		return exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", powershellEncodedCommand(script)), nil
	default:
		if player, err := exec.LookPath("paplay"); err == nil {
			return exec.Command(player, filePath), nil
		}
		if player, err := exec.LookPath("aplay"); err == nil {
			return exec.Command(player, filePath), nil
		}
		return nil, fmt.Errorf("当前系统未找到可用音频播放器")
	}
}

// powershellEncodedCommand 将 PowerShell 脚本编码为 -EncodedCommand 需要的 UTF-16LE Base64。
// script 为待执行脚本，返回可直接传给 PowerShell 的编码字符串。
func powershellEncodedCommand(script string) string {
	encoded := utf16.Encode([]rune(script))
	data := make([]byte, 0, len(encoded)*2)
	for _, value := range encoded {
		data = append(data, byte(value), byte(value>>8))
	}
	return base64.StdEncoding.EncodeToString(data)
}

// fallbackSoundCommand 创建系统默认提示音命令。
// 当 success.wav/failed.wav 缺失时使用，保证用户仍能听到反馈。
func fallbackSoundCommand() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		if player, err := exec.LookPath("osascript"); err == nil {
			return exec.Command(player, "-e", "beep 1"), nil
		}
		return nil, fmt.Errorf("系统未找到 osascript 播放器")
	case "windows":
		powershell, err := lookPathAny("powershell.exe", "powershell", "pwsh.exe", "pwsh")
		if err != nil {
			return nil, fmt.Errorf("系统未找到 PowerShell 播放器")
		}
		return exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", "[console]::beep(880,180)"), nil
	default:
		return nil, fmt.Errorf("当前系统未配置默认提示音")
	}
}

// lookPathAny 返回第一个可用命令路径。
// names 为候选命令名称。
func lookPathAny(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("没有找到可用命令")
}

// taskBrowserViewport 返回任务和手动打开入口共用的固定视口尺寸。
func taskBrowserViewport() (int, int) {
	return browser.FixedViewport()
}

// sendTaskFailNotification 通知云端任务失败，由云端按任务 ID 查用户并发邮件。
// ctx 为请求上下文，taskID 为任务 ID，errorMsg 为失败原因，options 为本次任务启动参数。
func (r *Runner) sendTaskFailNotification(ctx context.Context, taskID string, errorMsg string, options StartOptions) {
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	client := cloudapi.New(baseURL)
	if err := client.SendTaskFailNotice(ctx, options.Token, taskID, errorMsg); err != nil {
		r.taskLog(taskID, "warning", "任务失败：邮件通知发送失败，错误="+err.Error())
		return
	}
	r.taskLog(taskID, "info", "任务失败：已发送邮件通知")
}

// notifyCloudTaskStopped 通知云端任务已经停止。
// taskID 为云端任务 ID，options 为本次启动参数。
func (r *Runner) notifyCloudTaskStopped(taskID string, options StartOptions) {
	r.syncCloudTaskStatus(taskID, "stopped", "任务停止", options)
}

// notifyCloudTaskCompleted 通知云端任务已经完成。
// taskID 为云端任务 ID，options 为本次启动参数。
func (r *Runner) notifyCloudTaskCompleted(taskID string, options StartOptions) {
	r.syncCloudTaskStatus(taskID, "completed", "任务完成", options)
}

// syncCloudTaskStatus 同步云端任务状态。
// taskID 为云端任务 ID，status 为云端状态，label 为任务日志前缀。
func (r *Runner) syncCloudTaskStatus(taskID string, status string, label string, options StartOptions) {
	token := strings.TrimSpace(options.Token)
	if token == "" {
		return
	}
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client := cloudapi.New(baseURL)
	r.taskLog(taskID, "info", label+"：正在同步云端状态")
	if err := client.SyncTaskStatus(ctx, token, taskID, status); err != nil {
		r.taskLog(taskID, "warning", label+"：云端状态同步失败，错误="+err.Error())
		return
	}
	r.taskLog(taskID, "info", label+"：云端状态同步成功")
}
