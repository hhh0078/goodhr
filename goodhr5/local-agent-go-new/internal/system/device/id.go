// Package device 负责读取当前电脑稳定硬件标识并生成不可逆的 GoodHR 设备编号。
package device

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

const idPrefix = "goodhr-device-v1-"

var (
	currentOnce sync.Once
	currentID   string
	currentErr  error
)

// Current 返回当前电脑跨 GoodHR 软件重装保持稳定的不可逆设备编号。
func Current() (string, error) {
	currentOnce.Do(func() {
		rawID, err := readHardwareID()
		if err != nil {
			currentErr = err
			return
		}
		currentID, currentErr = hashHardwareID(rawID)
	})
	return currentID, currentErr
}

// readHardwareID 按当前系统读取主板提供的稳定硬件编号。
func readHardwareID() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		output, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return "", fmt.Errorf("读取 macOS 设备编号失败：%w", err)
		}
		return parseDarwinHardwareID(string(output))
	case "windows":
		commands := [][]string{
			{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID"},
			{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-ItemProperty 'HKLM:\\SOFTWARE\\Microsoft\\Cryptography').MachineGuid"},
			{"powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID"},
		}
		for _, command := range commands {
			output, err := exec.Command(command[0], command[1:]...).Output()
			if err != nil {
				continue
			}
			if value, parseErr := normalizeHardwareID(string(output)); parseErr == nil {
				return value, nil
			}
		}
		return "", errors.New("没有读取到 Windows 主板设备编号")
	default:
		return "", fmt.Errorf("当前系统暂不支持生成稳定设备编号：%s", runtime.GOOS)
	}
}

// parseDarwinHardwareID 从 ioreg 输出中提取 IOPlatformUUID。
func parseDarwinHardwareID(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		parts := strings.Split(line, "\"")
		if len(parts) >= 4 {
			return normalizeHardwareID(parts[3])
		}
	}
	return "", errors.New("没有找到 macOS IOPlatformUUID")
}

// normalizeHardwareID 清理并校验系统返回的硬件编号。
func normalizeHardwareID(value string) (string, error) {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "{}\"'"))
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return "", errors.New("设备编号格式不正确")
	}
	for _, character := range compact {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", errors.New("设备编号包含无法识别的字符")
		}
	}
	if compact == strings.Repeat("0", 32) || compact == strings.Repeat("f", 32) {
		return "", errors.New("设备编号是无效的默认值")
	}
	return value, nil
}

// hashHardwareID 使用固定命名空间哈希硬件编号，避免向云端暴露原始值。
func hashHardwareID(rawID string) (string, error) {
	normalized, err := normalizeHardwareID(rawID)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("goodhr-local-agent-device-v1:" + normalized))
	return fmt.Sprintf("%s%x", idPrefix, digest), nil
}
