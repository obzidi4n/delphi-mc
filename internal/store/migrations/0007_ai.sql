-- Conversations, messages, and the documentation corpus for FTS retrieval.

create table conversations (
    id         text primary key,
    org_id     text        not null references orgs (id) on delete cascade,
    user_id    text        not null references users (id) on delete cascade,
    title      text        not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create index conversations_user_idx on conversations (org_id, user_id, updated_at desc);

create table messages (
    id              text primary key,
    conversation_id text        not null references conversations (id) on delete cascade,
    role            text        not null check (role in ('user', 'assistant', 'tool', 'system')),
    content         text        not null default '',
    tool_calls      jsonb,
    tool_call_id    text,
    created_at      timestamptz not null default now()
);
create index messages_conversation_idx on messages (conversation_id, created_at);

create table docs_chunks (
    path       text not null,
    ordinal    int  not null,
    heading    text not null default '',
    content    text not null,
    tsv        tsvector generated always as (to_tsvector('english', coalesce(heading, '') || ' ' || content)) stored,
    primary key (path, ordinal)
);
create index docs_chunks_tsv_idx on docs_chunks using gin (tsv);

create table docs_meta (
    id   int primary key default 1 check (id = 1),
    hash text not null
);
