-- Pending production changes and temporary dev clones. Forward-only.

alter table servers add column if not exists pending_plugins boolean not null default false;
alter table servers add column if not exists pending_files jsonb not null default '[]'::jsonb;
alter table servers add column if not exists pending_since timestamptz;

alter table servers add column if not exists dev_of text not null default '';
alter table servers add column if not exists dev_whitelist jsonb not null default '[]'::jsonb;
alter table servers add column if not exists dev_expires_at timestamptz;
