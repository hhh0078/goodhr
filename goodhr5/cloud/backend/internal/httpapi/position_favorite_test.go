// 本文件验证岗位收藏分数的默认关闭、保存回读和无效参数保护。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPositionFavoriteScoreLifecycle 验证旧岗位缺省关闭收藏，修改分数后能回读并重新关闭。
func TestPositionFavoriteScoreLifecycle(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-favorite@example.com"
	token := loginForTest(t, routes, email)
	positionID := ""
	for _, item := range []struct {
		name   string
		config map[string]any
		want   float64
	}{
		{name: "旧配置默认关闭", config: map[string]any{"position_requirement": "保留岗位条件"}, want: 0},
		{name: "开启收藏", config: map[string]any{"favorite_score_threshold": 80.5, "position_requirement": "保留岗位条件"}, want: 80.5},
		{name: "最高分边界", config: map[string]any{"favorite_score_threshold": 100, "position_requirement": "保留岗位条件"}, want: 100},
		{name: "填零关闭", config: map[string]any{"favorite_score_threshold": 0, "position_requirement": "保留岗位条件"}, want: 0},
		{name: "空值关闭", config: map[string]any{"favorite_score_threshold": nil, "position_requirement": "保留岗位条件"}, want: 0},
	} {
		t.Run(item.name, func(t *testing.T) {
			resp := savePositionFavoriteForTest(t, routes, token, positionID, item.config)
			if resp.Code != http.StatusOK {
				t.Fatalf("save status = %d, body = %s", resp.Code, resp.Body.String())
			}
			var saved struct {
				Position struct {
					ID       string         `json:"id"`
					AIConfig map[string]any `json:"ai_config"`
				} `json:"position"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
				t.Fatal(err)
			}
			positionID = saved.Position.ID
			if saved.Position.AIConfig["favorite_score_threshold"] != item.want {
				t.Fatalf("保存返回的收藏分数 = %v, want %v", saved.Position.AIConfig["favorite_score_threshold"], item.want)
			}
			listReq := httptest.NewRequest(http.MethodGet, "/api/positions", nil)
			listReq.Header.Set("Authorization", "Bearer "+token)
			listResp := httptest.NewRecorder()
			routes.ServeHTTP(listResp, listReq)
			var listed struct {
				Positions []struct {
					AIConfig map[string]any `json:"ai_config"`
				} `json:"positions"`
			}
			if listResp.Code != http.StatusOK {
				t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
			}
			if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
				t.Fatal(err)
			}
			if len(listed.Positions) != 1 || listed.Positions[0].AIConfig["favorite_score_threshold"] != item.want || listed.Positions[0].AIConfig["position_requirement"] != "保留岗位条件" {
				t.Fatalf("岗位回读分数或原配置不正确：%+v", listed.Positions)
			}
		})
	}
}

// TestPositionFavoriteScoreRejectsInvalidUpdate 验证无效分数返回提示且不覆盖已保存的收藏条件。
func TestPositionFavoriteScoreRejectsInvalidUpdate(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-favorite-invalid@example.com"
	token := loginForTest(t, routes, email)
	initial := savePositionFavoriteForTest(t, routes, token, "", map[string]any{"favorite_score_threshold": 75})
	if initial.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", initial.Code, initial.Body.String())
	}
	var saved struct {
		Position struct {
			ID string `json:"id"`
		} `json:"position"`
	}
	if err := json.NewDecoder(initial.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name  string
		value any
	}{
		{name: "负数", value: -1},
		{name: "超过百分", value: 100.01},
		{name: "数字字符串", value: "80"},
		{name: "非数字", value: "abc"},
		{name: "布尔值", value: true},
		{name: "对象", value: map[string]any{"score": 80}},
	} {
		t.Run(item.name, func(t *testing.T) {
			resp := savePositionFavoriteForTest(t, routes, token, saved.Position.ID, map[string]any{"favorite_score_threshold": item.value})
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("save status = %d, body = %s", resp.Code, resp.Body.String())
			}
			position, err := server.positions.store.PositionByID("", email, saved.Position.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if position.AIConfig["favorite_score_threshold"] != float64(75) {
				t.Fatalf("无效保存改写了原收藏分数：%v", position.AIConfig["favorite_score_threshold"])
			}
		})
	}
}

// savePositionFavoriteForTest 通过真实岗位保存路由提交收藏配置并返回接口响应。
func savePositionFavoriteForTest(t *testing.T, routes http.Handler, token, positionID string, config map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"id": positionID, "name": "收藏测试岗位", "ai_config": config})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/positions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	return resp
}
