-- XEP-0424 tombstoning for the 1:1 message archive.
--
-- When a message is retracted, replace the archived copy with a tombstone
-- that keeps the archive sequence stable (key, timestamp, with) but drops
-- the content. Without this, retracted plaintext messages remain fully
-- retrievable via MAM until they expire.
--
-- Group chats are not handled here: mod_muc_moderation implements
-- XEP-0425 tombstones for MUC.

local st = require "prosody.util.stanza";
local jid_bare = require "prosody.util.jid".bare;
local jid_split = require "prosody.util.jid".split;
local jid_prepped_split = require "prosody.util.jid".prepped_split;
local datetime = require "prosody.util.datetime";
local time_now = require "prosody.util.time".now;

local xmlns_fasten = "urn:xmpp:fasten:0";
local xmlns_retract = "urn:xmpp:message-retract:1";
local xmlns_retract_legacy = "urn:xmpp:message-retract:0";
local xmlns_sid = "urn:xmpp:sid:0";

local archive_store = module:get_option_string("archive_store", "archive");
local archive = module:open_store(archive_store, "archive");

local function get_retracted_id(stanza)
	local apply_to = stanza:get_child("apply-to", xmlns_fasten);
	if apply_to then
		local retract = apply_to:get_child("retract", xmlns_retract)
			or apply_to:get_child("retract", xmlns_retract_legacy);
		if retract then
			return apply_to.attr.id;
		end
	end
	local retract = stanza:get_child("retract", xmlns_retract)
		or stanza:get_child("retract", xmlns_retract_legacy);
	if retract then
		return retract.attr.id;
	end
	return nil;
end

local function stanza_matches(stanza, message_id)
	if stanza.attr.id == message_id then
		return true;
	end
	for tag in stanza:childtags("origin-id", xmlns_sid) do
		if tag.attr.id == message_id then
			return true;
		end
	end
	return false;
end

local function make_tombstone(original, retractor)
	local tombstone = st.stanza("message", {
		type = original.attr.type;
		id = original.attr.id;
		from = original.attr.from;
		to = original.attr.to;
	});
	for tag in original:childtags() do
		if (tag.name == "stanza-id" or tag.name == "origin-id") and tag.attr.xmlns == xmlns_sid then
			tombstone:add_child(tag);
		end
	end
	tombstone:tag("retracted", {
		xmlns = xmlns_retract;
		stamp = datetime.datetime(time_now());
		by = retractor;
	});
	return tombstone;
end

local function tombstone_message(username, with, message_id, retractor)
	local iter, err = archive:find(username, { with = with });
	if not iter then
		if err and err ~= "item-not-found" then
			module:log("warn", "Could not search %s's archive for retraction of %s: %s", username, message_id, err);
		end
		return;
	end
	for key, stored, when, item_with in iter do
		if stanza_matches(stored, message_id) then
			if jid_bare(stored.attr.from) ~= retractor then
				module:log("warn", "Ignoring retraction of %s by %s: message was sent by %s",
					message_id, retractor, stored.attr.from);
				return;
			end
			local tombstone = make_tombstone(stored, retractor);
			if archive.set then
				local ok, set_err = archive:set(username, key, tombstone, when, item_with);
				if not ok then
					module:log("error", "Could not tombstone archived message %s for %s: %s", key, username, set_err);
				else
					module:log("debug", "Tombstoned archived message %s for %s", key, username);
				end
			else
				local deleted = archive:delete(username, { key = key });
				local ok, append_err = archive:append(username, key, tombstone, when, item_with);
				if not (deleted and ok) then
					module:log("error", "Could not tombstone archived message %s for %s: %s", key, username, append_err);
				else
					module:log("debug", "Tombstoned archived message %s for %s", key, username);
				end
			end
			return;
		end
	end
	module:log("debug", "Retracted message %s not found in %s's archive", message_id, username);
end

local function handle_retraction(event, to_local)
	local stanza = event.stanza;
	if stanza.attr.type == "groupchat" then
		return;
	end
	local message_id = get_retracted_id(stanza);
	if not message_id then
		return;
	end
	local from_bare = jid_bare(stanza.attr.from);
	local to_bare = jid_bare(stanza.attr.to or stanza.attr.from);
	if to_local then
		-- Inbound retraction: the archived copy lives in the recipient's
		-- archive under 'with = sender'.
		local username, host = jid_prepped_split(stanza.attr.to);
		if host == module.host and username then
			tombstone_message(username, from_bare, message_id, from_bare);
		end
	else
		-- Outbound retraction from a local client: the sent copy lives in
		-- the sender's archive under 'with = recipient'.
		local username, host = jid_split(stanza.attr.from);
		if username then
			tombstone_message(username, to_bare, message_id, from_bare);
		end
	end
end

local function c2s_handler(event)
	return handle_retraction(event, false);
end

local function inbound_handler(event)
	return handle_retraction(event, true);
end

module:hook("pre-message/bare", c2s_handler);
module:hook("pre-message/full", c2s_handler);
module:hook("message/bare", inbound_handler);
module:hook("message/full", inbound_handler);

module:hook("account-disco-info", function (event)
	event.reply:tag("feature", { var = xmlns_retract.."#tombstones" }):up();
end);
