-- Artifact CAS metadata (files live under the panel's data directory) and
-- managed plugins resolved for each server.
create table if not exists artifacts (
    sha256     text primary key,
    filename   text not null,
    size       bigint not null,
    kind       text not null,
    source     text not null,
    created_at timestamptz not null default now()
);

alter table servers add column if not exists mc_version text not null default '';
alter table servers add column if not exists plugins jsonb not null default '[]'::jsonb;
