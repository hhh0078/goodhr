// 本文件负责测试用户订阅状态和订阅套餐 API。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSubscriptionStatusAndPlans 验证登录用户可以读取试用订阅和默认套餐。
func TestSubscriptionStatusAndPlans(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "subscription@example.com")

	statusReq := httptest.NewRequest(http.MethodGet, "/api/subscription/status", nil)
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusResp := httptest.NewRecorder()
	routes.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", statusResp.Code, statusResp.Body.String())
	}

	var statusPayload struct {
		Subscription struct {
			MemberType string `json:"member_type"`
			ExpiresAt  string `json:"expires_at"`
			Active     bool   `json:"active"`
		} `json:"subscription"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&statusPayload); err != nil {
		t.Fatal(err)
	}
	if statusPayload.Subscription.MemberType != defaultMemberType || !statusPayload.Subscription.Active || statusPayload.Subscription.ExpiresAt == "" {
		t.Fatalf("unexpected subscription payload: %+v", statusPayload.Subscription)
	}

	plansReq := httptest.NewRequest(http.MethodGet, "/api/subscription/plans", nil)
	plansReq.Header.Set("Authorization", "Bearer "+token)
	plansResp := httptest.NewRecorder()
	routes.ServeHTTP(plansResp, plansReq)
	if plansResp.Code != http.StatusOK {
		t.Fatalf("plans code = %d, body = %s", plansResp.Code, plansResp.Body.String())
	}

	var plansPayload struct {
		Plans []struct {
			ID             string   `json:"id"`
			Name           string   `json:"name"`
			OriginalPrice  float64  `json:"original_price"`
			DiscountAmount float64  `json:"discount_amount"`
			Features       []string `json:"features"`
		} `json:"plans"`
	}
	if err := json.NewDecoder(plansResp.Body).Decode(&plansPayload); err != nil {
		t.Fatal(err)
	}
	if len(plansPayload.Plans) != 3 {
		t.Fatalf("plans length = %d", len(plansPayload.Plans))
	}
	if plansPayload.Plans[0].ID != "monthly" || plansPayload.Plans[1].ID != "quarterly" || plansPayload.Plans[2].ID != "yearly" {
		t.Fatalf("unexpected plans: %+v", plansPayload.Plans)
	}
}
