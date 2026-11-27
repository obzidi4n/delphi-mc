alter table servers add column if not exists restart_policy text not null default 'on-failure';

create table if not exists schedules (
    id           text primary key,
    org_id       text not null,
    name         text not null,
    cron         text not null,
    timezone     text not null default 'UTC',
    target_kind  text not null default 'server',
    target_id    text not null,
    action       text not null,
    enabled      boolean not null default true,
    next_run     timestamptz,
    last_run     timestamptz,
    created_at   timestamptz not null default now(),
    unique (org_id, name)
);

create table if not exists alert_rules (
    id             text primary key,
    org_id         text not null,
    kind           text not null,          -- crash_loop | server_down
    threshold      int not null default 3,
    window_seconds int not null default 600,
    enabled        boolean not null default true,
    created_at     timestamptz not null default now()
);

create table if not exists alert_channels (
    id         text primary key,
    org_id     text not null,
    name       text not null,
    kind       text not null,              -- webhook | smtp
    config_enc bytea not null,
    created_at timestamptz not null default now()
);

create table if not exists alerts (
    id          text primary key,
    org_id      text not null,
    rule_id     text not null,
    server_id   text not null,
    state       text not null,             -- firing | resolved
    summary     text not null default '',
    started_at  timestamptz not null default now(),
    resolved_at timestamptz
);

create index if not exists alerts_firing_idx on alerts (server_id, rule_id, state);

create table if not exists maintenance_windows (
    id              text primary key,
    org_id          text not null,
    starts_at       timestamptz not null,
    ends_at         timestamptz not null,
    suppress_alerts boolean not null default true,
    block_updates   boolean not null default false,
    created_at      timestamptz not null default now()
);
