-- 官网文章仅新增独立表和查询索引，不改动现有业务表、数据或发布状态。
CREATE TABLE IF NOT EXISTS public_articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    content TEXT NOT NULL CHECK (char_length(content) BETWEEN 1 AND 200000),
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_published BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE public_articles IS '官网文章，由全站超级管理员维护；仅已发布且发布时间已到的文章公开可读';
COMMENT ON COLUMN public_articles.id IS '文章稳定UUID，同时作为官网文章详情链接标识';
COMMENT ON COLUMN public_articles.title IS '文章标题，最多200个字符';
COMMENT ON COLUMN public_articles.content IS '保留换行的纯文本正文，不解释为HTML，最多200000个字符';
COMMENT ON COLUMN public_articles.published_at IS '文章计划发布时间，已发布且此时间不晚于当前时间才公开可见';
COMMENT ON COLUMN public_articles.is_published IS '发布开关，false为草稿，true仍需满足发布时间已到';
COMMENT ON COLUMN public_articles.created_at IS '文章首次创建时间，编辑时保持不变';
COMMENT ON COLUMN public_articles.updated_at IS '文章最近保存时间，供官网和站点地图更新使用';

CREATE INDEX IF NOT EXISTS idx_public_articles_published_time
    ON public_articles (published_at DESC, id DESC) WHERE is_published = true;
CREATE INDEX IF NOT EXISTS idx_public_articles_admin_time
    ON public_articles (published_at DESC, id DESC);
