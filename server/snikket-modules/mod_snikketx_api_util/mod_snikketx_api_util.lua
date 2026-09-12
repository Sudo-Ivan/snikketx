-- Shared helpers for the SnikketX HTTP API modules. Not meant to be enabled
-- directly; consumers fetch this module's environment via
-- module:depends("snikketx_api_util").
--luacheck: ignore 111 113/module 143/module

local array = require "prosody.util.array";
local usermanager = require "prosody.core.usermanager";
local tokens = module:depends("tokenauth");

-- Empty Lua tables encode as JSON objects. Wrap lists so Go gets [].
function list(t)
	return array(t or {});
end

function www_authenticate_header(host, name)
	return ("Bearer realm=%q"):format(host.."/"..name);
end

function check_credentials(request)
	local auth_type, auth_data = string.match(request.headers.authorization or "", "^(%S+)%s(.+)$");
	if not (auth_type and auth_data) then
		return false;
	end
	if auth_type == "Bearer" then
		return tokens.get_token_session(auth_data);
	end
	return nil;
end

function grant_has_admin(grants)
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

function session_is_admin(session)
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

function decode_query(query)
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

function retention_days()
	return tonumber(os.getenv("SNIKKET_RETENTION_DAYS")) or 7;
end
