-- Durable two-phase approvals for AI write actions.

create table ai_approvals (
    id              text primary key,
    org_id          text        not null,
    user_id         text        not null,
    conversation_id text        not null references conversations (id) on delete cascade,
    status          text        not null default 'pending' check (status in ('pending', 'approved', 'rejected', 'executed', 'failed')),
    plan            jsonb       not null,
    results         jsonb,
    created_at      timestamptz not null default now(),
    decided_at      timestamptz,
    executed_at     timestamptz
);
create index ai_approvals_pending_idx on ai_approvals (conversation_id, status);
