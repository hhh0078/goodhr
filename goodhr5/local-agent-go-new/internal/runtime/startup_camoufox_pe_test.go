// Package runtime 文件作用：构造隔离的 Windows PE 样本，验证浏览器启动文件只接受完整的 x64 EXE。
package runtime

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// startupTestPE 构造带完整程序头与可执行入口的最小 PE 样本，只供静态校验，不执行样本。
func startupTestPE(machine uint16) []byte {
	content := make([]byte, 1024)
	copy(content, "MZ")
	binary.LittleEndian.PutUint32(content[0x3c:], 0x80)
	copy(content[0x80:], "PE\x00\x00")
	header := pe.FileHeader{Machine: machine, NumberOfSections: 1, SizeOfOptionalHeader: 240, Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE | pe.IMAGE_FILE_LARGE_ADDRESS_AWARE}
	optional := pe.OptionalHeader64{
		Magic: 0x20b, AddressOfEntryPoint: 0x1000, BaseOfCode: 0x1000,
		ImageBase: 0x140000000, SectionAlignment: 4096, FileAlignment: 512,
		SizeOfImage: 8192, SizeOfHeaders: 512, Subsystem: pe.IMAGE_SUBSYSTEM_WINDOWS_GUI,
		NumberOfRvaAndSizes: 16,
	}
	section := pe.SectionHeader32{
		Name: [8]uint8{'.', 't', 'e', 'x', 't'}, VirtualSize: 512,
		VirtualAddress: 0x1000, SizeOfRawData: 512, PointerToRawData: 512,
		Characteristics: pe.IMAGE_SCN_CNT_CODE | pe.IMAGE_SCN_MEM_EXECUTE | pe.IMAGE_SCN_MEM_READ,
	}
	var headers bytes.Buffer
	_ = binary.Write(&headers, binary.LittleEndian, header)
	_ = binary.Write(&headers, binary.LittleEndian, optional)
	_ = binary.Write(&headers, binary.LittleEndian, section)
	copy(content[0x84:], headers.Bytes())
	content[512] = 0xc3
	return content
}

// TestStartupCamoufoxWindowsExecutable 验证有效 x64 EXE 通过，错架构、DLL、缺入口和截断文件均被拒绝。
func TestStartupCamoufoxWindowsExecutable(t *testing.T) {
	for _, scenario := range []string{"valid", "x86", "arm64", "dll", "not executable", "invalid bytes", "missing dos header", "truncated", "missing optional header", "missing entry", "entry outside image", "entry outside code", "non executable section", "invalid subsystem"} {
		t.Run(scenario, func(t *testing.T) {
			content := startupTestPE(pe.IMAGE_FILE_MACHINE_AMD64)
			switch scenario {
			case "x86":
				content = startupTestPE(pe.IMAGE_FILE_MACHINE_I386)
			case "arm64":
				content = startupTestPE(pe.IMAGE_FILE_MACHINE_ARM64)
			case "dll":
				binary.LittleEndian.PutUint16(content[0x96:], pe.IMAGE_FILE_EXECUTABLE_IMAGE|pe.IMAGE_FILE_DLL)
			case "not executable":
				binary.LittleEndian.PutUint16(content[0x96:], 0)
			case "invalid bytes":
				content = []byte("not a Windows executable")
			case "missing dos header":
				content[0] = 0
			case "truncated":
				content = content[:600]
			case "missing optional header":
				binary.LittleEndian.PutUint16(content[0x94:], 0)
			case "missing entry":
				binary.LittleEndian.PutUint32(content[0xa8:], 0)
			case "entry outside image":
				binary.LittleEndian.PutUint32(content[0xa8:], 0x3000)
			case "entry outside code":
				binary.LittleEndian.PutUint32(content[0xa8:], 0x1800)
			case "non executable section":
				binary.LittleEndian.PutUint32(content[0x1ac:], pe.IMAGE_SCN_MEM_READ)
			case "invalid subsystem":
				binary.LittleEndian.PutUint16(content[0xdc:], pe.IMAGE_SUBSYSTEM_NATIVE)
			}
			path := filepath.Join(t.TempDir(), "camoufox.exe")
			if err := os.WriteFile(path, content, 0o755); err != nil {
				t.Fatal(err)
			}
			err := checkStartupCamoufoxArchitecture(path, "win-x64")
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("Windows 启动文件校验结果不符：%s err=%v", scenario, err)
			}
			if err := checkStartupCamoufoxArchitecture(path, "darwin-x64"); err == nil {
				t.Fatal("Windows 包不能被 Mac 使用")
			}
		})
	}
}
