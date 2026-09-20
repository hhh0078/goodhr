// 文件作用说明：验证候选人卡片并发读取配置的默认值和允许范围。
package httpapi

import (
	"net/http/httptest"
	"testing"
)

// TestUserPreferencesCandidateReadConcurrencyDefaultsToOneHundred 验证旧版请求没有新字段时仍使用默认值一百。
func TestUserPreferencesCandidateReadConcurrencyDefaultsToOneHundred(t *testing.T) {
	recorder := httptest.NewRecorder()
	prefs, ok := (userPreferencesRequest{}).toPreferences(recorder)
	if !ok || prefs.CandidateReadConcurrency != 100 {
		t.Fatalf("默认并发读取数不正确：ok=%v value=%d body=%s", ok, prefs.CandidateReadConcurrency, recorder.Body.String())
	}
}

// TestUserPreferencesCandidateReadConcurrencyRejectsOutOfRange 验证小于五或大于一百的配置会被拒绝。
func TestUserPreferencesCandidateReadConcurrencyRejectsOutOfRange(t *testing.T) {
	for _, value := range []int{4, 101} {
		value := value
		recorder := httptest.NewRecorder()
		_, ok := (userPreferencesRequest{CandidateReadConcurrency: &value}).toPreferences(recorder)
		if ok {
			t.Fatalf("越界值 %d 不应通过校验", value)
		}
	}
}
