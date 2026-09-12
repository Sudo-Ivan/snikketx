-- Expose Prosody audit events to the SnikketX web portal over HTTP.
--luacheck: ignore 113/module 143/module

module:depends("http");
module:depends("audit");

local json = require "prosody.util.json";
local api_util = module:depends("snikketx_api_util");

local list = api_util.list;
local check_credentials = api_util.check_credentials;
local session_is_admin = api_util.session_is_admin;
local decode_query = api_util.decode_query;
local www_authenticate_header = api_util.www_authenticate_header(module.host, module.name);

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
		return json.encode({ events = list({}), error = "audit store unavailable" });
	end

	local results, err = store:find(nil, {
		limit = limit * 2;
		reverse = true;
	});
	if not results then
		module:log("warn", "audit query failed: %s", tostring(err));
		event.response.headers["Content-Type"] = "application/json";
		return json.encode({ events = list({}), error = tostring(err) });
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
	return json.encode({ events = list(events), host = module.host });
end

module:provides("http", {
	route = {
		["GET /events"] = handle_list;
		["GET /"] = handle_list;
	};
});

module:log("info", "SnikketX audit API listening for admin bearer tokens");
