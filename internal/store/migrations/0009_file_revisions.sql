-- Versioned text-file edits and AI/UI rollbacks.

create table file_revisions (
    id         text primary key,
    org_id     text        not null,
    server_id  text        not null references servers (id) on delete cascade,
    path       text        not null,
    revision   int         not null,
    content    text        not null,
    sha256     text        not null,
    author     text        not null,
    created_at timestamptz not null default now(),
    unique (server_id, path, revision)
);
create index file_revisions_lookup on file_revisions (server_id, path, revision desc);
create index file_revisions_path on file_revisions (server_id, path, created_at desc);
