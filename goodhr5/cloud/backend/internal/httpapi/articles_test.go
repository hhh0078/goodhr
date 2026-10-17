// 本文件使用合成文章验证超管权限、匿名发布过滤、分页、纯文本换行及稳定链接，不访问真实业务数据。
package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestArticlesHTTP 通过完整路由验证文章CRUD、公开边界和文本安全合同。
func TestArticlesHTTP(t *testing.T) {
	exerciseArticlesHTTP(t, mustNewServer(t))
}

// exerciseArticlesHTTP 为内存和真实数据库存储执行同一套HTTP断言，避免仅验证模拟结果。
func exerciseArticlesHTTP(t *testing.T, server *Server) {
	t.Helper()
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	userToken := loginForTest(t, routes, "article-synthetic-owner@example.com")
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	server.articles.now = func() time.Time { return now }
	for _, path := range []string{"/api/admin/articles", "/api/admin/articles/links", "/api/admin/articles/00000000-0000-4000-8000-000000000001"} {
		for _, check := range []struct {
			token  string
			status int
		}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
			status, _ := candidateTestRequest(t, routes, check.token, http.MethodGet, path, nil)
			if status != check.status {
				t.Fatalf("admin permission path=%s status=%d", path, status)
			}
		}
	}
	content := "  第一段保留缩进\r\n\r\n<script>alert('只是文字')</script>\r\n最后一行\n"
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	visibleIDs := []string{}
	var draftID, futureID, originalID string
	var original map[string]any
	for _, input := range []struct {
		title     string
		offset    time.Duration
		published bool
	}{{"草稿合成文章", -time.Hour, false}, {"未来合成文章", time.Hour, true}, {"过去合成文章", -2 * time.Hour, true}, {"当前文章 A_100% ' OR 1=1 --", 0, true}, {"当前合成文章B", 0, true}} {
		status, response := candidateTestRequest(t, routes, adminToken, http.MethodPost, "/api/admin/articles", map[string]any{"title": "  " + input.title + "  ", "content": content, "published_at": now.Add(input.offset).Format(time.RFC3339), "is_published": input.published})
		if status != http.StatusOK {
			t.Fatalf("create article status=%d response=%v", status, response)
		}
		article := response["article"].(map[string]any)
		id := article["id"].(string)
		if parsed, valid := articlePathID("/articles/"+id, "/articles/"); !valid || parsed != id || article["title"] != input.title || article["content"] != normalized {
			t.Fatal("article ID, title normalization or newlines changed")
		}
		if !input.published {
			draftID = id
		} else if input.offset > 0 {
			futureID = id
		} else {
			visibleIDs = append(visibleIDs, id)
			if input.offset < 0 {
				originalID, original = id, article
			}
		}
	}
	for _, id := range []string{draftID, futureID, "00000000-0000-4000-8000-000000000000", "bad-id"} {
		status, response := candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/articles/"+id, nil)
		if status != http.StatusNotFound || response["error"] != "这篇文章暂时无法查看。" {
			t.Fatalf("public hidden detail leaked id=%s status=%d", id, status)
		}
	}
	for _, prefix := range []string{"/api/public/articles/", "/api/admin/articles/"} {
		status, _ := candidateTestRequest(t, routes, adminToken, http.MethodGet, prefix+"--111111-1111-4111-8111-111111111111", nil)
		if status != http.StatusNotFound {
			t.Fatal("malformed UUID reached article storage", prefix, status)
		}
	}
	status, response := candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/articles?page=1&page_size=1&is_published=false&q=草稿", nil)
	if status != http.StatusOK || response["total"] != float64(3) || len(response["items"].([]any)) != 1 {
		t.Fatal("public paging or forced publication filter", response)
	}
	first := response["items"].([]any)[0].(map[string]any)
	if _, contentPresent := first["content"]; contentPresent || !strings.Contains(first["excerpt"].(string), "第一段保留缩进") || strings.Contains(first["excerpt"].(string), "\n") {
		t.Fatal("list must expose compact excerpt rather than full content")
	}
	seen := []string{}
	for page := 1; page <= 4; page++ {
		status, response = candidateTestRequest(t, routes, "", http.MethodGet, fmt.Sprintf("/api/public/articles/catalog?page=%d&page_size=1", page), nil)
		if status != http.StatusOK || response["total"] != float64(3) {
			t.Fatal("catalog pagination", response)
		}
		for _, raw := range response["items"].([]any) {
			item := raw.(map[string]any)
			if len(item) != 4 || item["id"] == nil || item["title"] == nil || item["published_at"] == nil || item["updated_at"] == nil {
				t.Fatal("catalog exposed more than metadata", item)
			}
			seen = append(seen, item["id"].(string))
		}
	}
	sort.Strings(seen)
	sort.Strings(visibleIDs)
	if fmt.Sprint(seen) != fmt.Sprint(visibleIDs) {
		t.Fatal("catalog lost or duplicated public articles")
	}
	status, response = candidateTestRequest(t, routes, adminToken, http.MethodGet, "/api/admin/articles/links?page=99&page_size=1&q=不存在&is_published=false", nil)
	if status != http.StatusOK || len(response["items"].([]any)) != 3 {
		t.Fatal("all links export incorrectly inherited list filters", response)
	}
	for _, raw := range response["items"].([]any) {
		if len(raw.(map[string]any)) != 1 {
			t.Fatal("links export must expose ID only")
		}
	}
	for _, check := range []struct {
		query string
		total int
	}{{"", 5}, {"?is_published=false", 1}, {"?is_published=true", 4}, {"?q=" + url.QueryEscape("A_100% ' OR 1=1 --"), 1}} {
		status, response = candidateTestRequest(t, routes, adminToken, http.MethodGet, "/api/admin/articles"+check.query, nil)
		if status != http.StatusOK || response["total"] != float64(check.total) {
			t.Fatal("admin article filter", check.query, response)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/public/articles/"+originalID, nil)
	writer := httptest.NewRecorder()
	routes.ServeHTTP(writer, request)
	if writer.Code != http.StatusOK || strings.Contains(writer.Body.String(), "<script>") || !strings.Contains(writer.Body.String(), `\u003cscript\u003e`) {
		t.Fatal("article JSON must escape HTML while preserving plain text")
	}
	now = now.Add(30 * time.Minute)
	status, response = candidateTestRequest(t, routes, adminToken, http.MethodPut, "/api/admin/articles/"+originalID, map[string]any{"title": "更新后的稳定链接", "content": "第一行\n\n第二行", "published_at": now.Add(time.Hour).Format(time.RFC3339), "is_published": true})
	if status != http.StatusOK {
		t.Fatal(response)
	}
	updated := response["article"].(map[string]any)
	if updated["id"] != originalID || updated["created_at"] != original["created_at"] || updated["updated_at"] == original["updated_at"] || updated["content"] != "第一行\n\n第二行" {
		t.Fatal("update changed stable ID/created_at or lost newlines")
	}
	status, _ = candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/articles/"+originalID, nil)
	if status != http.StatusNotFound {
		t.Fatal("future reschedule remained public")
	}
	status, response = candidateTestRequest(t, routes, adminToken, http.MethodPut, "/api/admin/articles/"+originalID, map[string]any{"title": "转成草稿", "content": "原样正文"})
	if status != http.StatusOK || response["article"].(map[string]any)["is_published"] != false || response["article"].(map[string]any)["published_at"] != updated["published_at"] {
		t.Fatal("omitted date must retain old date and omitted publish switch means draft", response)
	}
	status, response = candidateTestRequest(t, routes, adminToken, http.MethodPost, "/api/admin/articles", map[string]any{"title": "省略时间的草稿", "content": "草稿正文"})
	if status != http.StatusOK || response["article"].(map[string]any)["published_at"] != now.Format(time.RFC3339) || response["article"].(map[string]any)["is_published"] != false {
		t.Fatal("new draft defaults", response)
	}
	_, exchange := candidateTestRequest(t, routes, adminToken, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	for _, path := range []string{"/api/admin/articles", "/api/admin/articles/links"} {
		status, _ = candidateTestRequest(t, routes, exchange["token"].(string), http.MethodGet, path, nil)
		if status != http.StatusForbidden {
			t.Fatal("extension token gained article administration")
		}
	}
	status, _ = candidateTestRequest(t, routes, adminToken, http.MethodDelete, "/api/admin/articles/"+originalID, nil)
	if status != http.StatusOK {
		t.Fatal("article deletion failed")
	}
	for _, path := range []string{"/api/admin/articles/" + originalID, "/api/public/articles/" + originalID} {
		status, _ = candidateTestRequest(t, routes, adminToken, http.MethodGet, path, nil)
		if status != http.StatusNotFound {
			t.Fatal("deleted article still readable", path)
		}
	}
	status, _ = candidateTestRequest(t, routes, adminToken, http.MethodDelete, "/api/admin/articles/"+originalID, nil)
	if status != http.StatusNotFound {
		t.Fatal("repeated deletion incorrectly succeeded")
	}
}

// TestArticlePathID 验证固定UUID的解码长度，额外连字符不能绕过格式检查进入数据库类型转换。
func TestArticlePathID(t *testing.T) {
	valid := "abcdef12-abcd-4abc-8abc-abcdef123456"
	for _, value := range []string{valid, strings.ToUpper(valid)} {
		id, ok := articlePathID("/articles/"+value, "/articles/")
		if !ok || id != valid {
			t.Fatal("valid article UUID rejected", value)
		}
	}
	for _, value := range []string{"--111111-1111-4111-8111-111111111111", "11111111-1111-4111-8111-11111111111g", valid + "/extra", ""} {
		if _, ok := articlePathID("/articles/"+value, "/articles/"); ok {
			t.Fatal("malformed article UUID accepted", value)
		}
	}
}

// TestArticlesInputValidation 验证正文、时间、JSON和分页边界，非法请求不能创建文章。
func TestArticlesInputValidation(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "1224299352@qq.com")
	for _, input := range []map[string]any{
		{"title": "", "content": "正文"}, {"title": strings.Repeat("文", 201), "content": "正文"},
		{"title": "标题\n换行", "content": "正文"}, {"title": "标题", "content": "\t\n "},
		{"title": "标题", "content": strings.Repeat("字", 200001)}, {"title": "标题", "content": "禁止\x00正文"},
		{"title": "标题", "content": "正文", "published_at": "invalid"}, {"title": "标题", "content": "正文", "is_published": "true"},
		{"title": "标题", "content": "正文", "created_at": "2000-01-01T00:00:00Z"},
	} {
		status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/admin/articles", input)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid article accepted status=%d", status)
		}
	}
	for _, body := range []string{`{"title":"标题","content":"正文"} {}`, "[1]", strings.Repeat(" ", 2<<20) + `{}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/admin/articles", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatal("bad JSON/oversized body accepted", w.Code)
		}
	}
	for _, path := range []string{"/api/public/articles", "/api/public/articles/catalog", "/api/admin/articles"} {
		for _, query := range []string{"?page=0", "?page=-1", "?page=x", "?page=999999999999999999999999", "?page=9223372036854775807&page_size=1000", "?page_size=0"} {
			status, _ := candidateTestRequest(t, routes, token, http.MethodGet, path+query, nil)
			if status != http.StatusBadRequest {
				t.Fatal("invalid pagination accepted", path, query, status)
			}
		}
	}
	for _, check := range []struct {
		path string
		size int
	}{{"/api/public/articles?page_size=1000000", 100}, {"/api/public/articles/catalog?page_size=1000000", 1000}} {
		status, response := candidateTestRequest(t, routes, "", http.MethodGet, check.path, nil)
		if status != http.StatusOK || response["page_size"] != float64(check.size) || response["total"] != float64(0) || len(response["items"].([]any)) != 0 {
			t.Fatal("pagination bounds or empty array", response)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		status, _ := candidateTestRequest(t, routes, "", method, "/api/public/articles", map[string]any{})
		if status != http.StatusMethodNotAllowed {
			t.Fatal("public article endpoint accepted mutation", method, status)
		}
	}
}

// TestArticlesMemoryStore 验证开发存储与数据库共用的发布、分页、稳定排序和保存合同。
func TestArticlesMemoryStore(t *testing.T) {
	exerciseArticleStore(t, NewMemoryArticleStore())
}

// exerciseArticleStore 以稳定合成记录验证真实存储行为，正文、时间和ID必须完整往返。
func exerciseArticleStore(t *testing.T, store ArticleStore) {
	t.Helper()
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	content := "\n  首行\n\n" + strings.Repeat("中文正文", 80) + "\n<script>仅文字</script>\n"
	for index := 1; index <= 4; index++ {
		item := Article{ArticleMetadata: ArticleMetadata{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", index), Title: fmt.Sprintf("合成文章%d A_100%%", index), PublishedAt: now, UpdatedAt: now}, Content: content, IsPublished: index != 3, CreatedAt: now.Add(-time.Hour)}
		if index == 4 {
			item.PublishedAt = now.Add(time.Hour)
		}
		saved, err := store.CreateArticle(item)
		if err != nil || saved != item {
			t.Fatal("article round trip", err)
		}
	}
	page, err := store.ListArticles(ArticleQuery{PublicOnly: true, Now: now, PageSize: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != "00000000-0000-4000-8000-000000000002" || utf8.RuneCountInString(page.Items[0].Excerpt) != articleExcerptLimit || strings.Contains(page.Items[0].Excerpt, "\n") {
		t.Fatal("publication, tie order or excerpt", page, err)
	}
	second, err := store.ListArticles(ArticleQuery{PublicOnly: true, Now: now, Page: 2, PageSize: 1, CatalogOnly: true})
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].ID != "00000000-0000-4000-8000-000000000001" || second.Items[0].Excerpt != "" {
		t.Fatal("catalog projection or second page", second, err)
	}
	all, err := store.ListArticles(ArticleQuery{Now: now, Keyword: "a_100%"})
	if err != nil || all.Total != 4 || all.Page != 1 || all.PageSize != 20 {
		t.Fatal("store default page or literal title search", all, err)
	}
	for _, id := range []string{"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004"} {
		if _, err = store.GetArticle(id, true, now); !errors.Is(err, ErrNotFound) {
			t.Fatal("hidden article read publicly", err)
		}
		if _, err = store.GetArticle(id, false, now); err != nil {
			t.Fatal("admin cannot read hidden article", err)
		}
	}
	links, err := store.PublishedArticleLinks(now)
	if err != nil || len(links) != 2 || links[0].ID != "00000000-0000-4000-8000-000000000002" {
		t.Fatal("single-snapshot links", links, err)
	}
	item, err := store.GetArticle(links[0].ID, false, now)
	if err != nil || item.Content != content {
		t.Fatal("original text content was modified", err)
	}
	created := item.CreatedAt
	item.Title, item.Content, item.UpdatedAt, item.CreatedAt, item.IsPublished = "新的标题", "新正文\n\n换行", now.Add(time.Minute), now.Add(24*time.Hour), false
	saved, err := store.UpdateArticle(item)
	if err != nil || saved.ID != item.ID || !saved.CreatedAt.Equal(created) || saved.Content != item.Content {
		t.Fatal("update stable ID/created_at/content", err)
	}
	if _, err = store.GetArticle(item.ID, true, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("unpublished article remained visible")
	}
	if err = store.DeleteArticle(item.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteArticle(item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing deletion was not detected", err)
	}
	if _, err = store.UpdateArticle(item); !errors.Is(err, ErrNotFound) {
		t.Fatal("update re-created deleted article", err)
	}
	page, err = store.ListArticles(ArticleQuery{PublicOnly: true, Now: now, Page: 999})
	if err != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatal("out of range page should be empty", page, err)
	}
}
