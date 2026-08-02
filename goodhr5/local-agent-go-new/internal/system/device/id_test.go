// 本文件负责验证稳定设备编号的解析、校验和不可逆哈希规则。
package device

import (
	"runtime"
	"strings"
	"testing"
)

// TestCurrentReturnsStableID 验证受支持系统可以连续读取相同的真实设备编号。
func TestCurrentReturnsStableID(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("当前测试系统不读取 macOS 或 Windows 硬件编号")
	}
	first, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second || !strings.HasPrefix(first, idPrefix) {
		t.Fatalf("真实设备编号不稳定：first=%q second=%q", first, second)
	}
}

// TestParseDarwinHardwareID 验证可以从 macOS ioreg 输出读取主板编号。
func TestParseDarwinHardwareID(t *testing.T) {
	value, err := parseDarwinHardwareID(`    "IOPlatformUUID" = "ABCDEF12-3456-7890-ABCD-EF1234567890"`)
	if err != nil {
		t.Fatal(err)
	}
	if value != "abcdef12-3456-7890-abcd-ef1234567890" {
		t.Fatalf("hardware id = %q", value)
	}
}

// TestHashHardwareIDStable 验证同一硬件编号的大小写和花括号差异不会改变设备编号。
func TestHashHardwareIDStable(t *testing.T) {
	first, err := hashHardwareID("ABCDEF12-3456-7890-ABCD-EF1234567890")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashHardwareID("{abcdef12-3456-7890-abcd-ef1234567890}")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, idPrefix) || strings.Contains(first, "abcdef12") {
		t.Fatalf("设备编号不稳定或泄露原值：first=%q second=%q", first, second)
	}
}

// TestNormalizeHardwareIDRejectsDefaults 验证全零、全 F 和普通文本不会被当成真实设备编号。
func TestNormalizeHardwareIDRejectsDefaults(t *testing.T) {
	for _, value := range []string{
		"00000000-0000-0000-0000-000000000000",
		"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
		"To Be Filled By O.E.M.",
	} {
		if _, err := normalizeHardwareID(value); err == nil {
			t.Fatalf("无效设备编号被接受：%q", value)
		}
	}
}
