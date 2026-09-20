// Package config 文件作用：使用隔离数据验证真实迁移进度和二次启动跳过，不接触用户账号。
package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestMigrationReportsActualBytes 验证复制和三轮校验的实际字节数、完整副本以及再次启动不重复迁移。
func TestMigrationReportsActualBytes(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "old"), filepath.Join(root, "data")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("migration-fixture"), 16384)
	if err := os.WriteFile(filepath.Join(source, "fixture.bin"), want, 0600); err != nil {
		t.Fatal(err)
	}
	states := map[string]DataProgress{}
	updates := map[string]int{}
	err := migrateDataWithProgress(source, destination, func(p DataProgress) {
		if p.Stage == "copying" {
			if _, err := os.Stat(filepath.Join(source, "fixture.bin")); err != nil {
				t.Error("校验前删除了原数据")
			}
		}
		if previous, ok := states[p.Stage]; ok && p.Done < previous.Done {
			t.Error("阶段进度倒退")
		}
		states[p.Stage] = p
		updates[p.Stage]++
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"verifying_source", "copying", "verifying_copy", "verifying_original"} {
		p := states[stage]
		if p.Done != int64(len(want)) || p.Total != int64(len(want)) || updates[stage] < 3 {
			t.Fatalf("%s 未报告逐块实际进度：%+v", stage, p)
		}
	}
	got, err := os.ReadFile(filepath.Join(destination, "fixture.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("副本校验失败")
	}
	var second []DataProgress
	if err := migrateDataWithProgress(source, destination, func(p DataProgress) { second = append(second, p) }); err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Stage != "skipped" {
		t.Fatalf("二次启动重复迁移：%+v", second)
	}
}
