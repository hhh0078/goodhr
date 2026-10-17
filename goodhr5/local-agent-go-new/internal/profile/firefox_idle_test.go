// Package profile 文件作用：用合成目录、PID 链接和子进程系统锁验证只读占用检查，不访问真实浏览器账号。
package profile

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestFirefoxIdleRoots 验证首装无目录可继续，异常根目录、链接根与链接 Profile 均不会被跟随。
func TestFirefoxIdleRoots(t *testing.T) {
	root := t.TempDir()
	if err := AssertFirefoxProfilesIdle(filepath.Join(root, "not-created")); err != nil {
		t.Fatal(err)
	}
	if err := AssertFirefoxProfilesIdle(""); err == nil {
		t.Fatal("空根目录不应被当作当前工作目录")
	}
	file := filepath.Join(root, "ordinary-file")
	if err := os.WriteFile(file, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AssertFirefoxProfilesIdle(file); err == nil {
		t.Fatal("文件不能作为 Profile 根目录")
	}
	outside, linked := t.TempDir(), filepath.Join(root, "linked-profile")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if err := AssertFirefoxProfilesIdle(linked); err == nil {
		t.Fatal("链接根目录不应被跟随")
	}
	if err := AssertFirefoxProfilesIdle(root); err == nil {
		t.Fatal("一级链接 Profile 不应被跟随")
	}
}

// TestFirefoxIdleLeavesStaleFilesUntouched 验证未占用的历史普通锁、指纹与合成登录标记保持字节和修改时间不变。
func TestFirefoxIdleLeavesStaleFilesUntouched(t *testing.T) {
	root, account := t.TempDir(), "synthetic-account"
	profileDir := filepath.Join(root, account)
	if err := os.Mkdir(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{root, profileDir} {
		for _, name := range append([]string{"fingerprint.json", "synthetic-login-marker"}, firefoxLockNames...) {
			file := filepath.Join(directory, name)
			if err := os.WriteFile(file, []byte("unchanged synthetic file"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				content, readErr := os.ReadFile(file)
				after, statErr := os.Stat(file)
				if readErr != nil || statErr != nil || string(content) != "unchanged synthetic file" || !after.ModTime().Equal(before.ModTime()) {
					t.Errorf("只读检查改动了 %s", name)
				}
			})
		}
	}
	if err := AssertFirefoxProfilesIdle(root); err != nil {
		t.Fatal(err)
	}
}

// TestFirefoxSymlinkPIDParsing 验证 Mozilla 的两个本机 PID 格式，拒绝远端地址、溢出和无法解释的锁目标。
func TestFirefoxSymlinkPIDParsing(t *testing.T) {
	for _, marker := range []string{"", "+"} {
		pid, err := firefoxSymlinkPID(fmt.Sprintf("127.0.0.1:%s%d", marker, os.Getpid()))
		if err != nil || pid != os.Getpid() {
			t.Fatalf("合法锁未识别：%d %v", pid, err)
		}
	}
	for _, value := range []string{"", "not-a-lock", "host:123", "192.0.2.1:123", "127.0.0.1:", "127.0.0.1:+", "127.0.0.1:0", "127.0.0.1:-1", "127.0.0.1:123x", "127.0.0.1:2147483648", " 127.0.0.1:123"} {
		if _, err := firefoxSymlinkPID(value); err == nil {
			t.Fatalf("错误锁目标被接受：%s", value)
		}
	}
}

// TestFirefoxSymlinkLocks 验证 Unix 新旧链接锁只在 PID 已退出时放行，未知锁或活进程保持原样。
func TestFirefoxSymlinkLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Mozilla 的 PID 链接锁只用于 Unix；Windows 已由独占句柄测试覆盖")
	}
	for _, name := range firefoxLockNames {
		for _, target := range []string{fmt.Sprintf("127.0.0.1:%d", os.Getpid()), fmt.Sprintf("127.0.0.1:+%d", os.Getpid()), "unknown", "127.0.0.1:2147483647"} {
			root := t.TempDir()
			file := filepath.Join(root, name)
			if err := os.Symlink(target, file); err != nil {
				t.Fatal(err)
			}
			err := AssertFirefoxProfilesIdle(root)
			if (target == "127.0.0.1:2147483647") != (err == nil) {
				t.Fatalf("链接锁判断不正确：%s %v", target, err)
			}
			if current, readErr := os.Readlink(file); readErr != nil || current != target {
				t.Fatal("只读检查删除或改写了链接锁")
			}
		}
	}
}

// TestFirefoxNativeLockFromChild 验证真正的子进程系统锁会阻止更新，进程正常退出后的同一文件可继续使用。
func TestFirefoxNativeLockFromChild(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "synthetic-account")
	if err := os.Mkdir(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(profileDir, nativeFirefoxLockNameForTest())
	if err := os.WriteFile(file, []byte("synthetic lock content"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestFirefoxNativeLockHelper$")
	command.Env = append(os.Environ(), "GOODHR_FIREFOX_LOCK_HELPER="+file)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = command.Process.Kill(); _ = command.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("合成锁进程未准备好：%q %s", line, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("合成锁进程准备超时")
	}
	if err = AssertFirefoxProfilesIdle(root); err == nil {
		t.Fatal("实际被子进程占用的 Profile 未被阻止")
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = command.Wait(); err != nil {
		t.Fatalf("合成锁进程退出失败：%v %s", err, stderr.String())
	}
	if err = AssertFirefoxProfilesIdle(root); err != nil {
		t.Fatalf("进程退出后的历史普通文件不应阻断：%v", err)
	}
	content, err := os.ReadFile(file)
	after, statErr := os.Stat(file)
	if err != nil || statErr != nil || string(content) != "synthetic lock content" || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("检查占用时不应改写锁文件内容或修改时间")
	}
}

// TestFirefoxNativeLockHelper 仅在测试子进程中持有合成锁，等待父测试关闭标准输入后释放。
func TestFirefoxNativeLockHelper(t *testing.T) {
	file := os.Getenv("GOODHR_FIREFOX_LOCK_HELPER")
	if file == "" {
		t.Skip("仅供合成子进程锁测试使用")
	}
	unlock, err := holdFirefoxNativeLockForTest(file)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, _ = fmt.Fprintln(os.Stdout, "locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// TestFirefoxIdleOnlyScansOneProfileLevel 验证扫描不会递归读取账号目录中的备份或其它数据。
func TestFirefoxIdleOnlyScansOneProfileLevel(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "account", "nested-backup")
	if err := os.MkdirAll(filepath.Join(nested, "parent.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AssertFirefoxProfilesIdle(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "account", ".parentlock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AssertFirefoxProfilesIdle(root); err == nil {
		t.Fatal("一级账号的异常锁类型必须阻止更新")
	}
}
