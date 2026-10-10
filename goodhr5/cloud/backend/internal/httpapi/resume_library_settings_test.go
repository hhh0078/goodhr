// 文件作用：验证简历库账号设置默认开启、仅修改指定字段、账号隔离及60秒缓存即时刷新。
package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type countingResumeSettingsStore struct {
	ResumeLibrarySettingsStore
	reads int
}

// Get 记录设置持久层读取次数，验证轮询不重复访问数据库。
func (s *countingResumeSettingsStore) Get(email string) (ResumeLibrarySettings, error) {
	s.reads++
	return s.ResumeLibrarySettingsStore.Get(email)
}

// TestResumeLibrarySettingsDefaultScopeAndCache 验证默认值、PATCH严格字段以及修改后立即读取新值。
func TestResumeLibrarySettingsDefaultScopeAndCache(t *testing.T) {
	store := &countingResumeSettingsStore{ResumeLibrarySettingsStore: NewMemoryResumeLibrarySettingsStore()}
	service := NewResumeLibrarySettingsService(messageTestAuth(t), store)
	now := time.Now()
	service.now = func() time.Time { return now }
	response := messageRequest(service.User, http.MethodGet, "/api/resume-library/settings", "owner", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"auto_import_downloads":true`) {
		t.Fatal(response.Body.String())
	}
	for i := 0; i < 5; i++ {
		now = now.Add(10 * time.Second)
		response = messageRequest(service.User, "GET", "/api/resume-library/settings", "owner", "")
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	if store.reads != 1 {
		t.Fatalf("read count=%d", store.reads)
	}
	response = messageRequest(service.User, "PATCH", "/api/resume-library/settings", "owner", `{"auto_import_downloads":false}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"auto_import_downloads":false`) {
		t.Fatal(response.Body.String())
	}
	response = messageRequest(service.User, "GET", "/api/resume-library/settings", "owner", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"auto_import_downloads":false`) || store.reads != 1 {
		t.Fatal(response.Body.String(), store.reads)
	}
	response = messageRequest(service.User, "GET", "/api/resume-library/settings", "other", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"auto_import_downloads":true`) {
		t.Fatal(response.Body.String())
	}
	for _, body := range []string{`{}`, `{"auto_import_downloads":null}`, `{"auto_import_downloads":"false"}`, `{"auto_import_downloads":true,"ai_model":"overwrite"}`, `{"auto_import_downloads":true,"user_id":"other"}`} {
		response = messageRequest(service.User, "PATCH", "/api/resume-library/settings", "owner", body)
		if response.Code != 400 {
			t.Fatal(response.Body.String())
		}
	}
	response = messageRequest(service.User, "GET", "/api/resume-library/settings", "plugin", "")
	if response.Code != 403 {
		t.Fatal(response.Body.String())
	}
	now = now.Add(61 * time.Second)
	response = messageRequest(service.User, "GET", "/api/resume-library/settings", "owner", "")
	if response.Code != 200 || store.reads != 3 {
		t.Fatal(response.Body.String(), store.reads)
	}
}
