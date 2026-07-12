// Package platforms 测试招聘平台运行时注册表。
package platforms

import (
	"fmt"
	"testing"
)

// TestRuntimeForUsesIndependentPackages 验证猎聘和智联使用独立运行时包。
// t 为测试对象。
func TestRuntimeForUsesIndependentPackages(t *testing.T) {
	cases := map[string]string{
		"hliepin": "*hliepin.Runtime",
		"liepin":  "*liepin.Runtime",
		"zhaopin": "*zhaopin.Runtime",
	}
	for platformID, wantType := range cases {
		runtime, err := RuntimeFor(platformID)
		if err != nil {
			t.Fatal(err)
		}
		if gotType := fmt.Sprintf("%T", runtime); gotType != wantType {
			t.Fatalf("%s runtime = %s, want %s", platformID, gotType, wantType)
		}
	}
}
