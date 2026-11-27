create table if not exists backups (
    id          text primary key,
    org_id      text not null,
    server_id   text not null references servers(id) on delete cascade,
    node_id     text not null,
    state       text not null,          -- creating | ready | failed
    size_bytes  bigint not null default 0,
    sha256      text not null default '',
    format      text not null default 'tar.gz',
    encrypted   boolean not null default true,
    error       text not null default '',
    created_at  timestamptz not null default now(),
    ready_at    timestamptz
);

create index if not exists backups_server_created_idx on backups (server_id, created_at desc);
