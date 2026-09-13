-- HTTP/JSON API for mod_snikketx_bots. Loaded via module:require and
-- called with the dependency table; registers the http provider.
--
-- Auth model:
--   - management endpoints: Bearer token resolving to a tokenauth
--     session for the bot owner (or an admin), or a static token from
--     the bot_admin_tokens config (admin).
--   - runtime endpoints: Bearer sxb_ bot tokens with per-scope checks.
--   - SSE/WS endpoints: same bearer token, or a single-use ?ticket=
--     minted by POST /{name}/ticket for browser EventSource/WebSocket
--     clients that cannot set headers.
--luacheck: ignore 111 113/module 131 143/module

local json = require "prosody.util.json";
local jid = require "prosody.util.jid";
local hashes = require "prosody.util.hashes";
local st = require "prosody.util.stanza";
local usermanager = require "prosody.core.usermanager";

module:depends("http");

return function (deps)

local config = deps.config;
local registry = deps.registry;
local events = deps.events;
local webhook = deps.webhook;
local ws = deps.ws;
local dispatch = deps.dispatch;
local api_limit = deps.api_limit;
local ticket_limit = deps.ticket_limit;
local authfail_limit = deps.authfail_limit;
local audit = deps.audit;

local function audit_actor(principal)
	if not principal then return nil; end
	if principal.kind == "admin" then return principal.user or "admin-token"; end
	if principal.kind == "bot" then return "bot:" .. principal.bot.name; end
	return principal.user;
end

-- tokenauth is a community module in the Snikket image; absent on a
-- plain Prosody, where only bot_admin_tokens and bot tokens then work.
local tokens_mod;
pcall(function ()
	tokens_mod = module:depends("tokenauth");
end);

local sse_conns = {}; -- conn -> unsubscribe fn, used for keepalives

-- Helpers ------------------------------------------------------------------

local function json_response(event, payload, code)
	event.response.headers.content_type = "application/json";
	if code then event.response.status_code = code; end
	return json.encode(payload);
end

local function json_error(event, code, err)
	event.response.headers.content_type = "application/json";
	event.response.status_code = code;
	return json.encode({ error = err });
end

local function parse_body(event)
	local request = event.request;
	local len = tonumber(request.headers.content_length or "") or (request.body and #request.body) or 0;
	if len > config.max_body then
		return nil, 413, "body-too-large";
	end
	if not request.body or request.body == "" then
		return {}, nil;
	end
	local payload, err = json.decode(request.body);
	if type(payload) ~= "table" then
		return nil, 400, "bad-json:" .. tostring(err);
	end
	return payload;
end

local function decode_query(query)
	local out = {};
	if not query then return out; end
	for key, value in tostring(query):gmatch("([^&=]+)=([^&=]*)") do
		key = key:gsub("%%(%x%x)", function (h) return string.char(tonumber(h, 16)); end);
		value = value:gsub("%%(%x%x)", function (h) return string.char(tonumber(h, 16)); end);
		out[key] = value;
	end
	return out;
end

-- Auth ---------------------------------------------------------------------

local function session_is_admin(session)
	if not session then return false; end
	local username = session.username or (session.token_info and session.token_info.username);
	local host = session.host or (session.token_info and session.token_info.host) or config.owner_host;
	if not username then return false; end
	if usermanager.user_is_admin then
		local ok, res = pcall(usermanager.user_is_admin, username, host);
		if ok then return res; end
	end
	return usermanager.is_admin(username .. "@" .. host);
end

-- Returns one of:
--   {kind="admin"}
--   {kind="user", user=jid, session=session}
--   {kind="bot", bot=bot, tok=token_meta}
-- or nil.
local function authenticate(request)
	local auth = request.headers.authorization;
	local token = auth and auth:match("^[Bb]earer%s+(.+)%s*$");
	if not token then return nil; end

	for _, admin_token in ipairs(config.admin_tokens_array) do
		if hashes.equals(admin_token, token) then
			return { kind = "admin" };
		end
	end

	local ctx = { ip = request.ip; ua = request.headers.user_agent };
	local bot, tok, deny_reason = registry.verify_token(token, ctx);
	if bot then
		return { kind = "bot"; bot = bot; tok = tok };
	elseif deny_reason and deny_reason ~= "bad-token" then
		-- Token recognized but rejected on expiry or binding. Audit it:
		-- this is the token-theft signal.
		local name = token:match("^sxb_(............)_.+$");
		audit.record({
			action = "auth.deny";
			bot = name and registry.bot_name_for_token_id(name);
			detail = deny_reason;
			ip = request.ip;
			actor = "sxb:" .. (name or "?");
		});
		return nil, deny_reason;
	end

	if tokens_mod and tokens_mod.get_token_session then
		local session = tokens_mod.get_token_session(token);
		if session then
			local username = session.username or (session.token_info and session.token_info.username);
			local host = session.host or (session.token_info and session.token_info.host) or config.owner_host;
			if username then
				local user_jid = username .. "@" .. host;
				if session_is_admin(session) then
					return { kind = "admin"; user = user_jid; session = session };
				end
				return { kind = "user"; user = user_jid; session = session };
			end
		end
	end
	return nil;
end

local function can_manage(principal, bot)
	if not principal then return false; end
	if principal.kind == "admin" then return true; end
	if principal.kind == "user" and principal.user == bot.owner then return true; end
	if principal.kind == "bot" and principal.bot == bot
		and registry.has_scope(principal.tok, "manage") then return true; end
	return false;
end

-- Per-request rate limit keyed on the principal.
-- Handlers must never return nil after writing an error: the http layer
-- maps nil to 404. Every helper therefore returns the error body to
-- propagate to the route handler.
local function throttle(event, principal)
	local key = principal and (principal.kind == "bot" and ("bot:" .. principal.bot.name)
		or principal.user or "admin") or ("ip:" .. tostring(event.request.ip));
	if not api_limit:take(key) then
		event.response.headers.retry_after = tostring(api_limit:retry_after(key));
		return json_error(event, 429, "rate-limited");
	end
	return nil;
end

local function require_auth(event)
	local request = event.request;
	if not authfail_limit:take("authfail:" .. tostring(request.ip)) then
		return nil, json_error(event, 429, "rate-limited");
	end
	local principal, deny_reason = authenticate(request);
	if not principal then
		event.response.headers.www_authenticate = ("Bearer realm=%q"):format(module.host .. "/bots");
		audit.record({
			action = "auth.fail";
			detail = deny_reason or "bad-credential";
			ip = request.ip;
		});
		return nil, json_error(event, 401, "auth-required");
	end
	local err = throttle(event, principal);
	if err then return nil, err; end
	return principal;
end

-- Resolve bot + auth for management /{name}/... paths. Returns
-- bot, principal or nil, response on failure.
local function resolve_bot(event, name)
	local principal, resp = require_auth(event);
	if not principal then return nil, nil, resp; end
	local bot = registry.get(name);
	if not bot then
		return nil, nil, json_error(event, 404, "not-found");
	end
	return bot, principal;
end

local function owner_or_admin(principal, bot)
	return can_manage(principal, bot)
		or (principal.kind == "bot" and principal.bot == bot);
end

-- Management handlers --------------------------------------------------------

local function handle_list(event)
	local principal, resp = require_auth(event);
	if not principal then return resp; end
	local out = {};
	for _, bot in pairs(registry.all()) do
		if principal.kind == "admin" or bot.owner == principal.user then
			out[#out + 1] = registry.public(bot);
		end
	end
	return json_response(event, { bots = out });
end

local function handle_create(event)
	local principal, resp = require_auth(event);
	if not principal then return resp; end
	if principal.kind == "bot" then
		return json_error(event, 403, "forbidden");
	end
	local payload, code, err = parse_body(event);
	if not payload then return json_error(event, code, err); end
	local owner = payload.owner;
	if principal.kind ~= "admin" or not owner then
		owner = principal.user;
	end
	if not owner then
		return json_error(event, 403, "forbidden");
	end
	local bot, berr = registry.create({
		name = payload.name;
		owner = owner;
		label = type(payload.label) == "string" and payload.label:sub(1, 128) or nil;
		events = type(payload.events) == "table" and payload.events or nil;
	});
	if not bot then
		return json_error(event, berr == "conflict" and 409 or 400, berr);
	end
	if type(payload.webhook) == "table" and payload.webhook.url then
		webhook.validate(payload.webhook.url, function (ok, reason)
			if ok then
				bot.webhook = {
					url = payload.webhook.url;
					secret = type(payload.webhook.secret) == "string" and payload.webhook.secret:sub(1, 128) or nil;
				};
				registry.save(bot);
			else
				module:log("warn", "bot %s webhook rejected at create: %s", bot.name, reason);
			end
		end);
	end
	local token = registry.mint_token(bot);
	local result = registry.public(bot);
	result.token = token;
	audit.record({
		action = "bot.create";
		actor = audit_actor(principal);
		bot = bot.name;
		ip = event.request.ip;
	});
	return json_response(event, result, 201);
end

local function handle_get(event, bot, principal)
	if not owner_or_admin(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	local rec = registry.public(bot);
	rec.stats = bot.stats;
	rec.subscribers = events.subscriber_count(bot);
	rec.webhook_pending = webhook.pending(bot.name);
	rec.webhook_failures = webhook.failures(bot.name);
	return json_response(event, rec);
end

local function handle_update(event, bot, principal)
	if not can_manage(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	local payload, code, err = parse_body(event);
	if not payload then return json_error(event, code, err); end

	if payload.label ~= nil then
		bot.label = type(payload.label) == "string" and payload.label:sub(1, 128) or nil;
	end
	if payload.disabled ~= nil then
		bot.disabled = payload.disabled == true;
		if bot.disabled then
			-- Drop the bot from every room immediately.
			for room_jid, info in pairs(bot.rooms) do
				if info.joined then
					module:send(st.presence({
						from = bot.jid;
						to = room_jid .. "/" .. info.nick;
						type = "unavailable";
					}));
					info.joined = false;
				end
			end
			ws.close_for(bot);
		end
	end
	if type(payload.events) == "table" then
		local e = bot.events or {};
		for k, v in pairs(payload.events) do
			e[k] = v == true;
		end
		bot.events = e;
	end
	if payload.webhook ~= nil then
		if payload.webhook == json.null or payload.webhook == false
			or (type(payload.webhook) == "table" and not payload.webhook.url) then
			bot.webhook = nil;
		elseif type(payload.webhook) == "table" and type(payload.webhook.url) == "string" then
			webhook.validate(payload.webhook.url, function (ok, reason)
				if ok then
					bot.webhook = {
						url = payload.webhook.url;
						secret = type(payload.webhook.secret) == "string" and payload.webhook.secret:sub(1, 128) or nil;
						disabled = false;
					};
				else
					module:log("warn", "bot %s webhook rejected: %s", bot.name, reason);
					bot.webhook = { url = payload.webhook.url; rejected = reason; disabled = true };
				end
				registry.save(bot);
			end);
		end
	end
	if payload.webhook_enabled ~= nil and bot.webhook then
		bot.webhook.disabled = payload.webhook_enabled ~= true;
	end
	registry.save(bot);
	audit.record({
		action = "bot.update";
		actor = audit_actor(principal);
		bot = bot.name;
		detail = payload.disabled ~= nil and ("disabled=" .. tostring(payload.disabled == true))
			or payload.webhook ~= nil and "webhook"
			or "config";
		ip = event.request.ip;
	});
	return json_response(event, registry.public(bot));
end

local function handle_delete(event, bot, principal)
	if not can_manage(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	for room_jid, info in pairs(bot.rooms) do
		if info.joined then
			module:send(st.presence({
				from = bot.jid;
				to = room_jid .. "/" .. info.nick;
				type = "unavailable";
			}));
		end
	end
	events.close(bot, { type = "deleted"; reason = "bot deleted" });
	ws.close_for(bot);
	webhook.remove(bot.name);
	registry.delete(bot.name);
	audit.record({
		action = "bot.delete";
		actor = audit_actor(principal);
		bot = bot.name;
		ip = event.request.ip;
	});
	return json_response(event, { deleted = bot.name });
end

local function handle_tokens_create(event, bot, principal)
	if not can_manage(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	local payload, code, err = parse_body(event);
	if not payload then return json_error(event, code, err); end
	local token, tid_or_err = registry.mint_token(bot, payload.scopes, payload.ttl, {
		name = payload.name;
		ips = payload.ips;
		ua = payload.ua;
	});
	if not token then
		return json_error(event, 400, tid_or_err);
	end
	audit.record({
		action = "token.mint";
		actor = audit_actor(principal);
		bot = bot.name;
		detail = "id=" .. tostring(tid_or_err)
			.. (payload.ips and " ip-bound" or "")
			.. (payload.ua and (" ua=" .. tostring(payload.ua)) or "");
		ip = event.request.ip;
	});
	return json_response(event, { token = token; id = tid_or_err }, 201);
end

local function handle_tokens_list(event, bot, principal)
	if not owner_or_admin(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	local out = {};
	for tid, tok in pairs(bot.tokens) do
		out[#out + 1] = {
			id = tid;
			scopes = tok.scopes;
			created = tok.created;
			expires = tok.expires;
			last_used = tok.last_used;
		};
	end
	return json_response(event, { tokens = out });
end

local function handle_tokens_delete(event, bot, principal, tid)
	if not can_manage(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	if not registry.revoke_token(bot, tid) then
		return json_error(event, 404, "not-found");
	end
	audit.record({
		action = "token.revoke";
		actor = audit_actor(principal);
		bot = bot.name;
		detail = "id=" .. tostring(tid);
		ip = event.request.ip;
	});
	return json_response(event, { revoked = tid });
end

local function handle_ticket(event, bot, principal)
	-- Owner/admin may mint tickets too (handy for debugging), but the
	-- primary caller is the bot runtime.
	if principal.kind == "bot" then
		if principal.bot ~= bot or not registry.has_scope(principal.tok, "read") then
			return json_error(event, 403, "forbidden");
		end
	elseif not can_manage(principal, bot) then
		return json_error(event, 403, "forbidden");
	end
	if not ticket_limit:take("ticket:" .. bot.name) then
		return json_error(event, 429, "rate-limited");
	end
	local ticket = deps.mint_ticket(bot, event.request.ip);
	return json_response(event, { ticket = ticket; expires_in = config.ticket_ttl });
end

-- Runtime handlers -----------------------------------------------------------

local function bot_auth(event, bot, required_scope)
	-- Bot token or a ticket (stream endpoints). Owners/admins may also
	-- drive a bot for debugging.
	if not authfail_limit:take("authfail:" .. tostring(event.request.ip)) then
		return nil, json_error(event, 429, "rate-limited");
	end
	local principal, deny_reason = authenticate(event.request);
	local query = decode_query(event.request.url and event.request.url.query);
	if not principal and query.ticket then
		local tbot = deps.use_ticket(query.ticket, event.request.ip);
		if tbot and tbot == bot then
			return { kind = "bot"; bot = bot; tok = { scopes = { "read" } }; ticket = true };
		end
	end
	if not principal then
		event.response.headers.www_authenticate = ("Bearer realm=%q"):format(module.host .. "/bots");
		audit.record({
			action = "auth.fail";
			bot = bot.name;
			detail = deny_reason or "bad-credential";
			ip = event.request.ip;
		});
		return nil, json_error(event, 401, "auth-required");
	end
	local err = throttle(event, principal);
	if err then return nil, err; end
	if principal.kind == "bot" then
		if principal.bot ~= bot then
			return nil, json_error(event, 403, "forbidden");
		end
		if required_scope and not registry.has_scope(principal.tok, required_scope) then
			return nil, json_error(event, 403, "missing-scope:" .. required_scope);
		end
		return principal;
	end
	if can_manage(principal, bot) then
		return principal;
	end
	return nil, json_error(event, 403, "forbidden");
end

local function sse_send_chunk(conn, chunk)
	conn:write(("%x\r\n%s\r\n"):format(#chunk, chunk));
end

local function handle_events_sse(event, bot)
	local principal, resp = bot_auth(event, bot, "read");
	if not principal then return resp; end

	local request, response = event.request, event.response;
	local query = decode_query(request.url and request.url.query);

	response.status_code = 200;
	response.headers.content_type = "text/event-stream";
	response.headers.cache_control = "no-cache";
	response.headers.transfer_encoding = "chunked";
	response.persistent = false;
	response:write_headers();

	local conn = response.conn;
	local closed = false;

	local function send_event(ev)
		if closed then return false; end
		local frame = events.sse_frame(ev);
		if frame then sse_send_chunk(conn, frame); end
		return not closed;
	end

	-- Resume support: Last-Event-ID header or ?since=.
	local since = tonumber(request.headers.last_event_id or "") or tonumber(query.since or "");
	local contiguous = events.replay(bot, since, send_event);
	if not contiguous then
		sse_send_chunk(conn, "event: resync\ndata: {\"reason\":\"buffer-gap\"}\n\n");
	end

	local unsubscribe = events.subscribe(bot, { send = send_event });
	sse_conns[conn] = unsubscribe;
	response.on_destroy = function ()
		closed = true;
		sse_conns[conn] = nil;
		unsubscribe();
	end;
	return true;
end

local function handle_ws(event, bot)
	if not config.ws_enabled then
		return json_error(event, 404, "not-found");
	end
	local principal, resp = bot_auth(event, bot, "read");
	if not principal then return resp; end
	return ws.handle_request(event, bot, principal.kind == "bot" and principal.tok or nil);
end

local function handle_action(event, bot, action)
	local scope = deps.action_scope[action];
	local principal, resp = bot_auth(event, bot, scope);
	if not principal then return resp; end
	local payload, code, err = parse_body(event);
	if not payload then return json_error(event, code, err); end
	payload.op = action;
	local ok, result = dispatch(bot, principal.kind == "bot" and principal.tok or nil, payload);
	if not ok then
		return json_error(event, result == "rate-limited" and 429 or 400, tostring(result));
	end
	return json_response(event, result or {});
end

-- Routing --------------------------------------------------------------------

local base_path = module:http_url("bots", "/bots"):match("^https?://[^/]+(/.*)$") or "/bots";
if base_path:sub(-1) == "/" and #base_path > 1 then
	base_path = base_path:sub(1, -2);
end

local function tail(path)
	local t = (path or ""):sub(#base_path + 1);
	if t == "" then t = "/"; end
	return t;
end

local function get_root(event)
	return handle_list(event);
end

local function post_root(event)
	return handle_create(event);
end

local function split_tail(path)
	local parts = {};
	for part in tail(path):gmatch("[^/]+") do
		parts[#parts + 1] = (part:gsub("%%(%x%x)", function (h) return string.char(tonumber(h, 16)); end));
	end
	return parts;
end

-- Admin-only audit read: GET /bots/audit?bot=x&since=ts&limit=n
local function handle_audit(event)
	local principal, resp = require_auth(event);
	if not principal then return resp; end
	if principal.kind ~= "admin" then
		return json_error(event, 403, "forbidden");
	end
	local query = decode_query(event.request.url and event.request.url.query);
	local limit = tonumber(query.limit or "") or 100;
	if limit > 1000 then limit = 1000; end
	return json_response(event, {
		entries = audit.list({
			bot = query.bot;
			since = tonumber(query.since or "");
			limit = limit;
		});
	});
end

local function get_wildcard(event)
	local parts = split_tail(event.request.path);
	local name = parts[1];
	if not name then return handle_list(event); end
	if name == "audit" then
		return handle_audit(event);
	end
	local sub = parts[2];
	if sub == nil then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_get(event, bot, principal);
	elseif sub == "events" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		return handle_events_sse(event, bot);
	elseif sub == "ws" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		return handle_ws(event, bot);
	elseif sub == "rooms" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		return handle_action(event, bot, "rooms");
	elseif sub == "occupants" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		local query = decode_query(event.request.url and event.request.url.query);
		local principal, resp = bot_auth(event, bot, "read");
		if not principal then return resp; end
		local ok, result = dispatch(bot, principal.kind == "bot" and principal.tok or nil, { op = "occupants"; room = query.room });
		if not ok then return json_error(event, 400, tostring(result)); end
		return json_response(event, result);
	elseif sub == "status" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		return handle_action(event, bot, "status");
	elseif sub == "tokens" then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_tokens_list(event, bot, principal);
	end
	return json_error(event, 404, "not-found");
end

local function post_wildcard(event)
	local parts = split_tail(event.request.path);
	local name, sub = parts[1], parts[2];
	if not name then return json_error(event, 404, "not-found"); end
	if sub == "tokens" then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_tokens_create(event, bot, principal);
	elseif sub == "ticket" then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_ticket(event, bot, principal);
	elseif sub == "send" or sub == "join" or sub == "leave" or sub == "presence" then
		local bot = registry.get(name);
		if not bot then return json_error(event, 404, "not-found"); end
		return handle_action(event, bot, sub);
	end
	return json_error(event, 404, "not-found");
end

local function patch_wildcard(event)
	local parts = split_tail(event.request.path);
	local name = parts[1];
	if not name or parts[2] then return json_error(event, 404, "not-found"); end
	local bot, principal, resp = resolve_bot(event, name);
	if not bot then return resp; end
	return handle_update(event, bot, principal);
end

local function delete_wildcard(event)
	local parts = split_tail(event.request.path);
	local name, sub, tid = parts[1], parts[2], parts[3];
	if not name then return json_error(event, 404, "not-found"); end
	if sub == "tokens" and tid then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_tokens_delete(event, bot, principal, tid);
	elseif not sub then
		local bot, principal, resp = resolve_bot(event, name);
		if not bot then return resp; end
		return handle_delete(event, bot, principal);
	end
	return json_error(event, 404, "not-found");
end

-- Access log wrapper: one line per request with method, path, status
-- and source IP. Held-open streams (SSE/WS) log at setup time.
local function access_log(handler)
	return function (event)
		local result = handler(event);
		module:log("info", "bots-api: %s %s -> %s from %s",
			event.request.method, event.request.path,
			tostring(event.response.status_code or 200),
			tostring(event.request.ip));
		return result;
	end;
end

module:provides("http", {
	name = "bots";
	default_path = "/bots";
	cors = { enabled = module:get_option_boolean("bot_api_cors", false) };
	route = {
		["GET"] = access_log(get_root);
		["GET /"] = access_log(get_root);
		["POST"] = access_log(post_root);
		["POST /"] = access_log(post_root);
		["GET /*"] = access_log(get_wildcard);
		["POST /*"] = access_log(post_wildcard);
		["PATCH /*"] = access_log(patch_wildcard);
		["DELETE /*"] = access_log(delete_wildcard);
	};
});

-- SSE keepalive: comment lines keep intermediaries from buffering or
-- timing out idle streams.
module:add_timer(config.sse_keepalive, function ()
	for conn in pairs(sse_conns) do
		sse_send_chunk(conn, ": ka\n\n");
	end
	return config.sse_keepalive;
end);

module:log("info", "bots HTTP API mounted at %s on %s", base_path, module.host);

end -- of provider function
