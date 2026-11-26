-- Baseline schema (M0). Forward-only: see docs/decisions/0019-forward-only-migrations.md.
-- Org-owned rows carry org_id; children derive it through foreign keys.

create table orgs (
    id         text primary key,
    name       text        not null,
    created_at timestamptz not null default now()
);

create table users (
    id            text primary key,
    org_id        text        not null references orgs (id) on delete cascade,
    email         text        not null,
    password_hash text        not null,
    totp_secret   text,
    created_at    timestamptz not null default now(),
    unique (org_id, email)
);
