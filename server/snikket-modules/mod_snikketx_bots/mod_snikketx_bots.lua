-- SnikketX bot framework. Runs as an internal component
-- (Component "bots.example.com" "snikketx_bots") that owns a virtual JID
-- space: every node under the component host is a bot managed through a
-- JSON/HTTP API. Bots receive XMPP traffic as JSON events (webhook push,
-- SSE or WebSocket) and act through REST-style calls. Bot authors need
-- no XMPP library and no real account.
--
-- Security model in README.md. Every stanza from a bot JID is
-- constructed here; the API only accepts structured fields, never XML.
--
-- Layout: this file holds config, shared state, lifecycle and shell
-- commands. Inbound stanza handling lives in stanzas.lua, outbound
-- action dispatch in actions.lua, HTTP routes in httpapi.lua, the
-- WebSocket transport in ws.lua, delivery state in webhook.lua, event
-- buffering in events.lua, bot records in registry.lua and token
-- buckets in ratelimit.lua.
--luacheck: ignore 111 113/module 131 143/module

local st = require "prosody.util.stanza";
local jid = require "prosody.util.jid";
local id = require "prosody.util.id";

local ratelimit = module:require("ratelimit");
local registry = module:require("registry");
local events = module:require("events");
local webhook = module:require("webhook");
local audit = module:require("audit");
local ws = module:require("ws");

local xmlns_muc = "http://jabber.org/protocol/muc";

-- Config -----------------------------------------------------------------

local owner_host = module:get_option_string("bot_owner_host")
	or module.host:match("^[^.]+%.(.+)$");

local config = {
	owner_host = owner_host;
	muc_hosts = module:get_option_set("bot_muc_hosts", {});
	send_hosts = module:get_option_set("bot_allowed_remote_hosts", {}); -- remote DM targets, default none
	admin_tokens_array = module:get_option_array("bot_admin_tokens", {});
	max_per_owner = module:get_option_number("bot_max_per_owner", 10);
	max_total = module:get_option_number("bot_max_total", 200);
	max_tokens_per_bot = module:get_option_number("bot_max_tokens", 10);
	send_rate = module:get_option_number("bot_send_rate", 30); -- per minute
	event_rate = module:get_option_number("bot_event_rate", 600); -- per minute
	event_buffer = module:get_option_number("bot_event_buffer", 512);
	max_body = module:get_option_number("bot_api_max_body", 65536);
	max_message_body = module:get_option_number("bot_max_message_body", 8192);
	webhook_require_https = module:get_option_boolean("bot_webhook_require_https", true);
	webhook_allow_private = module:get_option_boolean("bot_webhook_allow_private", false);
	webhook_allowed_hosts = module:get_option("bot_webhook_allowed_hosts");
	webhook_queue_size = module:get_option_number("bot_webhook_queue_size", 1024);
	webhook_timeout = module:get_option_number("bot_webhook_timeout", 30);
	ws_enabled = module:get_option_boolean("bot_websocket", true);
	ws_frame_limit = module:get_option_number("bot_websocket_frame_limit", 131072);
	sse_keepalive = module:get_option_number("bot_sse_keepalive", 25);
	ticket_ttl = module:get_option_number("bot_ticket_ttl", 60);
	auto_accept_invites = module:get_option_boolean("bot_auto_accept_invites", false);
	audit_size = module:get_option_number("bot_audit_size", 1000);
};

if not owner_host then
	module:log("warn", "could not derive the user host from %s; set bot_owner_host", module.host);
end

-- Rate limiters ----------------------------------------------------------

local send_limit = ratelimit.new_registry(config.send_rate);
local target_limit = ratelimit.new_registry(module:get_option_number("bot_send_rate_target", 60));
local event_limit = ratelimit.new_registry(config.event_rate);
local sender_limit = ratelimit.new_registry(module:get_option_number("bot_dm_sender_rate", 60));
local api_limit = ratelimit.new_registry(module:get_option_number("bot_api_rate", 120));
local ticket_limit = ratelimit.new_registry(60);
local authfail_limit = ratelimit.new_registry(module:get_option_number("bot_authfail_rate", 30));

module:add_timer(300, function ()
	send_limit:sweep();
	target_limit:sweep();
	event_limit:sweep();
	sender_limit:sweep();
	api_limit:sweep();
	ticket_limit:sweep();
	authfail_limit:sweep();
	return 300;
end);

-- Stores and shared state --------------------------------------------------

local bots_store = module:open_store("bots");
registry.init(config, bots_store);
events.init(config, registry, webhook);
webhook.init(config, registry);
audit.init(config, module:open_store("bots_audit"));

-- Pending joins: "botname room@conf" -> { nick = requested nick }
local pending_joins = {};

-- Set during module unload / server stop. Self-presence unavailable
-- stanzas caused by our own shutdown leaves must not wipe the persisted
-- room membership, so bots rejoin their rooms on the next start.
local shutting_down = false;

-- Rate-limited event emission shared by stanzas.lua and actions.lua.
local function emit(bot, event)
	if not event_limit:take("events:" .. bot.name) then
		bot.stats.events_dropped = (bot.stats.events_dropped or 0) + 1;
		return nil;
	end
	return events.emit(bot, event);
end

-- MUC helpers --------------------------------------------------------------

local function muc_module(host)
	local h = prosody.hosts[host];
	return h and h.modules and h.modules.muc or nil;
end

local function get_room(room_jid)
	local _, host = jid.split(room_jid);
	local muc = host and muc_module(host);
	return muc and muc.get_room_from_jid(room_jid);
end

local function room_real_jid(room_jid, nick)
	local room = get_room(room_jid);
	if not room then return nil; end
	local occupant = room:get_occupant_by_nick(nick);
	return occupant and occupant.bare_jid or nil;
end

local function room_occupants(room_jid)
	local room = get_room(room_jid);
	if not room then return nil, "room-not-found"; end
	local out = {};
	for occupant_jid, occupant in room:each_occupant() do
		out[#out + 1] = {
			nick = jid.resource(occupant_jid);
			jid = occupant.bare_jid;
			role = occupant.role;
			affiliation = room:get_affiliation(occupant.bare_jid);
		};
	end
	return out;
end

-- Outbound stanza helpers --------------------------------------------------

local function bot_send(bot, stanza)
	stanza.attr.from = bot.jid;
	return module:send(stanza);
end

local function send_join(bot, room_jid, nick, password)
	local presence = st.presence({ from = bot.jid; to = room_jid .. "/" .. nick })
		:tag("x", { xmlns = xmlns_muc });
	if password then
		presence:tag("password"):text(password):up();
	end
	pending_joins[bot.name .. " " .. room_jid] = { nick = nick };
	module:send(presence);
end

local function send_leave(bot, room_jid, reason)
	local info = bot.rooms[room_jid];
	local nick = info and info.nick or bot.name;
	local presence = st.presence({
		from = bot.jid;
		to = room_jid .. "/" .. nick;
		type = "unavailable";
	});
	if reason and reason ~= "" then
		presence:tag("status"):text(reason:sub(1, 256)):up();
	end
	module:send(presence);
end

-- Stream tickets -----------------------------------------------------------

-- One-time tickets for SSE/WS clients that cannot set headers. Tickets
-- are bound to the minting request's source IP, so a ticket leaked via
-- referer or log scraper is useless from another address.
-- ticket string -> { bot = name, exp = epoch, used = bool, ip = str }
local tickets = {};

local function mint_ticket(bot, ip)
	local ticket = id.long();
	tickets[ticket] = {
		bot = bot.name;
		exp = os.time() + config.ticket_ttl;
		ip = ip;
	};
	return ticket;
end

local function use_ticket(ticket, ip)
	local t = ticket and tickets[ticket];
	if not t or t.used or t.exp <= os.time() then
		return nil;
	end
	if t.ip and ip and t.ip ~= ip then
		return nil;
	end
	t.used = true;
	tickets[ticket] = nil;
	return registry.get(t.bot);
end

module:add_timer(120, function ()
	local now = os.time();
	for k, t in pairs(tickets) do
		if t.exp <= now or t.used then tickets[k] = nil; end
	end
	return 120;
end);

-- Wiring -------------------------------------------------------------------

local env = {
	config = config;
	registry = registry;
	events = events;
	webhook = webhook;
	ws = ws;
	emit = emit;
	bot_send = bot_send;
	send_join = send_join;
	send_leave = send_leave;
	pending_joins = pending_joins;
	room_real_jid = room_real_jid;
	room_occupants = room_occupants;
	send_limit = send_limit;
	target_limit = target_limit;
	sender_limit = sender_limit;
	audit = audit;
	is_shutting_down = function () return shutting_down; end;
};

local actions_mod = module:require("actions")(env);
env.actions = actions_mod.actions;

module:require("stanzas")(env);

ws.init(config, events, actions_mod.dispatch);

module:require("httpapi")({
	config = config;
	registry = registry;
	events = events;
	webhook = webhook;
	ws = ws;
	dispatch = actions_mod.dispatch;
	mint_ticket = mint_ticket;
	use_ticket = use_ticket;
	api_limit = api_limit;
	ticket_limit = ticket_limit;
	authfail_limit = authfail_limit;
	audit = audit;
	action_scope = actions_mod.action_scope;
});

-- Lifecycle ----------------------------------------------------------------

local function rejoin_rooms(bot)
	for room_jid, info in pairs(bot.rooms) do
		if info.joined then
			info.joined = false; -- re-confirmed by self-presence
			send_join(bot, room_jid, info.nick, info.password);
		end
	end
end

local function leave_all(bot, reason)
	for room_jid, info in pairs(bot.rooms) do
		if info.joined then
			send_leave(bot, room_jid, reason);
		end
	end
end

function module.unload()
	shutting_down = true;
	for _, bot in pairs(registry.all()) do
		leave_all(bot, "bot service unloading");
	end
end

module:hook_global("server-stopping", function ()
	shutting_down = true;
	for _, bot in pairs(registry.all()) do
		leave_all(bot, "server stopping");
	end
end);

-- Disable bots whose owner account is deleted.
module:hook_global("user-deleted", function (event)
	local user_jid = event.username and event.host and (event.username .. "@" .. event.host)
		or event.jid;
	if not user_jid then return; end
	for _, bot in pairs(registry.all()) do
		if bot.owner == user_jid and not bot.disabled then
			bot.disabled = true;
			leave_all(bot, "owner account deleted");
			registry.save(bot);
			module:log("info", "disabled bot %s: owner %s deleted", bot.name, user_jid);
		end
	end
end);

-- Re-join persisted rooms once every host is up. During startup the MUC
-- host may not be active yet, so wait for server-started; on a module
-- reload mid-runtime the server already started, so rejoin immediately.
local function rejoin_all()
	for _, bot in pairs(registry.all()) do
		if not bot.disabled then
			rejoin_rooms(bot);
		end
	end
end

if prosody.start_time then
	rejoin_all();
else
	module:hook_global("server-started", rejoin_all);
end

-- Shell commands -----------------------------------------------------------

module:add_item("shell-command", {
	section = "bots";
	section_desc = "Manage SnikketX bots";
	name = "list";
	desc = "List bots on this component";
	host_selector = "host";
	args = {
		{ name = "host"; type = "string" };
	};
	handler = function (self, host) -- luacheck: ignore 212/self
		local lines = {};
		for _, bot in pairs(registry.all()) do
			local n = 0;
			for _ in pairs(bot.rooms) do n = n + 1; end
			lines[#lines + 1] = ("%s (owner %s, %d rooms%s)"):format(
				bot.jid, bot.owner, n, bot.disabled and ", disabled" or "");
		end
		if #lines == 0 then return true, "No bots"; end
		return true, table.concat(lines, "\n");
	end;
});

module:add_item("shell-command", {
	section = "bots";
	section_desc = "Manage SnikketX bots";
	name = "create";
	desc = "Create a bot: bots:create host owner_jid name [label]";
	host_selector = "host";
	args = {
		{ name = "host"; type = "string" };
		{ name = "owner"; type = "string" };
		{ name = "name"; type = "string" };
		{ name = "label"; type = "string"; required = false };
	};
	handler = function (self, host, owner, name, label) -- luacheck: ignore 212/self
		local bot, err = registry.create({ name = name; owner = owner; label = label });
		if not bot then return false, err; end
		local token = registry.mint_token(bot);
		return true, ("Created %s\nToken: %s"):format(bot.jid, token);
	end;
});

module:add_item("shell-command", {
	section = "bots";
	section_desc = "Manage SnikketX bots";
	name = "delete";
	desc = "Delete a bot: bots:delete host name";
	host_selector = "host";
	args = {
		{ name = "host"; type = "string" };
		{ name = "name"; type = "string" };
	};
	handler = function (self, host, name) -- luacheck: ignore 212/self
		local bot = registry.get(name);
		if not bot then return false, "not-found"; end
		leave_all(bot, "bot deleted");
		events.close(bot, { type = "deleted"; reason = "bot deleted" });
		ws.close_for(bot);
		webhook.remove(name);
		registry.delete(name);
		return true, "Deleted " .. name;
	end;
});

module:add_item("shell-command", {
	section = "bots";
	section_desc = "Manage SnikketX bots";
	name = "token";
	desc = "Mint a new bot token: bots:token host name";
	host_selector = "host";
	args = {
		{ name = "host"; type = "string" };
		{ name = "name"; type = "string" };
	};
	handler = function (self, host, name) -- luacheck: ignore 212/self
		local bot = registry.get(name);
		if not bot then return false, "not-found"; end
		local token, tid = registry.mint_token(bot);
		if not token then return false, tid; end
		return true, token;
	end;
});

local muc_host_names = {};
for h in config.muc_hosts:items() do muc_host_names[#muc_host_names + 1] = h; end
module:log("info", "SnikketX bots ready on %s (muc hosts: %s)",
	module.host, table.concat(muc_host_names, ", "));
