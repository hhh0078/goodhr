// 本文件定义官网纯文本文章合同、超管维护和匿名读取接口，草稿及未到发布时间的文章不公开。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const articleTitleLimit = 200
const articleContentLimit = 200000
const articleExcerptLimit = 160

// ArticleMetadata 是站点地图及文章目录所需的最小元数据，不包含正文或草稿状态。
type ArticleMetadata struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Article 保存稳定链接、纯文本正文和独立的发布开关及时间。
type Article struct {
	ArticleMetadata
	Content     string    `json:"content"`
	IsPublished bool      `json:"is_published"`
	CreatedAt   time.Time `json:"created_at"`
}

// ArticleSummary 用于分页列表，摘要压缩显示但不会改写原始正文。
type ArticleSummary struct {
	ArticleMetadata
	Excerpt     string    `json:"excerpt"`
	IsPublished bool      `json:"is_published"`
	CreatedAt   time.Time `json:"created_at"`
}

// ArticleQuery 区分超管查询、公开查询与最小目录投影，时间在一次查询内保持一致。
type ArticleQuery struct {
	Page, PageSize int
	Keyword        string
	Published      *bool
	PublicOnly     bool
	CatalogOnly    bool
	Now            time.Time
}

// ArticlePage 返回分页摘要与同一快照中的总数。
type ArticlePage struct {
	Items                 []ArticleSummary
	Total, Page, PageSize int
}

// ArticleLink 仅返回公开文章标识，供超管一次性复制所有完整链接。
type ArticleLink struct {
	ID string `json:"id"`
}

// ArticleStore 复用内存与PostgreSQL双实现模式，公开过滤必须在存储查询内完成。
type ArticleStore interface {
	ListArticles(ArticleQuery) (ArticlePage, error)
	GetArticle(id string, publicOnly bool, now time.Time) (Article, error)
	CreateArticle(Article) (Article, error)
	UpdateArticle(Article) (Article, error)
	DeleteArticle(id string) error
	PublishedArticleLinks(now time.Time) ([]ArticleLink, error)
}

// ArticleService 只管理官网内容，不访问招聘浏览器、本地程序或租户业务数据。
type ArticleService struct {
	auth  *AuthService
	store ArticleStore
	now   func() time.Time
}

// NewArticleService 复用全站超级管理员权限及已有JSON响应通道组装文章服务。
func NewArticleService(auth *AuthService, store ArticleStore) *ArticleService {
	return &ArticleService{auth: auth, store: store, now: time.Now}
}

// requireSuperAdmin 仅允许既有全站超管网页会话维护文章，团队管理员不获额外权限。
func (s *ArticleService) requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.auth == nil {
		writeError(w, http.StatusServiceUnavailable, "文章管理还没准备好，请稍后再试。")
		return false
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "先登录，再继续整理文章吧。")
		return false
	}
	if session.Kind == extensionSessionKind || !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "这一步需要全站管理员来处理。")
		return false
	}
	return true
}

// AdminCollection 分发超管分页列表与创建请求，写入成功后返回完整文章。
func (s *ArticleService) AdminCollection(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.list(w, r, false, false)
	case http.MethodPost:
		now := s.now().UTC()
		item, err := decodeArticle(w, r, Article{ArticleMetadata: ArticleMetadata{PublishedAt: now, UpdatedAt: now}, CreatedAt: now})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item.ID, err = newArticleID()
		if err == nil {
			item, err = s.store.CreateArticle(item)
		}
		if err != nil {
			writeArticleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "article": item})
	default:
		writeError(w, http.StatusMethodNotAllowed, "这个文章操作暂时不支持。")
	}
}

// AdminDetail 按稳定UUID读取、完整更新或删除文章，修改不会改变原始创建时间。
func (s *ArticleService) AdminDetail(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	id, valid := articlePathID(r.URL.Path, "/api/admin/articles/")
	if !valid {
		writeArticleError(w, ErrNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetArticle(id, false, s.now().UTC())
		if err != nil {
			writeArticleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "article": item})
	case http.MethodPut:
		item, err := s.store.GetArticle(id, false, s.now().UTC())
		if err != nil {
			writeArticleError(w, err)
			return
		}
		item, err = decodeArticle(w, r, item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item.UpdatedAt = s.now().UTC()
		item, err = s.store.UpdateArticle(item)
		if err != nil {
			writeArticleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "article": item})
	case http.MethodDelete:
		if err := s.store.DeleteArticle(id); err != nil {
			writeArticleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "这个文章操作暂时不支持。")
	}
}

// AdminLinks 在单次数据库快照中导出全部当前公开文章ID，不受后台搜索或分页筛选影响。
func (s *ArticleService) AdminLinks(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "文章链接只支持读取。")
		return
	}
	items, err := s.store.PublishedArticleLinks(s.now().UTC())
	if err != nil {
		writeArticleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": safeSlice(items)})
}

// PublicCollection 为官网匿名访问提供已发布且到期的分页摘要。
func (s *ArticleService) PublicCollection(w http.ResponseWriter, r *http.Request) {
	s.list(w, r, true, false)
}

// PublicCatalog 为站点地图提供有界分页元数据，不查询或传输正文内容。
func (s *ArticleService) PublicCatalog(w http.ResponseWriter, r *http.Request) {
	s.list(w, r, true, true)
}

// PublicDetail 隐藏草稿、计划文章与不存在文章的差异，正文以JSON字符串原样返回。
func (s *ArticleService) PublicDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "文章只支持读取。")
		return
	}
	id, valid := articlePathID(r.URL.Path, "/api/public/articles/")
	if !valid {
		writeArticleError(w, ErrNotFound)
		return
	}
	item, err := s.store.GetArticle(id, true, s.now().UTC())
	if err != nil {
		writeArticleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "article": item})
}

// list 共用分页解析与存储查询，公开目录严格投影为最小元数据。
func (s *ArticleService) list(w http.ResponseWriter, r *http.Request, publicOnly, catalogOnly bool) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "文章列表只支持读取。")
		return
	}
	query, err := articleQuery(r, publicOnly, catalogOnly, s.now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.store.ListArticles(query)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	var items any = safeSlice(result.Items)
	if catalogOnly {
		metadata := make([]ArticleMetadata, 0, len(result.Items))
		for _, item := range result.Items {
			metadata = append(metadata, item.ArticleMetadata)
		}
		items = metadata
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

// articleQuery 校验分页防止整数溢出，公开调用不接受后台的草稿状态过滤。
func articleQuery(r *http.Request, publicOnly, catalogOnly bool, now time.Time) (ArticleQuery, error) {
	query := ArticleQuery{Page: 1, PageSize: 20, PublicOnly: publicOnly, CatalogOnly: catalogOnly, Now: now}
	maxSize := 100
	if catalogOnly {
		query.PageSize, maxSize = 100, 1000
	}
	for _, field := range []struct {
		name  string
		value *int
	}{{"page", &query.Page}, {"page_size", &query.PageSize}} {
		if value := r.URL.Query().Get(field.name); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 {
				return ArticleQuery{}, errors.New("页码或每页数量没读明白，请重新选择。")
			}
			*field.value = parsed
		}
	}
	query.PageSize = min(query.PageSize, maxSize)
	if query.Page > (int(^uint(0)>>1) / query.PageSize) {
		return ArticleQuery{}, errors.New("页码有点大，请重新选择。")
	}
	if !publicOnly {
		query.Keyword = strings.TrimSpace(r.URL.Query().Get("q"))
		if utf8.RuneCountInString(query.Keyword) > articleTitleLimit || strings.ContainsRune(query.Keyword, 0) {
			return ArticleQuery{}, errors.New("搜索标题请控制在200字内。")
		}
		if state := r.URL.Query().Get("is_published"); state != "" {
			if state != "true" && state != "false" {
				return ArticleQuery{}, errors.New("发布状态没读明白，请重新选择。")
			}
			published := state == "true"
			query.Published = &published
		}
	}
	return query, nil
}

// decodeArticle 限制正文大小并保留文本换行，正文里的HTML标记只作为普通文字保存。
func decodeArticle(w http.ResponseWriter, r *http.Request, previous Article) (Article, error) {
	var input struct {
		Title       string     `json:"title"`
		Content     string     `json:"content"`
		PublishedAt *time.Time `json:"published_at"`
		IsPublished bool       `json:"is_published"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Article{}, errors.New("文章内容没读完整，请检查标题、正文和发布时间。")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Article{}, errors.New("请一次保存一篇文章。")
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > articleTitleLimit || strings.ContainsAny(input.Title, "\x00\r\n") {
		return Article{}, errors.New("标题请写一行，控制在1到200字。")
	}
	input.Content = strings.ReplaceAll(strings.ReplaceAll(input.Content, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(input.Content) == "" || utf8.RuneCountInString(input.Content) > articleContentLimit || strings.ContainsRune(input.Content, 0) {
		return Article{}, errors.New("正文请填一下，最多20万字。")
	}
	if input.PublishedAt != nil {
		if input.PublishedAt.IsZero() || input.PublishedAt.Year() < 1 || input.PublishedAt.Year() > 9999 {
			return Article{}, errors.New("发布时间没读明白，请重新选择。")
		}
		previous.PublishedAt = input.PublishedAt.UTC()
	}
	previous.Title, previous.Content, previous.IsPublished = input.Title, input.Content, input.IsPublished
	return previous, nil
}

// articleExcerpt 压缩摘要中的空白并限制字数，完整正文及其换行保持原样。
func articleExcerpt(content string) string {
	text := strings.Join(strings.Fields(content), " ")
	runes := []rune(text)
	if len(runes) > articleExcerptLimit {
		return string(runes[:articleExcerptLimit])
	}
	return text
}

// normalizeArticlePagination 为直接调用存储的开发或测试路径补齐分页默认值，并拒绝溢出的偏移量。
func normalizeArticlePagination(query ArticleQuery) (ArticleQuery, error) {
	query.Page = max(1, query.Page)
	defaultSize, maxSize := 20, 100
	if query.CatalogOnly {
		defaultSize, maxSize = 100, 1000
	}
	if query.PageSize < 1 {
		query.PageSize = defaultSize
	}
	query.PageSize = min(query.PageSize, maxSize)
	if query.Page > int(^uint(0)>>1)/query.PageSize {
		return ArticleQuery{}, errors.New("页码有点大，请重新选择。")
	}
	return query, nil
}

// newArticleID 生成标准随机UUID作为稳定文章链接，不依赖可预测的自增编号。
func newArticleID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// articlePathID 仅接受固定路径下的UUID，避免拼接额外路径或把无效标识传入数据库。
func articlePathID(path, prefix string) (string, bool) {
	id := strings.TrimPrefix(path, prefix)
	if id == path || len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return "", false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return strings.ToLower(id), err == nil && len(decoded) == 16
}

// writeArticleError 不泄露数据库细节，草稿、计划文章和不存在文章统一返回404。
func writeArticleError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "这篇文章暂时无法查看。")
		return
	}
	writeError(w, http.StatusInternalServerError, "文章没处理成功，请稍后再试。")
}
