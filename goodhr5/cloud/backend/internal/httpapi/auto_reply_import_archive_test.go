// 本文件验证旧资料归档的文件完整性、Windows 路径和已删除附件的隔离。
package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplyImportArchiveVerification 构造真实 ZIP，只有全部附件和源实体属于当前账号才可进库。
func TestReplyImportArchiveVerification(t *testing.T) {
	owner := "auto_reply:v1:" + replyImportHash("user")
	content := []byte("简历附件正文")
	sha := replyImportHash(string(content))
	rel := replyImportHash(owner) + "/attachments/" + sha + ".pdf"
	item := replyImportFile{RelativePath: rel, SHA256: sha, SizeBytes: int64(len(content))}
	for _, scenario := range []string{"windows", "tampered", "outside", "extra", "owner"} {
		t.Run(scenario, func(t *testing.T) {
			staging := t.TempDir()
			f, err := os.CreateTemp(t.TempDir(), "import-*")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			file := item
			manifestFile := item
			input := content
			if scenario == "windows" {
				file.RelativePath = strings.ReplaceAll(file.RelativePath, "/", "\\")
			}
			if scenario == "tampered" {
				input = bytes.Repeat([]byte{'x'}, len(content))
			}
			if scenario == "outside" {
				file.RelativePath = "../other.pdf"
				manifestFile = file
			}
			payload, _ := json.Marshal(file)
			rows := []AutoReplyStateChange{{Kind: "attachments", ID: "attachment", Payload: payload}}
			data, _ := json.Marshal(rows)
			m := replyImportManifest{Version: 1, Owner: owner, Records: 1, StateDigest: replyImportHash(string(data)), Files: []replyImportFile{manifestFile}}
			if scenario == "owner" {
				m.Owner = "another-user"
			}
			z := zip.NewWriter(f)
			a, _ := z.Create("manifest.json")
			json.NewEncoder(a).Encode(m)
			a, _ = z.Create("entities.json")
			a.Write(data)
			a, _ = z.Create("files/" + manifestFile.RelativePath)
			a.Write(input)
			if scenario == "extra" {
				z.Create("../escape")
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			stat, _ := f.Stat()
			imported, files, err := unpackReplyArchive(f, stat.Size(), staging, owner)
			if scenario != "windows" {
				if err == nil {
					t.Fatal("未拒绝不合规归档")
				}
				return
			}
			if err != nil || len(imported) != 1 || len(files) != 1 {
				t.Fatal(err, imported, files)
			}
			var got replyImportFile
			if err = json.Unmarshal(imported[0].Payload, &got); err != nil || got.RelativePath != item.RelativePath {
				t.Fatal("未转换旧Windows路径", err)
			}
			scope := t.TempDir()
			if err = installReplyImportFiles(context.Background(), staging, scope, files, nil); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(scope, filepath.FromSlash(item.RelativePath))
			if _, err = os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("没有引用的旧文件不应安装")
			}
			live := map[string]bool{rel: true}
			for range 2 {
				if err = installReplyImportFiles(context.Background(), staging, scope, files, live); err != nil {
					t.Fatal(err)
				}
			}
			b, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(b, content) {
				t.Fatal("文件内容不匹配", err)
			}
		})
	}
}
