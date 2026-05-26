// 本文件负责测试用户新手教学状态 API。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOnboardingStatusAndComplete 验证登录用户可以读取并完成新手教学。
func TestOnboardingStatusAndComplete(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "onboarding@example.com")

	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusResp := httptest.NewRecorder()
	routes.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", statusResp.Code, statusResp.Body.String())
	}

	var statusPayload struct {
		Onboarding OnboardingState `json:"onboarding"`
		Config     map[string]any  `json:"config"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&statusPayload); err != nil {
		t.Fatal(err)
	}
	if statusPayload.Onboarding.Completed {
		t.Fatalf("new onboarding should not be completed: %+v", statusPayload.Onboarding)
	}
	if statusPayload.Config["trial_days"] == nil {
		t.Fatalf("onboarding config missing trial_days: %+v", statusPayload.Config)
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete", nil)
	completeReq.Header.Set("Authorization", "Bearer "+token)
	completeResp := httptest.NewRecorder()
	routes.ServeHTTP(completeResp, completeReq)
	if completeResp.Code != http.StatusOK {
		t.Fatalf("complete code = %d, body = %s", completeResp.Code, completeResp.Body.String())
	}

	var completePayload struct {
		Onboarding OnboardingState `json:"onboarding"`
	}
	if err := json.NewDecoder(completeResp.Body).Decode(&completePayload); err != nil {
		t.Fatal(err)
	}
	if !completePayload.Onboarding.Completed || completePayload.Onboarding.CompletedAt == nil {
		t.Fatalf("unexpected completed onboarding: %+v", completePayload.Onboarding)
	}
}
