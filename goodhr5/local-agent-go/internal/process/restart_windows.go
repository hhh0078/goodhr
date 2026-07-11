//go:build windows

// Package process 负责端口探测和本地进程辅助管理。
package process

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StopOtherInstances 关闭除当前进程外的同名 Windows 进程。
// imageName 为进程镜像名，currentPID 为当前进程 ID。
func StopOtherInstances(imageName string, currentPID int) error {
	if imageName == "" || currentPID <= 0 {
		return nil
	}
	pids, err := findProcessPIDs(imageName, currentPID)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		cmd := hiddenCommand("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("关闭旧本地程序 PID=%d 失败：%w：%s", pid, err, string(output))
		}
	}
	return waitProcessesExit(imageName, pids, 5*time.Second)
}

// findProcessPIDs 查找需要关闭的旧本地程序进程。
// imageName 为进程镜像名，currentPID 为当前进程 ID。
func findProcessPIDs(imageName string, currentPID int) ([]int, error) {
	cmd := hiddenCommand("tasklist", "/FI", "IMAGENAME eq "+imageName, "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("查询旧本地程序失败：%w：%s", err, string(output))
	}
	reader := csv.NewReader(strings.NewReader(string(output)))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析旧本地程序列表失败：%w：%s", err, string(output))
	}
	var pids []int
	for _, record := range records {
		if len(record) < 2 || !strings.EqualFold(strings.TrimSpace(record[0]), imageName) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil || pid == currentPID {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// waitProcessesExit 等待旧本地程序真正退出，避免新进程启动时继续抢占端口。
// imageName 为进程镜像名，pids 为已发送关闭命令的旧进程 ID，timeout 为最长等待时间。
func waitProcessesExit(imageName string, pids []int, timeout time.Duration) error {
	if len(pids) == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		alive, err := findAlivePIDs(imageName, pids)
		if err != nil {
			return err
		}
		if len(alive) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("旧本地程序没有按时退出，仍在运行的 PID：%v", alive)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// findAlivePIDs 从指定 PID 中筛出仍在运行的旧本地程序。
// imageName 为进程镜像名，pids 为待检查的进程 ID。
func findAlivePIDs(imageName string, pids []int) ([]int, error) {
	running, err := findProcessPIDs(imageName, 0)
	if err != nil {
		return nil, err
	}
	wanted := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		wanted[pid] = struct{}{}
	}
	var alive []int
	for _, pid := range running {
		if _, ok := wanted[pid]; ok {
			alive = append(alive, pid)
		}
	}
	return alive, nil
}

// hiddenCommand 创建不会弹出黑色终端窗口的 Windows 系统命令。
// name 为命令名称，args 为命令参数。
func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd
}
