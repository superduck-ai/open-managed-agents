-- +goose Up

-- A Dream is the durable request record for memory consolidation.
-- Public status stays pending/running/terminal; worker scheduling uses
-- execution_state. Selected source sessions are stored as relations, not
-- copied JSONL.
create table dreams (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    organization_uuid uuid not null,
    workspace_uuid uuid not null,
    created_by_api_key_uuid uuid,
    runtime_user_uuid uuid,
    status text not null default 'pending',
    model text not null,
    instructions text,
    inputs jsonb not null,
    outputs jsonb not null default '[]'::jsonb,
    error jsonb,
    usage jsonb not null default '{}'::jsonb,
    output_memory_store_uuid uuid,
    internal_session_uuid uuid,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    started_at timestamptz,
    ended_at timestamptz,
    archived_at timestamptz,
    execution_state text not null default 'queued',
    attempt_count integer not null default 0,
    next_attempt_at timestamptz,
    claimed_by_worker_id text,
    claim_expires_at timestamptz,
    last_error text,
    constraint dreams_id_pk primary key (id),
    constraint dreams_uuid_key unique (uuid),
    constraint dreams_external_id_key unique (external_id),
    constraint dreams_external_id_format_check
        check (external_id ~ '^drm_[0-9A-Za-z]{24}$'),
    constraint dreams_status_check
        check (status in ('pending', 'running', 'completed', 'failed', 'canceled')),
    constraint dreams_instructions_length_check
        check (instructions is null or char_length(instructions) <= 4096),
    constraint dreams_inputs_array_check check (jsonb_typeof(inputs) = 'array'),
    constraint dreams_outputs_array_check check (jsonb_typeof(outputs) = 'array'),
    constraint dreams_execution_state_check
        check (execution_state in ('queued', 'preparing', 'dispatching', 'running', 'terminal')),
    constraint dreams_status_execution_state_check
        check (
            (status = 'pending' and execution_state in ('queued', 'preparing', 'dispatching'))
            or (status = 'running' and execution_state = 'running')
            or (status in ('completed', 'failed', 'canceled') and execution_state = 'terminal')
        ),
    constraint dreams_attempt_count_check check (attempt_count >= 0)
);

comment on table dreams is
    '记忆整理（Dream）请求记录，对应公开 Dream 资源。每次 POST /v1/dreams 一行；实际整理在内部 Session 中执行，结果写入新建的输出 Memory Store。';

comment on column dreams.id is '数据库内部自增主键，不对外暴露，也不被其他表引用。';
comment on column dreams.uuid is '稳定业务标识，用于跨表引用和行锁定。';
comment on column dreams.external_id is '兼容 Anthropic 的公开 ID，格式 drm_ + 24 位字母数字。';
comment on column dreams.organization_uuid is '所属组织（organizations.uuid），租户边界。';
comment on column dreams.workspace_uuid is '所属工作区（workspaces.uuid），所有读写都以它为范围。';
comment on column dreams.created_by_api_key_uuid is '创建该 Dream 的 API Key（api_keys.uuid）；通过控制台会话登录创建时为空。';
comment on column dreams.runtime_user_uuid is '内部 Session 以哪个用户身份运行；创建时解析并落库，worker 执行时无需请求上下文。';
comment on column dreams.status is '公开生命周期状态：pending -> running -> completed | failed | canceled，与 API 合同一致。';
comment on column dreams.model is '本次整理使用的模型 ID；API 响应中以 {"id": model} 形式返回。';
comment on column dreams.instructions is '调用方可选的整理焦点，追加在发给 skill 的 /dream 命令之后；最长 4096 字符。';
comment on column dreams.inputs is '规范化后的输入数组：[{type:"memory_store", memory_store_id}, {type:"sessions", session_ids:[...]}]。';
comment on column dreams.outputs is '输出数组；资源准备完成后 [0] 携带新建的 memory_store_id。仅内部使用的 key 在 API 响应前会被剥离。';
comment on column dreams.error is 'status 为 failed 时的终态错误对象 {type, message}；其他情况为空。';
comment on column dreams.usage is 'Dream 进入终态时从内部 Session 汇总的 token 用量。';
comment on column dreams.output_memory_store_uuid is '新建的输出 Memory Store（memory_stores.uuid），每个 Dream 一个；输入 Store 不会被修改。';
comment on column dreams.internal_session_uuid is '执行整理的内部 Session（sessions.uuid）；pending 阶段派发前为空。归档 Dream 时该 Session 只归档不删除。';
comment on column dreams.created_at is '请求创建时间，同时是 pending 队列的先进先出排序键。';
comment on column dreams.updated_at is '本行最后一次修改时间。';
comment on column dreams.started_at is '状态变为 running 的时间。';
comment on column dreams.ended_at is '状态进入终态的时间。';
comment on column dreams.archived_at is '软归档标记；已归档的 Dream 不出现在列表查询中，但仍可按 ID 获取。';
comment on column dreams.execution_state is 'worker 调度子状态：pending 下为 queued | preparing | dispatching，running 下为 running，completed/failed/canceled 下为 terminal。';
comment on column dreams.attempt_count is 'pending worker 认领本行的次数，用于重试退避。';
comment on column dreams.next_attempt_at is '瞬时失败后 pending worker 最早可再次认领的时间；为空表示可立即认领。';
comment on column dreams.claimed_by_worker_id is '当前持有 pending 认领的 worker 标识；释放后为空。';
comment on column dreams.claim_expires_at is '当前认领的租约到期时间；过期后任意 worker 可重新认领。';
comment on column dreams.last_error is '最近一次 worker 侧失败信息，用于诊断；与公开的 error 字段独立。';

-- Public list/pagination: scoped by workspace, ordered by creation time,
-- no status filter. Status is deliberately kept out so pagination stays
-- index-ordered.
create index dreams_workspace_created_v2_idx
    on dreams (workspace_uuid, created_at desc, uuid desc)
    where archived_at is null;

-- Global worker scans (ListByStatus, stopped-with-active-session, awaiting
-- archive) filter by status across all workspaces and order by created_at.
create index dreams_status_created_v1_idx
    on dreams (status, created_at, uuid);

-- ClaimNextPending orders by (created_at, uuid) and applies OR-based
-- next_attempt_at / claim_expires_at filters, so the partial index leads
-- with the ORDER BY columns rather than next_attempt_at.
create index dreams_pending_claim_v1_idx
    on dreams (created_at, uuid)
    where status = 'pending' and archived_at is null;

create unique index dreams_output_memory_store_uuid_key
    on dreams (output_memory_store_uuid)
    where output_memory_store_uuid is not null;

create unique index dreams_internal_session_uuid_key
    on dreams (internal_session_uuid)
    where internal_session_uuid is not null;

create table dream_session_transcripts (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    dream_uuid uuid not null,
    workspace_uuid uuid not null,
    source_session_uuid uuid not null,
    source_session_external_id text not null,
    ordinal integer not null,
    created_at timestamptz not null default now(),
    constraint dream_session_transcripts_id_pk primary key (id),
    constraint dream_session_transcripts_uuid_key unique (uuid),
    constraint dream_session_transcripts_ordinal_check check (ordinal >= 0),
    constraint dream_session_transcripts_dream_ordinal_key unique (dream_uuid, ordinal),
    constraint dream_session_transcripts_dream_source_session_key unique (dream_uuid, source_session_uuid)
);

comment on table dream_session_transcripts is
    'Dream 与其整理的源 Session 之间的关联表。transcript 按需从 session_events 渲染，这里不复制任何 JSONL。';

comment on column dream_session_transcripts.id is '数据库内部自增主键。';
comment on column dream_session_transcripts.uuid is '稳定业务标识。';
comment on column dream_session_transcripts.dream_uuid is '所属 Dream（dreams.uuid）。';
comment on column dream_session_transcripts.workspace_uuid is 'Dream 与源 Session 所属的工作区；冗余存储以便按租户范围查询。';
comment on column dream_session_transcripts.source_session_uuid is '为虚拟 transcript 提供事件的源 Session（sessions.uuid）。';
comment on column dream_session_transcripts.source_session_external_id is '源 Session 的公开 sess_ ID，用于沙箱中 transcript 文件的命名。';
comment on column dream_session_transcripts.ordinal is '在提交的 session_ids 数组中的位置（从 0 开始），固定 transcript 顺序。';
comment on column dream_session_transcripts.created_at is '关联记录写入时间（即 Dream 创建时）。';

create index dream_session_transcripts_workspace_dream_ordinal_v1_idx
    on dream_session_transcripts (workspace_uuid, dream_uuid, ordinal);

-- +goose Down

drop table if exists dream_session_transcripts;
drop table if exists dreams;
