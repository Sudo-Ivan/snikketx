-- Inbound stanza handling for mod_snikketx_bots: message, presence and
-- IQ routing for the component JID space, MUC self-presence processing
-- and event normalization. Registers all hooks on load.
--luacheck: ignore 111 113/module 131 143/module

local st = require "prosody.util.stanza";
local jid = require "prosody.util.jid";
local id = require "prosody.util.id";

local xmlns_muc = "http://jabber.org/protocol/muc";
local xmlns_muc_user = "http://jabber.org/protocol/muc#user";
local xmlns_muc_owner = "http://jabber.org/protocol/muc#owner";
local xmlns_data = "jabber:x:data";
local xmlns_delay = "urn:xmpp:delay";
local xmlns_chatstates = "http://jabber.org/protocol/chatstates";
local xmlns_reactions = "urn:xmpp:reactions:0";
local xmlns_reply = "urn:xmpp:reply:0";
local xmlns_ping = "urn:xmpp:ping";
local xmlns_disco_info = "http://jabber.org/protocol/disco#info";
local xmlns_disco_items = "http://jabber.org/protocol/disco#items";
local xmlns_version = "jabber:iq:version";
local xmlns_conference = "jabber:x:conference";
local xmlns_eme = "urn:xmpp:eme:0";

-- OMEMO namespaces: v1 (legacy axolotl, deployed everywhere) and v2.
local omemo_xmlns = {
	["eu.siacs.conversations.axolotl"] = true;
	["urn:xmpp:omemo:2"] = true;
};

local valid_chat_states = { composing = true; paused = true; active = true; inactive = true; gone = true };

local bot_features = {
	xmlns_disco_info;
	xmlns_ping;
	xmlns_muc;
	xmlns_chatstates;
	xmlns_reactions;
	xmlns_reply;
	"urn:xmpp:sid:0";
	"urn:xmpp:message-correct:0";
};

return function (env)

local config = env.config;
local registry = env.registry;
local pending_joins = env.pending_joins;
local emit = env.emit;
local bot_send = env.bot_send;
local room_real_jid = env.room_real_jid;
local actions = env.actions;

local function statuses_of(x)
	-- Name-only scan: children of the muc#user x element carry no xmlns
	-- of their own, and get_child/childtags only match children sharing
	-- the parent's xmlns when no xmlns is given.
	local codes = {};
	if x then
		for tag in x:children() do
			if tag.name == "status" then
				local code = tonumber(tag.attr.code);
				if code then codes[code] = true; end
			end
		end
	end
	return codes;
end

local function delay_stamp(stanza)
	local delay = stanza:get_child("delay", xmlns_delay);
	return delay and delay.attr.stamp or nil;
end

-- The module does not do crypto: bots relay OMEMO payloads verbatim so a
-- runtime that manages its own key material can decrypt outside Prosody.
-- Extracted fields are capped so a hostile stanza cannot blow up event
-- size.
local function omemo_info(stanza)
	-- Name-only lookups everywhere below: namespaced children do not
	-- share the stanza's xmlns, and OMEMO's inner elements carry none.
	local enc = stanza:child_with_name("encrypted");
	if not enc or not omemo_xmlns[enc.attr.xmlns or ""] then
		return nil;
	end
	local header = enc:child_with_name("header");
	local out = {
		xmlns = enc.attr.xmlns;
		sid = header and header.attr.sid or nil;
	};
	if header then
		local keys = {};
		for tag in header:children() do
			if tag.name == "iv" then
				local iv = tag:get_text();
				if iv and #iv <= 64 then out.iv = iv; end
			elseif tag.name == "key" and #keys < 256 then
				local data = tag:get_text();
				if data and #data <= 512 then
					keys[#keys + 1] = {
						rid = tag.attr.rid;
						prekey = tag.attr.prekey == "true" or nil;
						key = data;
					};
				end
			end
		end
		out.keys = keys;
	end
	local payload_tag = enc:child_with_name("payload");
	local payload = payload_tag and payload_tag:get_text();
	if payload and #payload <= 131072 then
		out.payload = payload;
	end
	return out;
end

local function auto_accept_invite(bot, from, room_jid, password)
	local want = bot.events.auto_accept_invites;
	if want == nil then want = config.auto_accept_invites; end
	if not want then return; end
	-- Only local users can trigger auto-join; a remote inviter could
	-- otherwise park bots in arbitrary rooms.
	if jid.host(from or "") == config.owner_host then
		actions.join(bot, nil, { room = room_jid; password = password });
	end
end

local function handle_bot_message(bot, stanza)
	local from = stanza.attr.from or "";
	local from_node, from_host, from_res = jid.split(from);
	local stype = stanza.attr.type or "normal";

	if stype == "error" then
		local err = stanza:get_child("error");
		local condition, text;
		if err then
			local cond = err.tags[1];
			condition = cond and cond.name;
			text = err:get_child_text("text", "urn:ietf:params:xml:ns:xmpp-stanzas");
		end
		emit(bot, {
			type = "error";
			from = from;
			stanza_id = stanza.attr.id;
			condition = condition;
			text = text;
		});
		return true;
	end

	-- Mediated invite: message from a room JID carrying muc#user invite.
	if config.muc_hosts:contains(from_host or "") and from_node and not from_res then
		local x = stanza:child_with_name("x");
		local invite = x and x.attr.xmlns == xmlns_muc_user and x:child_with_name("invite");
		if invite then
			local room_jid = from;
			local password_tag = x:child_with_name("password");
			local password = password_tag and password_tag:get_text() or nil;
			emit(bot, {
				type = "invite";
				room = room_jid;
				from = invite.attr.from;
				reason = invite:get_child_text("reason");
				mediated = true;
				password = password;
			});
			auto_accept_invite(bot, invite.attr.from, room_jid, password);
			return true;
		end
	end

	-- Direct invite (XEP-0249): x jabber:x:conference from a user.
	local conf = stanza:get_child("x", xmlns_conference);
	if conf and conf.attr.jid then
		emit(bot, {
			type = "invite";
			room = conf.attr.jid;
			from = from;
			reason = conf:get_text() ~= "" and conf:get_text() or nil;
			password = conf.attr.password;
			mediated = false;
		});
		auto_accept_invite(bot, from, conf.attr.jid, conf.attr.password);
		return true;
	end

	local body = stanza:get_child_text("body");
	local subject = stanza:get_child_text("subject");
	local thread = stanza:get_child_text("thread");
	local stanza_id = stanza:get_child_attr("stanza-id", "urn:xmpp:sid:0", "id") or stanza.attr.id;
	local origin_id = stanza:get_child_attr("origin-id", "urn:xmpp:sid:0", "id");
	local replace = stanza:get_child_attr("replace", "urn:xmpp:message-correct:0", "id");
	local reply = stanza:get_child("reply", xmlns_reply);
	local reactions = stanza:get_child("reactions", xmlns_reactions);
	local omemo = omemo_info(stanza);
	local eme = stanza:get_child("encryption", xmlns_eme);
	local eme_ns = eme and eme.attr.namespace or nil;

	local in_muc = config.muc_hosts:contains(from_host or "");
	local room_jid = in_muc and from_node and (from_node .. "@" .. from_host) or nil;
	local info = room_jid and bot.rooms[room_jid];

	if stype == "groupchat" then
		if not (bot.events and bot.events.groupchat ~= false) then return true; end
		if not room_jid then return true; end -- groupchat from non-MUC host: drop
		local self_echo = info and from_res == info.nick;
		if self_echo and not (bot.events and bot.events.echo_self) then
			return true;
		end
		if bot.events and bot.events.mentions_only and not self_echo then
			local nick = info and info.nick or bot.name;
			if not body or not body:lower():find(nick:lower(), 1, true) then
				return true;
			end
		end
		emit(bot, {
			type = "message";
			kind = "groupchat";
			room = room_jid;
			nick = from_res;
			from = from;
			real_jid = from_res and room_real_jid(room_jid, from_res) or nil;
			body = body;
			subject = subject;
			thread = thread;
			stanza_id = stanza_id;
			origin_id = origin_id;
			replace_id = replace;
			reply_to = reply and { to = reply.attr.to; id = reply.attr.id } or nil;
			stamp = delay_stamp(stanza);
			self = self_echo or nil;
			encrypted = omemo ~= nil or nil;
			encryption = eme_ns;
			omemo = omemo;
		});
		return true;
	end

	-- PM: chat message from a room occupant JID.
	if in_muc and from_res and room_jid then
		if bot.events and bot.events.pm == false then return true; end
		if not env.sender_limit:take("dmfrom:" .. bot.name .. ":" .. from) then
			bot.stats.dms_dropped = (bot.stats.dms_dropped or 0) + 1;
			return true;
		end
		emit(bot, {
			type = "message";
			kind = "pm";
			room = room_jid;
			nick = from_res;
			from = from;
			real_jid = room_real_jid(room_jid, from_res);
			body = body;
			thread = thread;
			stanza_id = stanza_id;
			replace_id = replace;
			stamp = delay_stamp(stanza);
			encrypted = omemo ~= nil or nil;
			encryption = eme_ns;
			omemo = omemo;
		});
		return true;
	end

	-- Direct message to the bot JID.
	if bot.events and bot.events.dm == false then return true; end
	if not env.sender_limit:take("dmfrom:" .. bot.name .. ":" .. from) then
		bot.stats.dms_dropped = (bot.stats.dms_dropped or 0) + 1;
		return true;
	end
	if reactions then
		local emojis = {};
		for r in reactions:children() do
			if r.name == "reaction" then
				emojis[#emojis + 1] = r:get_text();
			end
		end
		emit(bot, {
			type = "reaction";
			kind = "dm";
			from = from;
			message_id = reactions.attr.id;
			emojis = emojis;
		});
		return true;
	end
	local chat_state;
	for state_name in pairs(valid_chat_states) do
		if stanza:get_child(state_name, xmlns_chatstates) then
			chat_state = state_name;
			break;
		end
	end
	emit(bot, {
		type = "message";
		kind = "dm";
		from = from;
		body = body;
		thread = thread;
		stanza_id = stanza_id;
		origin_id = origin_id;
		replace_id = replace;
		chat_state = chat_state;
		reply_to = reply and { to = reply.attr.to; id = reply.attr.id } or nil;
		stamp = delay_stamp(stanza);
		encrypted = omemo ~= nil or nil;
		encryption = eme_ns;
		omemo = omemo;
	});
	return true;
end

local function handle_bot_presence(bot, stanza)
	local from = stanza.attr.from or "";
	local from_node, from_host, from_res = jid.split(from);
	local ptype = stanza.attr.type;

	-- Presence from a MUC room JID.
	if config.muc_hosts:contains(from_host or "") and from_node then
		local room_jid = from_node .. "@" .. from_host;
		local info = bot.rooms[room_jid];
		local x = stanza:get_child("x", xmlns_muc_user);
		local codes = statuses_of(x);
		local item = x and x:child_with_name("item");
		local pending = pending_joins[bot.name .. " " .. room_jid];
		local is_self = codes[110]
			or (info and from_res ~= nil and from_res == info.nick)
			or (pending and from_res ~= nil and from_res == pending.nick);

		if ptype == "error" then
			local err = stanza:get_child("error");
			local cond = err and err.tags[1] and err.tags[1].name;
			pending_joins[bot.name .. " " .. room_jid] = nil;
			if info then
				info.joined = false;
				registry.save(bot);
			end
			emit(bot, { type = "join_failed"; room = room_jid; condition = cond });
			return true;
		end

		if is_self then
			if ptype == "unavailable" then
				if env.is_shutting_down() then
					-- Our own shutdown leave; keep the room record so the
					-- bot rejoins on next start.
					pending_joins[bot.name .. " " .. room_jid] = nil;
					return true;
				end
				local reason = item and item:get_child_text("reason");
				local actor = item and item:get_child("actor");
				local actor_jid = actor and actor.attr.jid or nil;
				local etype = "leave";
				if codes[307] then etype = "kicked";
				elseif codes[301] then etype = "banned";
				elseif codes[321] or codes[322] then etype = "removed";
				end
				if codes[303] and item and item.attr.nick then
					-- Nick change: keep the room, update the nick.
					if info then
						info.nick = item.attr.nick;
						registry.save(bot);
					end
					emit(bot, { type = "nick_changed"; room = room_jid; nick = item.attr.nick });
					return true;
				end
				pending_joins[bot.name .. " " .. room_jid] = nil;
				if info then
					bot.rooms[room_jid] = nil;
					registry.save(bot);
				end
				emit(bot, { type = etype; room = room_jid; reason = reason; actor = actor_jid });
				return true;
			end
			-- Available self-presence: join confirmed.
			pending_joins[bot.name .. " " .. room_jid] = nil;
			if not info then
				info = { nick = from_res or bot.name; joined = true };
				bot.rooms[room_jid] = info;
			else
				info.joined = true;
				info.nick = from_res or info.nick;
			end
			registry.save(bot);
			if codes[201] then
				-- The bot just created this room. New rooms stay locked
				-- until the creator submits a config form; accept the
				-- defaults so others can join.
				bot_send(bot, st.iq({ to = room_jid; type = "set"; id = id.medium() })
					:tag("query", { xmlns = xmlns_muc_owner })
						:tag("x", { xmlns = xmlns_data; type = "submit" }));
			end
			emit(bot, {
				type = "join";
				room = room_jid;
				nick = info.nick;
				created = codes[201] or nil;
				renamed = codes[210] or nil;
			});
			return true;
		end

		-- Occupant presence in a joined room.
		if not info or not info.joined then return true; end
		if bot.events and bot.events.presence == false then return true; end
		emit(bot, {
			type = "presence";
			room = room_jid;
			nick = from_res;
			available = ptype ~= "unavailable";
			role = item and item.attr.role or nil;
			affiliation = item and item.attr.affiliation or nil;
			real_jid = item and item.attr.jid or nil;
			show = stanza:get_child_text("show");
			status = stanza:get_child_text("status");
		});
		return true;
	end

	-- Presence from a user JID: subscription management.
	if ptype == "subscribe" then
		if bot.events and bot.events.subscribe_requests ~= false then
			emit(bot, { type = "subscribe_request"; from = jid.bare(from) or from });
		end
		bot_send(bot, st.presence({ to = jid.bare(from) or from; type = "unsubscribed" }));
		return true;
	end
	if ptype == "subscribed" or ptype == "unsubscribe" or ptype == "unsubscribed" then
		return true; -- bots have no roster; acknowledge silently
	end
	return true;
end

-- Shared dispatcher for all stanza kinds addressed to this component.
local function bot_for(to)
	local node, host = jid.split(to or "");
	if host ~= module.host then return nil; end
	if not node then return nil, true; end -- bare host
	return registry.get(node), false;
end

local function on_message(event)
	local stanza = event.stanza;
	module:log("debug", "message in: from=%s to=%s type=%s", stanza.attr.from, stanza.attr.to, stanza.attr.type);
	local bot, is_host = bot_for(stanza.attr.to);
	if is_host then return true; end -- service-level message: drop
	if not bot then
		if stanza.attr.type ~= "error" then
			module:send(st.error_reply(stanza, "cancel", "service-unavailable", "No such bot"));
		end
		return true;
	end
	return handle_bot_message(bot, stanza);
end

local function on_presence(event)
	local stanza = event.stanza;
	module:log("debug", "presence in: from=%s to=%s type=%s", stanza.attr.from, stanza.attr.to, stanza.attr.type);
	local bot, is_host = bot_for(stanza.attr.to);
	if is_host then return true; end
	if not bot then
		if stanza.attr.type == "subscribe" then
			module:send(st.presence({
				to = stanza.attr.from;
				from = stanza.attr.to;
				type = "unsubscribed";
			}));
		end
		return true;
	end
	return handle_bot_presence(bot, stanza);
end

local function disco_info_reply(stanza, name)
	local reply = st.reply(stanza)
		:query(xmlns_disco_info)
		:tag("identity", { category = "automation"; type = "bot"; name = name }):up();
	for _, feature in ipairs(bot_features) do
		reply:tag("feature", { var = feature }):up();
	end
	return reply;
end

local function on_iq(event)
	local stanza = event.stanza;
	local stype = stanza.attr.type;
	if stype == "result" or stype == "error" then
		return true; -- consume replies; we issue no tracked IQs
	end
	local bot, is_host = bot_for(stanza.attr.to);
	local child = stanza.tags[1];
	local xmlns = child and child.attr.xmlns;
	local cname = child and child.name;

	if stype == "get" then
		if xmlns == xmlns_ping and cname == "ping" then
			if is_host or bot then
				module:send(st.reply(stanza));
			else
				module:send(st.error_reply(stanza, "cancel", "service-unavailable", "No such bot"));
			end
			return true;
		end
		if xmlns == xmlns_disco_info and cname == "query" then
			if is_host or not bot then
				local reply = st.reply(stanza):query(xmlns_disco_info)
					:tag("identity", { category = "component"; type = "generic"; name = "SnikketX bots" }):up()
					:tag("feature", { var = xmlns_disco_info }):up();
				module:send(reply);
			else
				module:send(disco_info_reply(stanza, bot.label or bot.name));
			end
			return true;
		end
		if xmlns == xmlns_disco_items and cname == "query" then
			module:send(st.reply(stanza):query(xmlns_disco_items));
			return true;
		end
		if xmlns == xmlns_version and cname == "query" then
			local reply = st.reply(stanza):tag("query", { xmlns = xmlns_version });
			if is_host or not bot then
				reply:tag("name"):text("SnikketX bots"):up();
				reply:tag("version"):text("1"):up();
			else
				reply:tag("name"):text(bot.label or bot.name):up();
				reply:tag("version"):text("snikketx-bots/1"):up();
			end
			module:send(reply);
			return true;
		end
	end
	module:send(st.error_reply(stanza, "cancel", "service-unavailable"));
	return true;
end

for _, ev in ipairs({ "message/bare"; "message/full"; "message/host" }) do
	module:hook(ev, on_message);
end
for _, ev in ipairs({ "presence/bare"; "presence/full"; "presence/host" }) do
	module:hook(ev, on_presence);
end
for _, ev in ipairs({ "iq/bare"; "iq/full"; "iq/host" }) do
	module:hook(ev, on_iq, -5);
end

end
