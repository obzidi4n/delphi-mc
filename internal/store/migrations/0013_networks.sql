create table if not exists networks (
    id              text primary key,
    org_id          text not null,
    name            text not null,
    proxy_server_id text not null default '',
    mode            text not null default 'legacy',
    created_at      timestamptz not null default now(),
    unique (org_id, name)
);

alter table servers add column if not exists network_id text not null default '';
alter table servers add column if not exists network_role text not null default '';
