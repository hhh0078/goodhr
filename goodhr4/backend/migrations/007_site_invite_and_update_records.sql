alter table if exists accounts
  add column if not exists inviter_id bigint;

create index if not exists idx_accounts_inviter_id on accounts(inviter_id);

create table if not exists update_records (
  id bigserial primary key,
  version text not null,
  title text not null default '',
  content text not null default '',
  force_update boolean not null default false,
  published_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_update_records_published_at on update_records (published_at desc, id desc);

insert into update_records (version, title, content, force_update, published_at)
select
  '4.1.0',
  '4.1.0 发布',
  '优化配置结构与广告位展示。',
  false,
  now()
where not exists (
  select 1 from update_records where version = '4.1.0'
);
