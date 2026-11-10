// Package model 本文件验证BOSS固定AI、其它平台固定DOM，避免历史OCR或大小写重新启用本机识别。
package model

import "testing"

// TestDetailRecognitionMode 验证所有已支持平台和历史空平台均得到固定结果。
func TestDetailRecognitionMode(t *testing.T) {
	for _, platform := range []string{"boss", " BOSS ", "liepin", "hliepin", "zhaopin", "51job", ""} {
		want := "dom"
		if platform == "boss" || platform == " BOSS " {
			want = "ai"
		}
		if got := DetailRecognitionMode(platform); got != want {
			t.Fatalf("平台%s详情模式=%s，期望%s", platform, got, want)
		}
	}
}
