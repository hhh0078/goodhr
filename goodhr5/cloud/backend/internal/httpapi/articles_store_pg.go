// 本文件通过独立官网文章表提供参数化读写和发布过滤，目录与链接导出不读取正文。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresArticleStore 复用云端数据库连接保存全站文章，不关联招聘或租户业务表。
type PostgresArticleStore struct {
	db *sql.DB
}

// NewPostgresArticleStore 创建官网文章PostgreSQL存储，不在构造时执行迁移或修改数据。
func NewPostgresArticleStore(db *sql.DB) *PostgresArticleStore {
	return &PostgresArticleStore{db: db}
}

// articleWhere 用参数绑定组合公开条件与超管筛选，标题搜索按文字包含而非SQL通配符解释。
func articleWhere(query ArticleQuery) (string, []any) {
	clauses, args := []string{"true"}, []any{}
	if query.PublicOnly {
		args = append(args, query.Now)
		clauses = append(clauses, "is_published=true", "published_at<=$1")
	} else if query.Published != nil {
		args = append(args, *query.Published)
		clauses = append(clauses, "is_published=$1")
	}
	if query.Keyword != "" {
		args = append(args, query.Keyword)
		clauses = append(clauses, fmt.Sprintf("strpos(lower(title),lower($%d))>0", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// ListArticles 在同一只读快照内计算总数与分页，目录分支不选择正文或摘要表达式。
func (s *PostgresArticleStore) ListArticles(query ArticleQuery) (ArticlePage, error) {
	query, err := normalizeArticlePagination(query)
	if err != nil {
		return ArticlePage{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ArticlePage{}, err
	}
	defer tx.Rollback()
	where, args := articleWhere(query)
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM public_articles WHERE `+where, args...).Scan(&total); err != nil {
		return ArticlePage{}, err
	}
	projection := "''"
	if !query.CatalogOnly {
		projection = `left(btrim(regexp_replace(content,'[[:space:]]+',' ','g')),160)`
	}
	args = append(args, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := tx.QueryContext(ctx, `SELECT id::text,title,published_at,updated_at,is_published,created_at,`+projection+` FROM public_articles WHERE `+where+fmt.Sprintf(` ORDER BY published_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return ArticlePage{}, err
	}
	items := []ArticleSummary{}
	for rows.Next() {
		var item ArticleSummary
		if err = rows.Scan(&item.ID, &item.Title, &item.PublishedAt, &item.UpdatedAt, &item.IsPublished, &item.CreatedAt, &item.Excerpt); err != nil {
			rows.Close()
			return ArticlePage{}, err
		}
		item.PublishedAt, item.UpdatedAt, item.CreatedAt = item.PublishedAt.UTC(), item.UpdatedAt.UTC(), item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return ArticlePage{}, err
	}
	if err = rows.Close(); err != nil {
		return ArticlePage{}, err
	}
	if err = tx.Commit(); err != nil {
		return ArticlePage{}, err
	}
	return ArticlePage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

// scanArticle 复用查询和写入返回的完整文章扫描，不向调用者泄露数据库无记录错误。
func scanArticle(row interface{ Scan(...any) error }) (Article, error) {
	var item Article
	err := row.Scan(&item.ID, &item.Title, &item.Content, &item.PublishedAt, &item.IsPublished, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	item.PublishedAt, item.UpdatedAt, item.CreatedAt = item.PublishedAt.UTC(), item.UpdatedAt.UTC(), item.CreatedAt.UTC()
	return item, err
}

const articleColumns = `id::text,title,content,published_at,is_published,created_at,updated_at`

// GetArticle 在数据库读取时执行公开条件，防止草稿或计划文章经详情接口泄露。
func (s *PostgresArticleStore) GetArticle(id string, publicOnly bool, now time.Time) (Article, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	where, args := "id=$1::uuid", []any{id}
	if publicOnly {
		where += " AND is_published=true AND published_at<=$2"
		args = append(args, now)
	}
	return scanArticle(s.db.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM public_articles WHERE `+where, args...))
}

// CreateArticle 插入完整合法文章并返回数据库中的实际时间与正文，不解释HTML内容。
func (s *PostgresArticleStore) CreateArticle(item Article) (Article, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return scanArticle(s.db.QueryRowContext(ctx, `INSERT INTO public_articles(id,title,content,published_at,is_published,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7) RETURNING `+articleColumns, item.ID, item.Title, item.Content, item.PublishedAt, item.IsPublished, item.CreatedAt, item.UpdatedAt))
}

// UpdateArticle 原子更新当前文章并保持创建时间；并发删除时返回不存在而不重新插入。
func (s *PostgresArticleStore) UpdateArticle(item Article) (Article, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return scanArticle(s.db.QueryRowContext(ctx, `UPDATE public_articles SET title=$2,content=$3,published_at=$4,is_published=$5,updated_at=$6 WHERE id=$1::uuid RETURNING `+articleColumns, item.ID, item.Title, item.Content, item.PublishedAt, item.IsPublished, item.UpdatedAt))
}

// DeleteArticle 仅按UUID删除指定文章，空结果按不存在处理。
func (s *PostgresArticleStore) DeleteArticle(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM public_articles WHERE id=$1::uuid`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}

// PublishedArticleLinks 用单条只读SQL导出全部已到发布时间的文章ID，不加载正文且不受列表筛选影响。
func (s *PostgresArticleStore) PublishedArticleLinks(now time.Time) ([]ArticleLink, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM public_articles WHERE is_published=true AND published_at<=$1 ORDER BY published_at DESC,id DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ArticleLink{}
	for rows.Next() {
		var item ArticleLink
		if err = rows.Scan(&item.ID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
