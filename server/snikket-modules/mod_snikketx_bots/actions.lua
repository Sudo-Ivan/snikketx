-- Outbound action dispatch for mod_snikketx_bots. The same actions back
-- the REST endpoints and WebSocket frames. Returns a table with
-- actions, action_scope and dispatch after wiring the shared env.
--luacheck: ignore 111 113/module 131 143/module

local st = require "prosody.util.stanza";
local jid = require "prosody.util.jid";
local id = require "prosody.util.id";

local xmlns_chatstates = "http://jabber.org/protocol/chatstates";
local xmlns_reactions = "urn:xmpp:reactions:0";
local xmlns_reply = "urn:xmpp:reply:0";
local xmlns_eme = "urn:xmpp:eme:0";
local xmlns_hints = "urn:xmpp:hints";

local omemo_xmlns = {
	["eu.siacs.conversations.axolotl"] = true;
	["urn:xmpp:omemo:2"] = true;
};

local valid_chat_states = { composing = true; paused = true; active = true; inactive = true; gone = true };

return function (env)

local config = env.config;
local registry = env.registry;
local events = env.events;
local pending_joins = env.pending_joins;
local send_limit = env.send_limit;

local actions = {};

-- Room traffic is only allowed to rooms the bot actually occupies; PMs
-- to occupants go to room@conf/nick. Direct messages go to the local
-- user host or an explicitly allowed remote host.
local function target_policy(bot, to, kind)
	local to_node, to_host, to_res = jid.split(to);
	if not to_host then return nil, "bad-jid"; end
	if config.muc_hosts:contains(to_host) then
		local room_jid = to_node and (to_node .. "@" .. to_host);
		if not room_jid or not bot.rooms[room_jid] or not bot.rooms[room_jid].joined then
			return nil, "not-in-room";
		end
		if kind == "groupchat" and to_res then
			return nil, "bad-jid"; -- groupchat goes to the bare room JID
		end
		if kind ~= "groupchat" and not to_res then
			return nil, "bad-jid"; -- PMs go to room@conf/nick
		end
		return true;
	end
	if to_host == config.owner_host then
		return true; -- local user DM
	end
	if to_host == module.host then
		-- Bot-to-bot DM: allowed only if the target bot exists.
		if to_node and env.registry.get(to_node) then
			return true;
		end
		return nil, "no-such-bot";
	end
	if config.send_hosts:contains(to_host) then
		return true; -- explicitly allowed remote target
	end
	return nil, "target-not-allowed";
end

function actions.send(bot, tok, msg) -- luacheck: ignore 212/tok
	local kind = msg.kind or (msg.to and config.muc_hosts:contains(jid.host(msg.to) or "") and "groupchat") or "chat";
	local to = jid.prep(msg.to or "");
	if not to then return nil, "bad-jid"; end
	local ok, err = target_policy(bot, to, kind);
	if not ok then return nil, err; end
	if not send_limit:take("send:" .. bot.name) then
		bot.stats.sends_denied = (bot.stats.sends_denied or 0) + 1;
		return nil, "rate-limited";
	end
	-- Per-target cap: a stolen token cannot hammer a single user even
	-- while the global send budget lasts.
	if not env.target_limit:take("sendto:" .. bot.name .. ":" .. to) then
		bot.stats.sends_denied = (bot.stats.sends_denied or 0) + 1;
		return nil, "rate-limited-target";
	end

	local stanza = st.message({ to = to; type = kind; id = msg.stanza_id or id.medium() });

	if type(msg.body) == "string" and msg.body ~= "" then
		if #msg.body > config.max_message_body then
			return nil, "body-too-large";
		end
		stanza:tag("body"):text(msg.body):up();
	end
	if type(msg.subject) == "string" and msg.subject ~= "" and kind == "groupchat" then
		stanza:tag("subject"):text(msg.subject:sub(1, 512)):up();
	end
	if type(msg.thread) == "string" and msg.thread ~= "" then
		stanza:tag("thread"):text(msg.thread:sub(1, 128)):up();
	end
	if type(msg.reply_to) == "table" and type(msg.reply_to.id) == "string" then
		stanza:tag("reply", {
			xmlns = xmlns_reply;
			to = type(msg.reply_to.to) == "string" and msg.reply_to.to or to;
			id = msg.reply_to.id:sub(1, 128);
		}):up();
	end
	if type(msg.reactions) == "table" and type(msg.reactions.id) == "string"
		and type(msg.reactions.emojis) == "table" then
		local r = stanza:tag("reactions", { xmlns = xmlns_reactions; id = msg.reactions.id:sub(1, 128) });
		local count = 0;
		for _, emoji in ipairs(msg.reactions.emojis) do
			if type(emoji) == "string" and #emoji <= 16 and count < 20 then
				r:tag("reaction"):text(emoji):up();
				count = count + 1;
			end
		end
		r:up();
	end
	if type(msg.chat_state) == "string" and valid_chat_states[msg.chat_state] then
		stanza:tag(msg.chat_state, { xmlns = xmlns_chatstates }):up();
	end
	-- OMEMO passthrough: the runtime does its own crypto, we validate and
	-- construct the envelope. See README for the threat model.
	if type(msg.omemo) == "table" then
		local o = msg.omemo;
		if not omemo_xmlns[o.xmlns or ""] then
			return nil, "bad-omemo";
		end
		if type(o.payload) ~= "string" or #o.payload > 131072
			or (o.iv and #o.iv > 64)
			or (o.sid and #tostring(o.sid) > 32) then
			return nil, "bad-omemo";
		end
		local enc = stanza:tag("encrypted", { xmlns = o.xmlns });
		local header = enc:tag("header", { sid = tostring(o.sid or "") });
		if type(o.keys) == "table" then
			if #o.keys > 256 then return nil, "bad-omemo"; end
			for _, key in ipairs(o.keys) do
				if type(key) ~= "table" or type(key.key) ~= "string" or #key.key > 512
					or (key.rid and #tostring(key.rid) > 32) then
					return nil, "bad-omemo";
				end
				header:tag("key", {
					rid = key.rid and tostring(key.rid) or nil;
					prekey = key.prekey and "true" or nil;
				}):text(key.key):up();
			end
		end
		if o.iv then
			header:tag("iv"):text(o.iv):up();
		end
		header:up();
		enc:tag("payload"):text(o.payload):up();
		enc:up();
		-- Storage hint and EME marker so clients and archives handle it.
		stanza:tag("store", { xmlns = xmlns_hints }):up();
		stanza:tag("encryption", {
			xmlns = xmlns_eme;
			namespace = o.xmlns;
			name = "OMEMO";
		}):up();
	end
	if not stanza:get_child("body") and not stanza:get_child("subject")
		and #stanza.tags == 0 then
		return nil, "empty-message";
	end

	env.bot_send(bot, stanza);
	bot.stats.sends = (bot.stats.sends or 0) + 1;
	return { stanza_id = stanza.attr.id };
end

function actions.join(bot, tok, msg) -- luacheck: ignore 212/tok
	local room_jid = jid.prep(msg.room or "");
	if not room_jid then return nil, "bad-jid"; end
	local room_node, room_host = jid.split(room_jid);
	if not room_node or not config.muc_hosts:contains(room_host) then
		return nil, "room-not-allowed";
	end
	local nick = msg.nick or bot.label or bot.name;
	if type(nick) ~= "string" or nick == "" or #nick > 64 then
		return nil, "bad-nick";
	end
	local info = bot.rooms[room_jid];
	if info and info.joined then
		return { room = room_jid; nick = info.nick; already = true };
	end
	bot.rooms[room_jid] = {
		nick = nick;
		joined = false;
		password = type(msg.password) == "string" and msg.password:sub(1, 128) or nil;
	};
	registry.save(bot);
	env.send_join(bot, room_jid, nick, bot.rooms[room_jid].password);
	env.audit.record({ action = "bot.join"; bot = bot.name; detail = room_jid });
	return { room = room_jid; nick = nick; pending = true };
end

function actions.leave(bot, tok, msg) -- luacheck: ignore 212/tok
	local room_jid = jid.prep(msg.room or "");
	if not room_jid or not bot.rooms[room_jid] then
		return nil, "not-in-room";
	end
	pending_joins[bot.name .. " " .. room_jid] = nil;
	env.send_leave(bot, room_jid, msg.reason);
	bot.rooms[room_jid] = nil;
	registry.save(bot);
	env.emit(bot, { type = "leave"; room = room_jid; reason = msg.reason });
	env.audit.record({ action = "bot.leave"; bot = bot.name; detail = room_jid });
	return { room = room_jid };
end

function actions.presence(bot, tok, msg) -- luacheck: ignore 212/tok
	local show = msg.show;
	if show ~= nil and not ({ away = true; xa = true; dnd = true; chat = true })[show] then
		return nil, "bad-show";
	end
	local status = type(msg.status) == "string" and msg.status:sub(1, 256) or nil;
	local sent = 0;
	for room_jid, info in pairs(bot.rooms) do
		if info.joined then
			local p = st.presence({ to = room_jid .. "/" .. info.nick });
			if show then p:tag("show"):text(show):up(); end
			if status then p:tag("status"):text(status):up(); end
			env.bot_send(bot, p);
			sent = sent + 1;
		end
	end
	return { sent = sent };
end

function actions.rooms(bot) -- luacheck: ignore 212
	local out = {};
	for room_jid, info in pairs(bot.rooms) do
		out[#out + 1] = { room = room_jid; nick = info.nick; joined = info.joined };
	end
	return { rooms = out };
end

function actions.occupants(bot, tok, msg) -- luacheck: ignore 212/tok
	local room_jid = jid.prep(msg.room or "");
	if not room_jid then return nil, "bad-jid"; end
	if not bot.rooms[room_jid] then return nil, "not-in-room"; end
	local list, err = env.room_occupants(room_jid);
	if not list then return nil, err; end
	return { room = room_jid; occupants = list };
end

function actions.status(bot) -- luacheck: ignore 212
	return {
		bot = bot.name;
		jid = bot.jid;
		disabled = bot.disabled or false;
		rooms = bot.rooms;
		stats = bot.stats;
		subscribers = events.subscriber_count(bot);
		webhook_pending = env.webhook.pending(bot.name);
		webhook_failures = env.webhook.failures(bot.name);
		buffer = { oldest = events.oldest_seq(bot); newest = events.newest_seq(bot) };
	};
end

-- Scope required per action.
local action_scope = {
	send = "write";
	join = "rooms";
	leave = "rooms";
	presence = "presence";
	rooms = "read";
	occupants = "read";
	status = "read";
};

local function dispatch(bot, tok, msg)
	local action = msg.op or msg.action;
	if type(action) ~= "string" or not actions[action] then
		return false, "unknown-action";
	end
	if bot.disabled then
		return false, "bot-disabled";
	end
	if tok and not registry.has_scope(tok, action_scope[action]) then
		return false, "forbidden";
	end
	local ok, result, err = pcall(actions[action], bot, tok, msg);
	if not ok then
		module:log("error", "bot action %s failed for %s: %s", action, bot.name, result);
		return false, "internal-error";
	end
	if result == nil then
		return false, err or "error";
	end
	return true, result;
end

return {
	actions = actions;
	action_scope = action_scope;
	dispatch = dispatch;
};

end
