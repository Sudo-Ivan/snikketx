-- Per-user daily quota on contact invite creation.
--
-- Limits how often a local user may run the "Create new contact invite"
-- ad-hoc command (urn:xmpp:invite#invite, XEP-0401) per UTC day.
-- Accounts with an admin or operator role are exempt.

local st = require "prosody.util.stanza";
local jid_split = require "prosody.util.jid".split;
local usermanager = require "prosody.core.usermanager";

local invites_daily_limit = module:get_option_integer("invites_daily_limit", 20);

local quota_store = module:open_store("invites_quota");

local exempt_roles = {
	["prosody:admin"] = true;
	["prosody:operator"] = true;
};

local function is_exempt(username)
	local role = usermanager.get_user_role(username, module.host);
	return role and exempt_roles[role.name] or false;
end

local function utc_day()
	return os.date("!%Y-%m-%d");
end

-- mod_adhoc handles this IQ at priority 500, so run earlier to enforce
-- the quota before the invite is created.
module:hook("iq-set/host/http://jabber.org/protocol/commands:command", function (event)
	local origin, stanza = event.origin, event.stanza;
	local command = stanza.tags[1];
	if not command or command.attr.node ~= "urn:xmpp:invite#invite" then
		return;
	end
	local action = command.attr.action;
	if action and action ~= "execute" then
		return;
	end
	local username, host = jid_split(stanza.attr.from);
	if not username or host ~= module.host then
		return;
	end
	if is_exempt(username) then
		return;
	end

	local today = utc_day();
	local record = quota_store:get(username);
	local count = (record and record.day == today) and record.count or 0;
	if count >= invites_daily_limit then
		module:log("info", "Rejecting contact invite from %s: daily limit of %d reached", stanza.attr.from, invites_daily_limit);
		origin.send(st.error_reply(stanza, "wait", "resource-constraint",
			("Daily invite limit of %d reached, try again tomorrow"):format(invites_daily_limit)));
		return true;
	end
	local ok, err = quota_store:set(username, { day = today; count = count + 1 });
	if not ok then
		module:log("warn", "Failed to update invite quota for %s: %s", stanza.attr.from, err);
	end
end, 1000);
