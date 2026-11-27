-- Identity, authorization, audit, groups, and the per-server summary row.

create table sessions (
    id         text primary key,
    user_id    text        not null references users (id) on delete cascade,
    expires_at timestamptz not null,
    created_at timestamptz not null default now()
);
create index sessions_user_idx on sessions (user_id);

create table api_keys (
    id           text primary key,
    org_id       text        not null references orgs (id) on delete cascade,
    user_id      text        references users (id) on delete cascade,
    name         text        not null,
    key_hash     bytea       not null unique,
    scopes       text[]      not null default '{}',
    created_at   timestamptz not null default now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

create table role_bindings (
    id         text primary key,
    user_id    text not null references users (id) on delete cascade,
    org_id     text not null references orgs (id) on delete cascade,
    scope_kind text not null check (scope_kind in ('org', 'group', 'server')),
    scope_id   text not null default '',
    role       text not null check (role in ('owner', 'admin', 'viewer')),
    unique (user_id, scope_kind, scope_id)
);

create table audit_log (
    seq        bigserial primary key,
    org_id     text        not null,
    actor      text        not null,
    via        text        not null,
    action     text        not null,
    target     text        not null,
    diff       jsonb       not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);
create index audit_org_seq_idx on audit_log (org_id, seq desc);

create table server_groups (
    id          text primary key,
    org_id      text        not null references orgs (id) on delete cascade,
    name        text        not null,
    description text        not null default '',
    created_at  timestamptz not null default now(),
    unique (org_id, name)
);

create table server_group_members (
    group_id  text not null references server_groups (id) on delete cascade,
    server_id text not null references servers (id) on delete cascade,
    primary key (group_id, server_id)
);

create table server_summary (
    server_id  text primary key references servers (id) on delete cascade,
    state      text        not null default 'unknown',
    updated_at timestamptz not null default now()
);

alter table servers add column deleted_at timestamptz;
alter table servers add column purge_after timestamptz;
create index servers_deleted_idx on servers (deleted_at);

insert into server_summary (server_id, state)
select id, observed_state from servers on conflict (server_id) do nothing;
