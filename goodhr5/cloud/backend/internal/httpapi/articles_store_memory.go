// 本文件实现官网文章的内存存储，供未启用PostgreSQL的开发与隔离HTTP测试使用。
package httpapi

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryArticleStore 用读写锁隔离文章读取与保存，记录均为不含可变集合的值副本。
type MemoryArticleStore struct {
	mu    sync.RWMutex
	items map[string]Article
}

// NewMemoryArticleStore 创建默认空文章库，不自动添加或发布示例内容。
func NewMemoryArticleStore() *MemoryArticleStore {
	return &MemoryArticleStore{items: map[string]Article{}}
}

// ListArticles 先过滤发布状态再稳定排序分页，目录投影不包含正文摘要。
func (s *MemoryArticleStore) ListArticles(query ArticleQuery) (ArticlePage, error) {
	query, err := normalizeArticlePagination(query)
	if err != nil {
		return ArticlePage{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []ArticleSummary{}
	for _, item := range s.items {
		if query.PublicOnly && (!item.IsPublished || item.PublishedAt.After(query.Now)) || !query.PublicOnly && query.Published != nil && item.IsPublished != *query.Published || query.Keyword != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(query.Keyword)) {
			continue
		}
		summary := ArticleSummary{ArticleMetadata: item.ArticleMetadata, IsPublished: item.IsPublished, CreatedAt: item.CreatedAt}
		if !query.CatalogOnly {
			summary.Excerpt = articleExcerpt(item.Content)
		}
		items = append(items, summary)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	total := len(items)
	start := (query.Page - 1) * query.PageSize
	if start >= total {
		items = []ArticleSummary{}
	} else {
		items = items[start:min(start+query.PageSize, total)]
	}
	return ArticlePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

// GetArticle 按公开规则读取值副本，草稿和未到发布时间的文章统一按不存在处理。
func (s *MemoryArticleStore) GetArticle(id string, publicOnly bool, now time.Time) (Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, found := s.items[id]
	if !found || publicOnly && (!item.IsPublished || item.PublishedAt.After(now)) {
		return Article{}, ErrNotFound
	}
	return item, nil
}

// CreateArticle 保存新文章并拒绝复用现有稳定标识。
func (s *MemoryArticleStore) CreateArticle(item Article) (Article, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.items[item.ID]; found {
		return Article{}, ErrAlreadyExists
	}
	s.items[item.ID] = item
	return item, nil
}

// UpdateArticle 仅更新现有文章，保留首次创建时间和稳定标识。
func (s *MemoryArticleStore) UpdateArticle(item Article) (Article, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, found := s.items[item.ID]
	if !found {
		return Article{}, ErrNotFound
	}
	item.CreatedAt = previous.CreatedAt
	s.items[item.ID] = item
	return item, nil
}

// DeleteArticle 只移除指定文章，重复删除不会误报为成功。
func (s *MemoryArticleStore) DeleteArticle(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.items[id]; !found {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}

// PublishedArticleLinks 在单个读锁快照内读取全部当前可见文章ID，避免逐页复制时遗漏或重复。
func (s *MemoryArticleStore) PublishedArticleLinks(now time.Time) ([]ArticleLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := []ArticleMetadata{}
	for _, item := range s.items {
		if item.IsPublished && !item.PublishedAt.After(now) {
			items = append(items, item.ArticleMetadata)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	links := make([]ArticleLink, 0, len(items))
	for _, item := range items {
		links = append(links, ArticleLink{ID: item.ID})
	}
	return links, nil
}
