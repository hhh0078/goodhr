//go:build windows

// Package process 负责端口探测和本地进程辅助管理。
package process

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// goodHRHealthResponse 表示本地 GoodHR 健康检查的关键身份字段。
type goodHRHealthResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Port    int    `json:"port"`
		DataDir string `json:"dataDir"`
	} `json:"data"`
}

// StopGoodHRPortOwner 仅在固定端口占用者确认是旧 GoodHR 本地程序时结束其进程树。
// host 和 port 为待检查的监听地址，currentPID 为当前新版进程 ID。
func StopGoodHRPortOwner(host string, port int, currentPID int) error {
	pid, occupied, err := findListeningPID(port)
	if err != nil {
		return err
	}
	if !occupied {
		return nil
	}
	if pid == currentPID {
		return fmt.Errorf("端口 %d 正由当前进程 PID=%d 使用，拒绝结束自身", port, pid)
	}
	if err := verifyGoodHRHealth(host, port); err != nil {
		return fmt.Errorf("端口 %d 被非 GoodHR 程序或异常旧程序占用，已禁止自动结束 PID=%d：%w", port, pid, err)
	}
	cmd := hiddenCommand("positionkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("结束占用端口 %d 的旧 GoodHR 程序 PID=%d 失败：%w：%s", port, pid, err, strings.TrimSpace(string(output)))
	}
	return waitPortReleased(port, 5*time.Second)
}

// findListeningPID 从 Windows TCP 监听表中查找指定端口的进程 ID。
// port 为待检查端口，返回 PID、是否被占用及查询错误。
func findListeningPID(port int) (int, bool, error) {
	cmd := hiddenCommand("netstat", "-ano", "-p", "tcp")
	output, err := cmd.Output()
	if err != nil {
		return 0, false, fmt.Errorf("查询端口 %d 占用进程失败：%w", port, err)
	}
	return parseListeningPID(string(output), port)
}

// parseListeningPID 解析 netstat 输出并返回指定监听端口的进程 ID。
// output 为 netstat 文本，port 为待匹配端口。
func parseListeningPID(output string, port int) (int, bool, error) {
	portSuffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if !strings.HasSuffix(fields[1], portSuffix) {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 0 {
			return 0, false, fmt.Errorf("端口 %d 的监听 PID 无效：%s", port, fields[4])
		}
		return pid, true, nil
	}
	return 0, false, nil
}

// verifyGoodHRHealth 验证指定地址返回的是 GoodHR 本地程序健康信息。
// host 和 port 为健康检查监听地址。
func verifyGoodHRHealth(host string, port int) error {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/health"
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("健康检查不可用：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查状态码为 %d", resp.StatusCode)
	}
	var health goodHRHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("健康检查不是有效 JSON：%w", err)
	}
	if !health.OK || health.Data.Status != "ok" || strings.TrimSpace(health.Data.Version) == "" || health.Data.Port != port || strings.TrimSpace(health.Data.DataDir) == "" {
		return fmt.Errorf("健康检查缺少 GoodHR 身份字段")
	}
	return nil
}

// waitPortReleased 等待指定端口真正停止监听。
// port 为待检查端口，timeout 为最长等待时间。
func waitPortReleased(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, occupied, err := findListeningPID(port)
		if err != nil {
			return err
		}
		if !occupied {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("旧 GoodHR 程序已结束，但端口 %d 未在限定时间内释放", port)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

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
		cmd := hiddenCommand("positionkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("关闭旧本地程序 PID=%d 失败：%w：%s", pid, err, string(output))
		}
	}
	return waitProcessesExit(imageName, pids, 5*time.Second)
}

// findProcessPIDs 查找需要关闭的旧本地程序进程。
// imageName 为进程镜像名，currentPID 为当前进程 ID。
func findProcessPIDs(imageName string, currentPID int) ([]int, error) {
	cmd := hiddenCommand("positionlist", "/FI", "IMAGENAME eq "+imageName, "/FO", "CSV", "/NH")
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
