// 本文件验证岗位旧识别配置按平台固定兼容，读取响应不反向改写存储中的旧配置。
package httpapi

import "testing"

// TestPositionFixedDetailCompatibility 验证所有旧识别方式都使用固定平台策略，启动资格同步使用实际AI需求。
func TestPositionFixedDetailCompatibility(t *testing.T) {
	for _, platform := range []string{"boss", "liepin", "hliepin", "zhaopin", "51job"} {
		for _, old := range []string{"ocr", "ai", "dom", ""} {
			original := map[string]any{"mode_default": "keyword", "detail_mode": old}
			position := Position{PlatformID: platform, CommonConfig: original}
			result := publicPosition(position)
			want := "dom"
			if platform == "boss" {
				want = "ai"
			}
			if result["common_config"].(map[string]any)["detail_mode"] != want || original["detail_mode"] != old || positionUsesAI(position) != (platform == "boss") {
				t.Fatalf("平台%s旧模式%s未正确兼容：%+v", platform, old, result)
			}
		}
	}
}
