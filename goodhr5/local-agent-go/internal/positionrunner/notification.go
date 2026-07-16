// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

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

// positionLog 写入本地岗位运行日志。
// positionID 为岗位运行 ID，level 为日志等级，msg 为日志内容。
func (r *Runner) positionLog(positionID string, level string, msg string) {
	positionID = strings.TrimSpace(positionID)
	level = strings.TrimSpace(level)
	msg = strings.TrimSpace(msg)
	if level == "" {
		level = "info"
	}
	if msg == "" {
		return
	}
	if r.db != nil && positionID != "" {
		_, _ = r.db.AddPositionLog(positionID, level, msg)
	}
}

// playSound 播放提示音文件。
// soundName 为文件名（如 success.wav），positionID 为岗位运行 ID（用于日志）。
func (r *Runner) playSound(soundName string, positionID string) {
	filePath := filepath.Join(r.audioDir, soundName)
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		r.positionLog(positionID, "warning", "音频文件不存在或为空："+filePath)
		playCmd, cmdErr := fallbackSoundCommand()
		if cmdErr != nil {
			r.positionLog(positionID, "warning", "播放系统提示音失败："+cmdErr.Error())
			return
		}
		r.startSoundCommand(playCmd, positionID, "系统提示音")
		return
	}
	playCmd, err := soundPlayCommand(filePath)
	if err != nil {
		r.positionLog(positionID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.startSoundCommand(playCmd, positionID, soundName)
}

// startSoundCommand 启动提示音命令并记录启动后失败日志。
// cmd 为播放命令，positionID 为岗位运行 ID，label 为提示音名称。
func (r *Runner) startSoundCommand(cmd *exec.Cmd, positionID string, label string) {
	hideCommandWindow(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		r.positionLog(positionID, "warning", "播放提示音失败："+err.Error())
		return
	}
	r.positionLog(positionID, "info", "提示音播放命令已启动："+label)
	// 非阻塞——不等待播放结束，避免卡主流程
	go func() {
		if err := cmd.Wait(); err != nil {
			detail := strings.TrimSpace(output.String())
			if detail != "" {
				r.positionLog(positionID, "warning", "提示音播放进程异常："+err.Error()+"，输出："+detail)
				return
			}
			r.positionLog(positionID, "warning", "提示音播放进程异常："+err.Error())
			return
		}
		r.positionLog(positionID, "info", "提示音播放成功："+label)
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

// positionBrowserViewport 返回岗位运行和手动打开入口共用的固定视口尺寸。
func positionBrowserViewport() (int, int) {
	return browser.FixedViewport()
}

// sendPositionFailNotification 通知云端岗位运行失败，由云端按岗位运行 ID 查用户并发邮件。
// ctx 为请求上下文，positionID 为岗位运行 ID，errorMsg 为失败原因，options 为本次岗位运行启动参数。
func (r *Runner) sendPositionFailNotification(ctx context.Context, positionID string, errorMsg string, options StartOptions) {
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	client := cloudapi.New(baseURL)
	if err := client.SendPositionFailNotice(ctx, options.Token, positionID, errorMsg); err != nil {
		r.positionLog(positionID, "warning", "岗位运行失败：邮件通知发送失败，错误="+err.Error())
		return
	}
	r.positionLog(positionID, "info", "岗位运行失败：已发送邮件通知")
}

// notifyCloudPositionStopped 通知云端岗位运行已经停止。
// positionID 为云端岗位运行 ID，options 为本次启动参数。
func (r *Runner) notifyCloudPositionStopped(positionID string, options StartOptions) {
	r.syncCloudPositionStatus(positionID, "stopped", "岗位运行停止", options)
}

// notifyCloudPositionCompleted 通知云端岗位运行已经完成。
// positionID 为云端岗位运行 ID，options 为本次启动参数。
func (r *Runner) notifyCloudPositionCompleted(positionID string, options StartOptions) {
	r.syncCloudPositionStatus(positionID, "completed", "岗位运行完成", options)
}

// syncCloudPositionStatus 同步云端岗位运行状态。
// positionID 为云端岗位运行 ID，status 为云端状态，label 为岗位运行日志前缀。
func (r *Runner) syncCloudPositionStatus(positionID string, status string, label string, options StartOptions) {
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
	r.positionLog(positionID, "info", label+"：正在同步云端状态")
	if err := client.SyncPositionStatus(ctx, token, positionID, status); err != nil {
		r.positionLog(positionID, "warning", label+"：云端状态同步失败，错误="+err.Error())
		return
	}
	r.positionLog(positionID, "info", label+"：云端状态同步成功")
}
