-- Fleet spine: nodes, their certificate identity, and single-use enrollment tokens.

create table nodes (
    id              text primary key,
    org_id          text        not null references orgs (id) on delete cascade,
    name            text        not null,
    status          text        not null default 'pending',
    agent_version   text        not null default '',
    fs_capability   text        not null default 'copy',
    capacity        jsonb       not null default '{}'::jsonb,
    cert_serial     text        not null unique,
    cert_expires_at timestamptz not null,
    revoked_at      timestamptz,
    last_seen       timestamptz,
    created_at      timestamptz not null default now()
);

create index nodes_status_idx on nodes (status);

create table enrollment_tokens (
    token_hash bytea primary key,
    org_id     text        not null references orgs (id) on delete cascade,
    expires_at timestamptz not null,
    used_at    timestamptz,
    created_at timestamptz not null default now()
);

insert into orgs (id, name) values ('default', 'Default') on conflict (id) do nothing;
