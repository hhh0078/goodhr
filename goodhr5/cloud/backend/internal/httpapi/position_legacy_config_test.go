// Package httpapi 本文件验证旧岗位原始配置不会被展示转换掩盖，且所有云端启动入口都会拒绝运行。
package httpapi

import (
	"net/http"
	"reflect"
	"testing"
)

// TestLegacyPositionStartRejectsBeforeNormalization 验证任一旧模式均拒绝启动，重复读取和拒绝不改写数据库配置。
func TestLegacyPositionStartRejectsBeforeNormalization(t *testing.T) {
	for _, tc := range []struct{ name, mode, detail string }{
		{"keyword", "keyword", "dom"}, {"ocr", "ai", "ocr"}, {"both", "keyword", "ocr"}, {"spaces", " KEYWORD ", " OCR "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mustNewServer(t)
			routes := server.Routes()
			email := "legacy-" + tc.name + "@example.com"
			token := loginForTest(t, routes, email)
			bindPositionDeviceForTest(t, routes, token)
			common := map[string]any{"mode_default": tc.mode, "detail_mode": tc.detail, "output_structured_resume": true}
			position, err := server.positionExecution.store.SavePosition(Position{UserEmail: email, PlatformID: "boss", Name: "原始旧岗位", CommonConfig: cloneMap(common)})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				dto := publicPosition(position)
				if dto["requires_recreate"] != true || dto["common_config"].(map[string]any)["mode_default"] != "ai" {
					t.Fatalf("展示转换丢失旧配置标记：%+v", dto)
				}
				for _, suffix := range []string{"start", "status"} {
					response := postPositionExecutionForTest(t, routes, token, "/api/positions/"+position.ID+"/"+suffix, `{"status":"running","task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
					if response.Code != http.StatusConflict || positionStartErrorCodeForTest(t, response) != "POSITION_RECREATE_REQUIRED" {
						t.Fatalf("未拦截%s：%d %s", suffix, response.Code, response.Body.String())
					}
				}
				stored, err := server.positionExecution.store.PositionByID("", email, position.ID, true)
				if err != nil {
					t.Fatal(err)
				}
				if stored.Status == "running" || stored.StartedAt != nil || !reflect.DeepEqual(stored.CommonConfig, common) {
					t.Fatalf("拒绝启动却改写了岗位：%+v", stored)
				}
			}
		})
	}
}

// TestPositionRequiresRecreateOnlyLegacyModes 验证新岗位与缺省字段不误报，仅检查明确停用的两种配置。
func TestPositionRequiresRecreateOnlyLegacyModes(t *testing.T) {
	for _, common := range []map[string]any{nil, {}, {"mode_default": "ai", "detail_mode": "ai"}, {"mode_default": "ai", "detail_mode": "dom"}} {
		position := Position{CommonConfig: common}
		if positionRequiresRecreate(position) || publicPosition(position)["requires_recreate"] != false {
			t.Fatalf("新配置被误报：%+v", common)
		}
	}
}
