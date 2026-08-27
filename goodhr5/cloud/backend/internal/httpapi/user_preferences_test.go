// Package httpapi 文件作用：验证个人配置中的 CloakBrowser Key 格式和公开字段。
package httpapi

import (
	"net/http/httptest"
	"testing"
)

// TestUserPreferencesAcceptsCloakBrowserLicenseKey 验证完整 Key 可以保存并返回给当前登录用户。
func TestUserPreferencesAcceptsCloakBrowserLicenseKey(t *testing.T) {
	licenseKey := "cb_test_cloud_preferences_123456"
	recorder := httptest.NewRecorder()
	prefs, ok := (userPreferencesRequest{
		CloakBrowserLicenseKey: licenseKey,
		ClickFrequency:         80, DetailOpenProbability: 80,
	}).toPreferences(recorder)
	if !ok || prefs.CloakBrowserLicenseKey != licenseKey {
		t.Fatalf("完整 Key 被拒绝：status=%d", recorder.Code)
	}
	if got := publicUserPreferences(prefs)["cloakbrowser_license_key"]; got != licenseKey {
		t.Fatalf("个人配置没有返回当前用户自己的 Key：%v", got)
	}
}

// TestUserPreferencesRejectsBrokenCloakBrowserLicenseKey 验证截断或含空格的 Key 不会保存。
func TestUserPreferencesRejectsBrokenCloakBrowserLicenseKey(t *testing.T) {
	for _, value := range []string{"cb_short", "cb_has space_123456"} {
		recorder := httptest.NewRecorder()
		_, ok := (userPreferencesRequest{
			CloakBrowserLicenseKey: value,
			ClickFrequency:         80, DetailOpenProbability: 80,
		}).toPreferences(recorder)
		if ok {
			t.Fatalf("错误 Key 被接受：%q", value)
		}
	}
}
