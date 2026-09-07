-- Expose Prosody audit events to the SnikketX web portal over HTTP.
--luacheck: ignore 113/module 143/module

module:depends("http");
module:depends("audit");

local json = require "util.json";
local usermanager = require "core.usermanager";
local tokens = module:depends("tokenauth");

local www_authenticate_header = ("Bearer realm=%q"):format(module.host.."/"..module.name);

local function check_credentials(request)
	local auth_type, auth_data = string.match(request.headers.authorization or "", "^(%S+)%s(.+)$");
	if not (auth_type and auth_data) then
		return false;
	end
	if auth_type == "Bearer" then
		return tokens.get_token_session(auth_data);
	end
	return nil;
end

local function grant_has_admin(grants)
	if type(grants) == "string" then
		return grants:find("prosody:admin", 1, true) ~= nil;
	end
	if type(grants) ~= "table" then
		return false;
	end
	if grants["prosody:admin"] or grants["prosody:operator"] then
		return true;
	end
	for key, value in pairs(grants) do
		if key == "prosody:admin" and value then
			return true;
		end
		if value == "prosody:admin" then
			return true;
		end
	end
	return false;
end

local function session_is_admin(session)
	if not session then
		return false;
	end
	if grant_has_admin(session.roles) or grant_has_admin(session.grants) or grant_has_admin(session.scopes) or grant_has_admin(session.scope) then
		return true;
	end
	if session.role then
		local name = session.role;
		if type(session.role) == "table" then
			name = session.role.name or session.role.role;
		end
		if name == "prosody:admin" or name == "prosody:operator" then
			return true;
		end
	end
	if session.token_info and (
		grant_has_admin(session.token_info.grants)
		or grant_has_admin(session.token_info.scopes)
		or grant_has_admin(session.token_info.scope)
	) then
		return true;
	end
	local username = session.username;
	local host = session.host or module.host;
	if not username and session.token_info then
		username = session.token_info.username;
		host = session.token_info.host or host;
	end
	if not username then
		return false;
	end
	if usermanager.user_is_admin then
		return usermanager.user_is_admin(username, host);
	end
	return usermanager.is_admin(username.."@"..host);
end

local function serialize_event(id, item, when, with)
	local source = nil;
	local event_type = "event";
	local username = with;
	local ip = nil;
	local detail = nil;
	if item then
		source = item.source or item.source_type;
		event_type = item.event_type or item.type or item.action or event_type;
		username = item.username or with;
		ip = item.ip or item.source_ip;
		detail = item.message or item.summary or item.detail;
		if type(detail) == "table" then
			detail = json.encode(detail);
		end
	end
	return {
		id = id;
		when = when and math.floor(when) or nil;
		source = source or "prosody";
		actor = username;
		action = event_type;
		target = with;
		detail = detail;
		ip = ip;
	};
end

local function decode_query(query)
	local out = {};
	if not query or query == "" then
		return out;
	end
	for key, value in query:gmatch("([^&=]+)=([^&=]*)") do
		key = key:gsub("%+", " "):gsub("%%(%x%x)", function (h)
			return string.char(tonumber(h, 16));
		end);
		value = value:gsub("%+", " "):gsub("%%(%x%x)", function (h)
			return string.char(tonumber(h, 16));
		end);
		out[key] = value;
	end
	return out;
end

local function handle_list(event)
	local session = check_credentials(event.request);
	if not session then
		event.response.headers.authorization = www_authenticate_header;
		return 401;
	end
	if not session_is_admin(session) then
		return 403;
	end

	local params = decode_query(event.request.url.query);
	local limit = math.min(500, math.max(1, tonumber(params.limit) or 100));
	local search = params.q or params.query;
	if search then
		search = search:lower();
	end

	local store = module:open_store("audit", "archive");
	if not store then
		event.response.headers["Content-Type"] = "application/json";
		return json.encode({ events = {}, error = "audit store unavailable" });
	end

	local results, err = store:find(nil, {
		limit = limit * 2;
		reverse = true;
	});
	if not results then
		module:log("warn", "audit query failed: %s", tostring(err));
		event.response.headers["Content-Type"] = "application/json";
		return json.encode({ events = {}, error = tostring(err) });
	end

	local events = {};
	for id, item, when, with in results do
		local row = serialize_event(id, item, when, with);
		local hay = table.concat({
			tostring(row.action or "");
			tostring(row.actor or "");
			tostring(row.target or "");
			tostring(row.detail or "");
			tostring(row.source or "");
		}, " "):lower();
		if not search or hay:find(search, 1, true) then
			events[#events+1] = row;
			if #events >= limit then
				break;
			end
		end
	end

	event.response.headers["Content-Type"] = "application/json";
	return json.encode({ events = events, host = module.host });
end

module:provides("http", {
	route = {
		["GET /events"] = handle_list;
		["GET /"] = handle_list;
	};
});

module:log("info", "SnikketX audit API listening for admin bearer tokens");
