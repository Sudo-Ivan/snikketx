-- Bot registry for mod_snikketx_bots: bot records, scoped bearer tokens,
-- validation and persistence. Tokens are stored only as SHA-256 hashes;
-- the cleartext is returned exactly once at mint time.
--luacheck: ignore 111 113/module 131 143/module

local jid = require "prosody.util.jid";
local hashes = require "prosody.util.hashes";
local encodings = require "prosody.util.encodings";
local random = require "prosody.util.random";
local ip = require "prosody.util.ip";
local usermanager = require "prosody.core.usermanager";

local b64 = encodings.base64.encode;

local function random_token_id()
	return (b64(random.bytes(9)):gsub("[+/=]", { ["+"] = "-", ["/"] = "_", ["="] = "" }));
end

local function random_secret()
	return (b64(random.bytes(24)):gsub("[+/=]", { ["+"] = "-", ["/"] = "_", ["="] = "" }));
end

local M = {};

-- Store handle is injected by the parent module so the file can be
-- exercised standalone if needed.
local store;
local config;

local bots = {}; -- name -> bot record (in-memory cache, store is source of truth)
local locked = false; -- operator lockdown, blocks new registrations
local token_index = {}; -- token_id -> bot name

local valid_scopes = {
	read = true;
	write = true;
	rooms = true;
	presence = true;
	manage = true; -- manage own config: webhook, subscription, label
};

local default_scopes = { "read", "write", "rooms" };

-- Names that collide with API paths must never become bots.
local reserved_names = { audit = true; access = true; lockdown = true };

local function valid_bot_name(name)
	return type(name) == "string"
		and name:match("^[a-z0-9][a-z0-9%._%-]*$") ~= nil
		and #name <= 48
		and not reserved_names[name];
end

local function short_hash(token)
	return hashes.sha256(token, true);
end

local function token_expired(tok)
	return tok.expires ~= nil and tok.expires <= os.time();
end

local function public_bot_record(bot)
	return {
		name = bot.name;
		jid = bot.jid;
		owner = bot.owner;
		label = bot.label;
		disabled = bot.disabled or false;
		created = bot.created;
		webhook_configured = bot.webhook ~= nil;
		webhook_url = bot.webhook and bot.webhook.url or nil;
		webhook_disabled = bot.webhook and bot.webhook.disabled or false;
		events = bot.events;
		rooms = bot.rooms;
		tokens = (function ()
			local out = {};
			for tid, tok in pairs(bot.tokens) do
				out[#out+1] = {
					id = tid;
					scopes = tok.scopes;
					name = tok.name;
					created = tok.created;
					expires = tok.expires;
					last_used = tok.last_used;
					bound_ips = tok.bind_ips;
					bound_ua = tok.bind_ua;
				};
			end
			return out;
		end)();
	};
end

local function persist(bot)
	if bot then
		store:set(bot.name, bot);
	end
end

function M.init(cfg, store_handle)
	config = cfg;
	store = store_handle;
	bots = {};
	token_index = {};
	for name in store:users() do
		local rec = store:get(name);
		if type(rec) == "table" and type(rec.name) == "string" then
			rec.tokens = rec.tokens or {};
			rec.rooms = rec.rooms or {};
			bots[name] = rec;
			for tid in pairs(rec.tokens) do
				token_index[tid] = name;
			end
		end
	end
	local meta = store:get("__meta");
	locked = type(meta) == "table" and meta.locked or false;
	return bots;
end

-- Operator lockdown: when set, no new bots can be registered. Existing
-- bots and their tokens keep working.
function M.set_lockdown(v)
	locked = v and true or false;
	store:set("__meta", { locked = locked });
	return locked;
end

function M.lockdown()
	return locked;
end

function M.get(name)
	return bots[name];
end

function M.all()
	return bots;
end

function M.count()
	local n = 0;
	for _ in pairs(bots) do n = n + 1; end
	return n;
end

function M.count_for_owner(owner)
	local n = 0;
	for _, bot in pairs(bots) do
		if bot.owner == owner then n = n + 1; end
	end
	return n;
end

function M.create(opts)
	local name = opts.name;
	if not valid_bot_name(name) then
		return nil, "invalid-name";
	end
	if locked then
		return nil, "bots-closed";
	end
	if bots[name] then
		return nil, "conflict";
	end
	local owner = jid.bare(opts.owner);
	if not owner then
		return nil, "invalid-owner";
	end
	local owner_node, owner_host = jid.split(owner);
	local local_host = config.owner_host;
	if owner_host ~= local_host then
		return nil, "owner-not-local";
	end
	if not usermanager.user_exists(owner_node, owner_host) then
		return nil, "owner-unknown";
	end
	if M.count_for_owner(owner) >= config.max_per_owner then
		return nil, "owner-quota";
	end
	if M.count() >= config.max_total then
		return nil, "global-quota";
	end

	local bot = {
		name = name;
		jid = name .. "@" .. module.host;
		owner = owner;
		label = opts.label;
		created = os.time();
		disabled = false;
		tokens = {};
		rooms = {};
		events = opts.events or {
			dm = true;
			groupchat = true;
			pm = true;
			presence = false;
			invites = true;
			mentions_only = false;
			echo_self = false;
			subscribe_requests = true;
		};
		webhook = nil;
		stats = { events = 0; events_dropped = 0; sends = 0; sends_denied = 0 };
	};
	bots[name] = bot;
	persist(bot);
	return bot;
end

function M.delete(name)
	local bot = bots[name];
	if not bot then return nil, "not-found"; end
	bots[name] = nil;
	for tid in pairs(bot.tokens or {}) do
		token_index[tid] = nil;
	end
	store:set(name, nil);
	return true;
end

-- Validate an optional bind spec: { ips = {"1.2.3.4", "10.0.0.0/8", ...},
-- ua = "substring" }. CIDRs are validated eagerly so bad config fails at
-- mint time, not at use time.
local function parse_bind(opts)
	if type(opts) ~= "table" then return nil; end
	local bind = {};
	if type(opts.ips) == "table" then
		if #opts.ips == 0 or #opts.ips > 16 then
			return nil, "bad-bind";
		end
		bind.ips = {};
		for _, cidr in ipairs(opts.ips) do
			if type(cidr) ~= "string" or #cidr > 64 then
				return nil, "bad-bind";
			end
			local addr, bits = ip.parse_cidr(cidr);
			if not addr then
				return nil, "bad-bind:" .. cidr;
			end
			bind.ips[#bind.ips + 1] = { addr = addr; bits = bits; raw = cidr };
		end
	end
	if type(opts.ua) == "string" then
		if #opts.ua > 128 then
			return nil, "bad-bind";
		end
		bind.ua = opts.ua:lower();
	end
	if not bind.ips and not bind.ua then return nil; end
	return bind;
end

function M.mint_token(bot, scopes, ttl_seconds, opts)
	if type(scopes) == "table" then
		for _, s in ipairs(scopes) do
			if not valid_scopes[s] then
				return nil, "invalid-scope:" .. tostring(s);
			end
		end
		if #scopes == 0 then scopes = nil; end
	end
	local bind, berr = parse_bind(opts);
	if opts and not bind and berr then
		return nil, berr;
	end
	local tid = random_token_id();
	local secret = random_secret();
	local token = "sxb_" .. tid .. "_" .. secret;
	local ntok = 0;
	for _ in pairs(bot.tokens) do ntok = ntok + 1; end
	if ntok >= (config.max_tokens_per_bot or 10) then
		return nil, "token-quota";
	end
	-- Always copy: two tokens must not share the scopes table or the
	-- store serializer rejects the record for multiple references.
	local scope_list = {};
	for _, s in ipairs(scopes or default_scopes) do scope_list[#scope_list + 1] = s; end
	local tok = {
		hash = short_hash(token);
		scopes = scope_list;
		created = os.time();
		expires = ttl_seconds and (os.time() + ttl_seconds) or nil;
		name = type(opts) == "table" and type(opts.name) == "string" and opts.name:sub(1, 64) or nil;
	};
	if bind then
		-- Persist plain values; the parsed objects are rebuilt on verify.
		tok.bind_ips = {};
		if bind.ips then
			for _, c in ipairs(bind.ips) do tok.bind_ips[#tok.bind_ips + 1] = c.raw; end
		end
		tok.bind_ua = bind.ua;
	end
	bot.tokens[tid] = tok;
	token_index[tid] = bot.name;
	persist(bot);
	return token, tid;
end

-- Check a stored token's bind constraints against the request context
-- (ctx = { ip = "1.2.3.4", ua = "User-Agent" }). Returns true or
-- nil, reason for audit logging.
local function bind_check(tok, ctx)
	if tok.bind_ips then
		if not ctx or not ctx.ip then return nil, "no-client-ip"; end
		local client = ip.new_ip(ctx.ip);
		if not client then return nil, "bad-client-ip"; end
		local matched = false;
		for _, raw in ipairs(tok.bind_ips) do
			local addr, bits = ip.parse_cidr(raw);
			if addr and ip.match(client, addr, bits) then
				matched = true;
				break;
			end
		end
		if not matched then return nil, "ip-not-bound"; end
	end
	if tok.bind_ua then
		local ua = ctx and ctx.ua;
		if not ua or not ua:lower():find(tok.bind_ua, 1, true) then
			return nil, "ua-not-bound";
		end
	end
	return true;
end

-- Returns bot, token_meta or nil, deny_reason. The token id is exactly
-- 12 base64url chars; secrets may themselves contain underscores, so the
-- split is positional, not pattern-greedy.
function M.verify_token(token, ctx)
	if type(token) ~= "string" then return nil; end
	local tid = token:match("^sxb_(............)_.+$");
	if not tid then return nil; end
	local name = token_index[tid];
	if not name then return nil; end
	local bot = bots[name];
	if not bot then return nil; end
	local tok = bot.tokens[tid];
	if not tok then return nil; end
	if not hashes.equals(short_hash(token), tok.hash) then
		return nil, nil, "bad-token";
	end
	if token_expired(tok) then
		return nil, nil, "expired";
	end
	local ok, reason = bind_check(tok, ctx);
	if not ok then
		return nil, nil, reason;
	end
	tok.last_used = os.time();
	if ctx then
		tok.last_ip = ctx.ip;
		tok.last_ua = ctx.ua;
	end
	return bot, tok;
end

function M.bot_name_for_token_id(tid)
	return token_index[tid];
end

function M.revoke_token(bot, tid)
	if not bot.tokens[tid] then return nil, "not-found"; end
	bot.tokens[tid] = nil;
	token_index[tid] = nil;
	persist(bot);
	return true;
end

function M.has_scope(tok, scope)
	if not tok or not tok.scopes then return false; end
	for _, s in ipairs(tok.scopes) do
		if s == scope or s == "manage" then return true; end
	end
	return false;
end

function M.save(bot)
	persist(bot);
end

function M.public(bot)
	return public_bot_record(bot);
end

return M;
