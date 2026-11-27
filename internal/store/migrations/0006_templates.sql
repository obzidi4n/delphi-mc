-- Server templates (built-in rows have a null org) and the template facts a
-- server carries so its node can render the same files without the panel.

create table templates (
    id         text primary key,
    org_id     text references orgs (id) on delete cascade,
    name       text        not null,
    revision   int         not null default 1,
    body       jsonb       not null,
    builtin    boolean     not null default false,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create unique index templates_org_name_idx on templates (org_id, name) where org_id is not null;

alter table servers add column base text not null default 'paper';
alter table servers add column template_id text not null default '';
alter table servers add column template_version int not null default 0;
alter table servers add column variables jsonb not null default '{}'::jsonb;
alter table servers add column files jsonb not null default '{}'::jsonb;
