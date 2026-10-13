// Package runtime 文件作用：验证浏览器与 OCR 原地安装、真实组件根目录、旧版本保留及 Mac 应用结构。
package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
)

// componentTestFiles 构造当前平台的最小组件结构；Mac 浏览器保留 .app，Vision OCR 不带 ONNX。
func componentTestFiles(component string, content string) map[string][]byte {
	if component == "camoufox" {
		switch runtime.GOOS {
		case "darwin":
			return map[string][]byte{"Camoufox.app/Contents/MacOS/camoufox": []byte(content), "Camoufox.app/Contents/Resources/omni.ja": []byte("fixture")}
		case "windows":
			return map[string][]byte{"camoufox.exe": []byte(content), "browser/omni.ja": []byte("fixture")}
		default:
			return map[string][]byte{"camoufox-bin": []byte(content), "browser/omni.ja": []byte("fixture")}
		}
	}
	name := "RapidOCR-json"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	files := map[string][]byte{name: []byte(content)}
	if runtime.GOOS != "darwin" {
		files["models/ch_PP-OCRv3_det_infer.onnx"] = []byte("fixture model")
	}
	return files
}

// writeComponentTestFiles 将带执行权限的组件样本写入指定目录。
func writeComponentTestFiles(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// makeComponentTestArchive 构造带包装目录的 ZIP，验证安装记录保留真实组件根目录。
func makeComponentTestArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		header := &zip.FileHeader{Name: "package-wrapper/" + name, Method: zip.Deflate}
		header.SetMode(0o755)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// componentTestPath 返回当前管理器选择的浏览器或 OCR 路径。
func componentTestPath(manager *Manager, component string) string {
	if component == "camoufox" {
		return manager.CamoufoxPath()
	}
	return manager.OCRPath()
}

// TestInstallComponentsInPlace 验证新组件不覆盖旧目录，同摘要跳过下载，重启和迁移后相对路径可用。
func TestInstallComponentsInPlace(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, component := range []string{"camoufox", "ocr"} {
		t.Run(component, func(t *testing.T) {
			parent := t.TempDir()
			runtimeDir := filepath.Join(parent, "runtime data")
			legacyRoot := filepath.Join(runtimeDir, component)
			writeComponentTestFiles(t, legacyRoot, componentTestFiles(component, "legacy"))
			manager := &Manager{runtimeDir: runtimeDir}
			legacyPath := componentTestPath(manager, component)
			asset, downloads := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles(component, "new component")))
			asset.Version = "152.0.4-beta.30"
			if err := manager.installAsset(context.Background(), component, component, component, asset); err != nil {
				t.Fatal(err)
			}
			if content, err := os.ReadFile(legacyPath); err != nil || string(content) != "legacy" {
				t.Fatalf("旧组件被修改：content=%q err=%v", content, err)
			}
			recorded := manager.loadVersions()[component]
			if !strings.HasPrefix(recorded.DirectoryPath, component+"-runtime-") || !strings.HasPrefix(recorded.ExecutablePath, recorded.DirectoryPath+"/") {
				t.Fatalf("未记录真实的相对组件路径：%+v", recorded)
			}
			if !manager.componentInstalled(component) || componentTestPath(manager, component) == legacyPath {
				t.Fatal("新安装的组件未被选用")
			}
			if component == "camoufox" {
				root, err := safeJoin(runtimeDir, recorded.DirectoryPath)
				if err != nil || manager.CamoufoxDir() != root || !fileExists(filepath.Join(root, "version.json")) {
					t.Fatalf("浏览器根目录或版本记录不正确：root=%s err=%v", root, err)
				}
			}
			if err := manager.installAsset(context.Background(), component, component, component, asset); err != nil || downloads.Load() != 1 {
				t.Fatalf("同版本同摘要应跳过：downloads=%d err=%v", downloads.Load(), err)
			}
			restarted := &Manager{runtimeDir: runtimeDir}
			if componentTestPath(restarted, component) != componentTestPath(manager, component) {
				t.Fatal("重启后丢失组件路径")
			}
			relocated := filepath.Join(parent, "relocated runtime")
			if err := os.Rename(runtimeDir, relocated); err != nil {
				t.Fatal(err)
			}
			restarted.runtimeDir = relocated
			if !restarted.componentInstalled(component) || !strings.HasPrefix(componentTestPath(restarted, component), relocated) {
				t.Fatal("迁移后相对路径失效")
			}
		})
	}
}

// TestComponentInstallFailurePreservesPrevious 验证缺文件、损坏压缩包和校验失败时保留旧组件与记录。
func TestComponentInstallFailurePreservesPrevious(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, component := range []string{"camoufox", "ocr"} {
		for _, failure := range []string{"missing executable", "bad archive", "bad sha", "missing model"} {
			if failure == "missing model" && (component != "ocr" || runtime.GOOS == "darwin") {
				continue
			}
			t.Run(component+"/"+failure, func(t *testing.T) {
				runtimeDir := t.TempDir()
				writeComponentTestFiles(t, filepath.Join(runtimeDir, component), componentTestFiles(component, "legacy"))
				manager := &Manager{runtimeDir: runtimeDir}
				legacyPath := componentTestPath(manager, component)
				if err := manager.saveVersion(component, Asset{Version: "previous"}, "", ""); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(manager.statePath())
				if err != nil {
					t.Fatal(err)
				}
				files := componentTestFiles(component, "new")
				if failure == "missing executable" {
					files = map[string][]byte{"README": []byte("fixture")}
				}
				if failure == "missing model" {
					delete(files, "models/ch_PP-OCRv3_det_infer.onnx")
				}
				archive := makeComponentTestArchive(t, files)
				if failure == "bad archive" {
					archive = []byte("broken zip")
				}
				asset, _ := serveNodeTestArchive(t, archive)
				if failure == "bad sha" {
					asset.SHA256 = strings.Repeat("0", 64)
				}
				if err := manager.installAsset(context.Background(), component, component, component, asset); err == nil {
					t.Fatal("损坏组件不应报告成功")
				}
				after, err := os.ReadFile(manager.statePath())
				if err != nil || !bytes.Equal(before, after) || componentTestPath(manager, component) != legacyPath {
					t.Fatalf("失败后未保留旧版本：err=%v", err)
				}
				remaining, err := filepath.Glob(filepath.Join(runtimeDir, component+"-runtime-*"))
				if err != nil || len(remaining) != 0 {
					t.Fatalf("失败安装目录未清理：%v err=%v", remaining, err)
				}
			})
		}
	}
}

// TestRecordedComponentPathsFallback 验证旧记录、越界记录、失效记录回退，显式浏览器路径继续优先。
func TestRecordedComponentPathsFallback(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, component := range []string{"camoufox", "ocr"} {
		root := t.TempDir()
		writeComponentTestFiles(t, filepath.Join(root, component), componentTestFiles(component, "legacy"))
		manager := &Manager{runtimeDir: root}
		legacyPath := componentTestPath(manager, component)
		for _, path := range []string{"", "missing/binary", "../outside/binary", legacyPath} {
			if err := manager.saveVersion(component, Asset{Version: "previous"}, path, "../outside"); err != nil {
				t.Fatal(err)
			}
			if componentTestPath(manager, component) != legacyPath {
				t.Fatal("错误记录没有回退旧路径")
			}
			if component == "camoufox" && manager.CamoufoxDir() != filepath.Join(root, component) {
				t.Fatal("错误记录改变了浏览器根目录")
			}
		}
	}
	root := t.TempDir()
	configured := filepath.Join(root, "browser", "camoufox.exe")
	t.Setenv("CAMOUFOX_BINARY_PATH", configured)
	manager := &Manager{runtimeDir: root}
	if manager.CamoufoxPath() != configured || manager.CamoufoxDir() != filepath.Dir(configured) {
		t.Fatal("显式浏览器路径与组件目录不一致")
	}
}

// TestMacComponentLayout 验证 Mac .app 根目录和 Vision 要求；Windows 执行本测试不代表 Mac 真机运行。
func TestMacComponentLayout(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "Camoufox.app", "Contents", "MacOS", "camoufox")
	writeComponentTestFiles(t, root, map[string][]byte{"Camoufox.app/Contents/MacOS/camoufox": []byte("mac fixture")})
	if findCamoufoxBinary(root, "darwin") != binary || camoufoxDirectoryForBinary(binary, "darwin") != root {
		t.Fatal("Mac 应用目录解析错误")
	}
	if installRoot(root, "camoufox") != root {
		t.Fatal("Mac .app 应用结构被拆开")
	}
	ocr := filepath.Join(root, "RapidOCR-json")
	if err := os.WriteFile(ocr, []byte("vision fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := checkOCRFiles(ocr, "darwin"); err != nil {
			t.Fatal("Mac Vision 不应要求 ONNX 模型：", err)
		}
		if err := os.Chmod(ocr, 0o644); err != nil {
			t.Fatal(err)
		}
		if checkOCRFiles(ocr, "darwin") == nil {
			t.Fatal("Unix 缺少执行权限应被拒绝")
		}
	}
}

// TestOCRClientFollowsInstalledPath 验证安装前创建的真实 OCR 客户端会通过管理器改用新路径，而不是继续读旧目录。
func TestOCRClientFollowsInstalledPath(t *testing.T) {
	root := t.TempDir()
	writeComponentTestFiles(t, filepath.Join(root, "ocr"), componentTestFiles("ocr", "legacy"))
	manager := &Manager{runtimeDir: root}
	var selected string
	client := ocr.NewWithExecutableResolver(func() string {
		selected = manager.OCRPath()
		return selected
	})
	if err := client.Ready(); err != nil {
		t.Fatal(err)
	}
	legacy := selected
	asset, _ := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles("ocr", "new")))
	if err := manager.installAsset(context.Background(), "ocr", "OCR", "ocr", asset); err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(); err != nil || selected == legacy || selected != manager.OCRPath() {
		t.Fatalf("OCR 客户端没有使用新组件：selected=%s err=%v", selected, err)
	}
}

// TestWorkerReceivesRecordedBrowserDirectory 启动隔离的 Node 样本，验证 Worker 收到新目录、版本与内置扩展路径。
func TestWorkerReceivesRecordedBrowserDirectory(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	node, err := exec.LookPath(nodeBinaryName())
	if err != nil {
		t.Skip("需要本机 Node 执行 Worker 环境样本")
	}
	root := t.TempDir()
	manager := &Manager{runtimeDir: root, nodePath: node}
	asset, _ := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles("camoufox", "browser")))
	asset.Version = "152.0.4-beta.30"
	if err := manager.installAsset(context.Background(), "camoufox", "Camoufox", "camoufox", asset); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "worker-environment.json")
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "worker-environment.cjs")
	script := "require('node:fs').writeFileSync(" + string(markerJSON) + ", JSON.stringify({directory:process.env.CAMOUFOX_INSTALL_DIR,binary:process.env.CAMOUFOX_BINARY_PATH,ubo:process.env.GOODHR_UBO_EXTENSION_PATH,version:JSON.parse(require('node:fs').readFileSync(require('node:path').join(process.env.CAMOUFOX_INSTALL_DIR,'version.json'),'utf8'))}));setInterval(()=>{},1000);"
	if err := os.WriteFile(entry, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	manager.worker = browserprocess.New(node, entry, 0, nil)
	manager.configureWorkerEnvironment()
	if err := manager.worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.worker.Stop() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(marker)
		if err == nil {
			var actual struct {
				Directory string `json:"directory"`
				Binary    string `json:"binary"`
				UBO       string `json:"ubo"`
				Version   struct {
					Version string `json:"version"`
					Release string `json:"release"`
				} `json:"version"`
			}
			if err := json.Unmarshal(content, &actual); err != nil {
				t.Fatal(err)
			}
			if actual.Directory != manager.CamoufoxDir() || actual.Binary != manager.CamoufoxPath() || actual.UBO != bundledUBOPath(manager.CamoufoxDir(), runtime.GOOS) || actual.Version.Version != "152.0.4" || actual.Version.Release != "beta.30" {
				t.Fatalf("Worker 环境仍指向旧目录：%+v", actual)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Node 样本没有返回 Worker 环境")
}
