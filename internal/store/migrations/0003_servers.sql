-- Servers: desired state owned by the panel, observed state reported by the node.

create table servers (
    id                text primary key,
    org_id            text        not null references orgs (id) on delete cascade,
    node_id           text        not null references nodes (id) on delete restrict,
    name              text        not null,
    desired_state     text        not null default 'stopped'
                      check (desired_state in ('running', 'stopped')),
    desired_revision  bigint      not null default 1,
    observed_state    text        not null default 'unknown',
    applied_revision  bigint      not null default 0,
    kill_requested    boolean     not null default false,
    accept_eula       boolean     not null default false,
    image             text        not null,
    java_version      int         not null,
    memory_mb         int         not null,
    cpu_cores         real        not null,
    port              int         not null,
    jar_url           text        not null,
    jar_sha256        text        not null,
    last_error        text        not null default '',
    created_at        timestamptz not null default now(),
    unique (node_id, port),
    unique (node_id, name)
);

create index servers_node_idx on servers (node_id);
