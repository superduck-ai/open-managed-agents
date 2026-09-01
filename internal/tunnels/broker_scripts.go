package tunnels

import "github.com/redis/go-redis/v9"

const compactTerminalStateLua = `
local function compact_terminal_state(state)
    state.created_at = nil
    state.headers = nil
    state.jsonrpc = nil
    state.expires_at = nil
    state.expires_at_unix_ms = nil
    state.payload_size = nil
    state.affinity_key = nil
end
`

var registerConnectorScript = redis.NewScript(`
local instance_id = ARGV[1]
local expires_at = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
local channel_count = tonumber(ARGV[4])
local now = expires_at - ttl
local new_count = 0
local presence_prefix = ARGV[6 + channel_count * 2]

if tonumber(redis.call('GET', KEYS[3]) or '0') ~= tonumber(ARGV[5 + channel_count * 2]) then
    return -3
end

local known_channels = redis.call('SMEMBERS', KEYS[1])
for _, name in ipairs(known_channels) do
    local presence_key = presence_prefix .. name
    redis.call('ZREMRANGEBYSCORE', presence_key, '-inf', now)
    if redis.call('ZCARD', presence_key) == 0 then
        redis.call('SREM', KEYS[1], name)
        redis.call('HDEL', KEYS[2], name)
    end
end

for i = 1, channel_count do
    local name = ARGV[3 + i * 2]
    local affinity = ARGV[4 + i * 2]
    if redis.call('SISMEMBER', KEYS[1], name) == 0 then
        new_count = new_count + 1
    end
    local existing = redis.call('HGET', KEYS[2], name)
    if existing and existing ~= affinity then
        return -2
    end
end

if redis.call('SCARD', KEYS[1]) + new_count > 32 then
    return -1
end

for i = 1, channel_count do
    local name = ARGV[3 + i * 2]
    local affinity = ARGV[4 + i * 2]
    redis.call('SADD', KEYS[1], name)
    redis.call('HSETNX', KEYS[2], name, affinity)
    redis.call('ZREMRANGEBYSCORE', KEYS[3 + i], '-inf', now)
    redis.call('ZADD', KEYS[3 + i], expires_at, instance_id)
    redis.call('PEXPIRE', KEYS[3 + i], ttl * 2)
end
return 1
`)

var enqueueScript = redis.NewScript(`
local request_id = ARGV[1]
local state_json = ARGV[2]
local now = tonumber(ARGV[3])
local expires_at = tonumber(ARGV[4])
local payload_size = tonumber(ARGV[5])
local max_count = tonumber(ARGV[6])
local max_bytes = tonumber(ARGV[7])

redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now)
if redis.call('ZCARD', KEYS[3]) == 0 then
    return -1
end
if redis.call('HEXISTS', KEYS[1], request_id) == 1 then
    return 1
end
local pending_count = tonumber(redis.call('HGET', KEYS[4], 'pending_count') or '0')
local pending_bytes = tonumber(redis.call('HGET', KEYS[4], 'pending_bytes') or '0')
if pending_count + 1 > max_count then
    return -2
end
if pending_bytes + payload_size > max_bytes then
    return -3
end

redis.call('HSET', KEYS[1], request_id, state_json)
redis.call('ZADD', KEYS[2], now, request_id)
redis.call('ZADD', KEYS[5], expires_at, request_id)
redis.call('HINCRBY', KEYS[4], 'pending_count', 1)
redis.call('HINCRBY', KEYS[4], 'pending_bytes', payload_size)
redis.call('PUBLISH', ARGV[8], request_id)
return 1
`)

var ensureActiveTokenVersionScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
local requested = tonumber(ARGV[1])
if not current then
    redis.call('SET', KEYS[1], requested)
    return 1
end
current = tonumber(current)
if current == requested then
    return 1
end
local current_version = math.abs(current)
if requested > current_version then
    redis.call('SET', KEYS[1], requested)
    return 1
end
return -1
`)

var suspendActiveTokenVersionScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
local requested = tonumber(ARGV[1])
if current and tonumber(current) ~= requested and tonumber(current) ~= -requested then
    return -1
end
redis.call('SET', KEYS[1], -requested)
local channels = redis.call('SMEMBERS', KEYS[2])
for _, channel in ipairs(channels) do
    redis.call('DEL', ARGV[3] .. 'presence:' .. channel)
end
redis.call('PUBLISH', ARGV[2], 'token_version_changed')
return 1
`)

var activateTokenVersionScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local requested = tonumber(ARGV[1])
if math.abs(current) > requested then
    return -1
end
redis.call('SET', KEYS[1], requested)
redis.call('PUBLISH', ARGV[2], 'token_version_changed')
return 1
`)

var claimScript = redis.NewScript(compactTerminalStateLua + `
local instance_id = ARGV[2]
local token_version = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
local now = tonumber(ARGV[5])
local tombstone_ttl = tonumber(ARGV[6])
local affinity_ttl = tonumber(ARGV[7])
local channel_count = tonumber(ARGV[8])
local max_scan = math.max(1, tonumber(ARGV[9]))
local claimed = {}
local claimed_count = 0
local made_progress = true

if tonumber(redis.call('GET', KEYS[6]) or '0') ~= token_version then
    return {'__token_retired__'}
end

local expired_owners = redis.call('ZRANGEBYSCORE', KEYS[5], '-inf', now, 'LIMIT', 0, 512)
for _, owner_key in ipairs(expired_owners) do
    redis.call('HDEL', KEYS[4], owner_key)
    redis.call('ZREM', KEYS[5], owner_key)
end

local function decrement_budget(state)
    local count = tonumber(redis.call('HGET', KEYS[3], 'pending_count') or '0')
    local bytes = tonumber(redis.call('HGET', KEYS[3], 'pending_bytes') or '0')
    redis.call('HSET', KEYS[3], 'pending_count', math.max(0, count - 1))
    redis.call('HSET', KEYS[3], 'pending_bytes', math.max(0, bytes - tonumber(state.payload_size or 0)))
end

local channel_ids = {}
local channel_positions = {}
for channel_index = 1, channel_count do
    local key_index = 6 + channel_index
    channel_ids[channel_index] = redis.call('ZRANGE', KEYS[key_index], 0, max_scan - 1)
    channel_positions[channel_index] = 1
end

while claimed_count < limit and made_progress do
    made_progress = false
    for channel_index = 1, channel_count do
        if claimed_count >= limit then
            break
        end
        local key_index = 6 + channel_index
        local ids = channel_ids[channel_index]
        local process_affinity = ARGV[9 + channel_index] == 'true'
        while channel_positions[channel_index] <= #ids do
            if claimed_count >= limit then
                break
            end
            local request_id = ids[channel_positions[channel_index]]
            channel_positions[channel_index] = channel_positions[channel_index] + 1
            local raw = redis.call('HGET', KEYS[1], request_id)
            if not raw then
                redis.call('ZREM', KEYS[key_index], request_id)
                redis.call('ZREM', KEYS[2], request_id)
                made_progress = true
            else
                local state = cjson.decode(raw)
                if state.state ~= 'queued' then
                    redis.call('ZREM', KEYS[key_index], request_id)
                    made_progress = true
                elseif tonumber(state.expires_at_unix_ms or 0) > 0 and tonumber(state.expires_at_unix_ms) <= now then
                    local payload_size = tonumber(state.payload_size or 0)
                    state.state = 'expired'
                    compact_terminal_state(state)
                    redis.call('HSET', KEYS[1], request_id, cjson.encode(state))
                    redis.call('ZREM', KEYS[key_index], request_id)
                    redis.call('ZADD', KEYS[2], now + tombstone_ttl, request_id)
                    decrement_budget({payload_size=payload_size})
                    made_progress = true
                else
                    local affinity_allowed = true
                    if process_affinity and state.affinity_key and state.affinity_key ~= '' then
                        local owner_key = state.channel .. ':' .. state.affinity_key
                        local owner_raw = redis.call('HGET', KEYS[4], owner_key)
                        if owner_raw then
                            local owner = cjson.decode(owner_raw)
                            if tonumber(owner.expires_at or 0) <= now then
                                redis.call('HDEL', KEYS[4], owner_key)
                                redis.call('ZREM', KEYS[5], owner_key)
                                owner_raw = nil
                            elseif owner.instance_id ~= instance_id then
                                affinity_allowed = false
                            end
                        end
                        if affinity_allowed then
                            redis.call('HSET', KEYS[4], owner_key, cjson.encode({instance_id=instance_id, expires_at=now + affinity_ttl}))
                            redis.call('ZADD', KEYS[5], now + affinity_ttl, owner_key)
                        end
                    end
                    if affinity_allowed then
                        claimed_count = claimed_count + 1
                        state.state = 'dispatched'
                        state.instance_id = instance_id
                        state.token_version = token_version
                        state.shard_token = ARGV[9 + channel_count + claimed_count]
                        local updated = cjson.encode(state)
                        redis.call('HSET', KEYS[1], request_id, updated)
                        redis.call('ZREM', KEYS[key_index], request_id)
                        table.insert(claimed, updated)
                        made_progress = true
                        break
                    end
                end
            end
        end
    end
end
return claimed
`)

var submitResponseScript = redis.NewScript(compactTerminalStateLua + `
local request_id = ARGV[1]
local raw = redis.call('HGET', KEYS[1], request_id)
if not raw then
    return -1
end
local state = cjson.decode(raw)
local response = cjson.decode(ARGV[5])
if state.instance_id ~= ARGV[2] or tonumber(state.token_version or 0) ~= tonumber(ARGV[3]) or state.shard_token ~= ARGV[4] then
    return -2
end
if response.channel and response.channel ~= '' and response.channel ~= state.channel then
    return -2
end
if state.command_type == 'jsonrpc' then
    if response.resp_type ~= 'jsonrpc_response' and response.resp_type ~= 'jsonrpc_notify' and response.resp_type ~= 'notify_ack' then
        return -2
    end
elseif state.command_type == 'oauth_discovery' then
    if response.resp_type ~= 'oauth_discovery_response' then
        return -2
    end
elseif state.command_type == 'session_termination' then
    if response.resp_type ~= 'session_termination_response' then
        return -2
    end
else
    return -2
end
if state.state == 'completed' then
    return 2
end
if state.state == 'canceled' then
    return -3
end
if state.state == 'expired' or tonumber(state.expires_at_unix_ms or 0) <= tonumber(ARGV[7]) then
    return -4
end
if state.state ~= 'dispatched' then
    return -1
end

local affinity_enabled = redis.call('HGET', KEYS[6], state.channel) == 'true'
local response_session_id = ARGV[11]
if state.command_type == 'session_termination' then
    response_session_id = state.affinity_key or ''
end
if affinity_enabled and response_session_id and response_session_id ~= '' then
    local owner_key = state.channel .. ':' .. response_session_id
    if state.command_type == 'session_termination' then
        redis.call('HDEL', KEYS[4], owner_key)
        redis.call('ZREM', KEYS[5], owner_key)
    else
        redis.call('HSET', KEYS[4], owner_key, cjson.encode({instance_id=state.instance_id, expires_at=tonumber(ARGV[7]) + tonumber(ARGV[10])}))
        redis.call('ZADD', KEYS[5], tonumber(ARGV[7]) + tonumber(ARGV[10]), owner_key)
    end
end

if tonumber(ARGV[6]) == 0 then
    redis.call('PUBLISH', ARGV[9], ARGV[5])
    return 1
end

state.state = 'completed'
state.response = response
local payload_size = tonumber(state.payload_size or 0)
local count = tonumber(redis.call('HGET', KEYS[2], 'pending_count') or '0')
local bytes = tonumber(redis.call('HGET', KEYS[2], 'pending_bytes') or '0')
redis.call('HSET', KEYS[2], 'pending_count', math.max(0, count - 1))
redis.call('HSET', KEYS[2], 'pending_bytes', math.max(0, bytes - payload_size))
compact_terminal_state(state)
redis.call('HSET', KEYS[1], request_id, cjson.encode(state))
redis.call('ZADD', KEYS[3], tonumber(ARGV[7]) + tonumber(ARGV[8]), request_id)
redis.call('PUBLISH', ARGV[9], ARGV[5])
return 1
`)

var cancelRequestScript = redis.NewScript(compactTerminalStateLua + `
local request_id = ARGV[1]
local raw = redis.call('HGET', KEYS[1], request_id)
if not raw then
    return -1
end
local state = cjson.decode(raw)
if state.state == 'completed' or state.state == 'canceled' or state.state == 'expired' then
    return 1
end
if state.state == 'queued' then
    redis.call('ZREM', ARGV[2] .. 'queue:' .. state.channel, request_id)
end
local payload_size = tonumber(state.payload_size or 0)
state.state = 'canceled'
local count = tonumber(redis.call('HGET', KEYS[2], 'pending_count') or '0')
local bytes = tonumber(redis.call('HGET', KEYS[2], 'pending_bytes') or '0')
redis.call('HSET', KEYS[2], 'pending_count', math.max(0, count - 1))
redis.call('HSET', KEYS[2], 'pending_bytes', math.max(0, bytes - payload_size))
compact_terminal_state(state)
redis.call('HSET', KEYS[1], request_id, cjson.encode(state))
redis.call('ZADD', KEYS[3], tonumber(ARGV[3]) + tonumber(ARGV[4]), request_id)
redis.call('PUBLISH', ARGV[5], '{"canceled":true}')
return 1
`)

var cleanupRequestsScript = redis.NewScript(compactTerminalStateLua + `
local now = tonumber(ARGV[1])
local tombstone_ttl = tonumber(ARGV[2])
local batch_size = tonumber(ARGV[4])
local expired_owners = redis.call('ZRANGEBYSCORE', KEYS[5], '-inf', now, 'LIMIT', 0, batch_size)
for _, owner_key in ipairs(expired_owners) do
    redis.call('HDEL', KEYS[4], owner_key)
    redis.call('ZREM', KEYS[5], owner_key)
end
local ids = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now, 'LIMIT', 0, batch_size)
for _, request_id in ipairs(ids) do
    local raw = redis.call('HGET', KEYS[1], request_id)
    if not raw then
        redis.call('ZREM', KEYS[2], request_id)
    else
        local state = cjson.decode(raw)
        if state.state == 'queued' or state.state == 'dispatched' then
            if state.state == 'queued' then
                redis.call('ZREM', ARGV[3] .. 'queue:' .. state.channel, request_id)
            end
            local payload_size = tonumber(state.payload_size or 0)
            state.state = 'expired'
            local count = tonumber(redis.call('HGET', KEYS[3], 'pending_count') or '0')
            local bytes = tonumber(redis.call('HGET', KEYS[3], 'pending_bytes') or '0')
            redis.call('HSET', KEYS[3], 'pending_count', math.max(0, count - 1))
            redis.call('HSET', KEYS[3], 'pending_bytes', math.max(0, bytes - payload_size))
            compact_terminal_state(state)
            redis.call('HSET', KEYS[1], request_id, cjson.encode(state))
            redis.call('ZADD', KEYS[2], now + tombstone_ttl, request_id)
            redis.call('PUBLISH', ARGV[3] .. 'response:' .. request_id, '{"expired":true}')
        else
            redis.call('HDEL', KEYS[1], request_id)
            redis.call('ZREM', KEYS[2], request_id)
        end
    end
end
return math.max(#ids, #expired_owners)
`)

var acknowledgeResponseScript = redis.NewScript(`
local raw = redis.call('HGET', KEYS[1], ARGV[1])
if not raw then
    return 0
end
local state = cjson.decode(raw)
if state.state ~= 'completed' or not state.response then
    return 0
end
state.response = nil
redis.call('HSET', KEYS[1], ARGV[1], cjson.encode(state))
return 1
`)
