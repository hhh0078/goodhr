create table if not exists accounts (
  id bigserial primary key,
  identifier text not null unique,
  identity_type text not null,
  settings jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_accounts_identity_type on accounts(identity_type);
