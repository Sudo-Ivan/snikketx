-- Expose group chat (MUC) management to the SnikketX web portal.
--luacheck: ignore 113/module 143/module

module:depends("http");

local json = require "util.json";
local array = require "util.array";
local jid = require "util.jid";
local id = require "util.id";
local usermanager = require "core.usermanager";
local tokens = module:depends("tokenauth");

local function list(t)
	return array(t or {});
end

local muc_host = module:get_option_string("groups_muc_host") or ("groups." .. module.host);
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

local function require_admin(event)
	local session = check_credentials(event.request);
	if not session then
		event.response.headers.authorization = www_authenticate_header;
		return nil, 401;
	end
	if not session_is_admin(session) then
		return nil, 403;
	end
	return session;
end

local function muc_module()
	local host_session = prosody.hosts[muc_host];
	if not host_session then
		module:log("warn", "MUC host %s is not loaded", muc_host);
		return nil;
	end
	if host_session.modules and host_session.modules.muc then
		return host_session.modules.muc;
	end
	if host_session.muc then
		return host_session.muc;
	end
	local ok, muc = pcall(function ()
		return module:context(muc_host):depends("muc");
	end);
	if ok and muc then
		return muc;
	end
	module:log("warn", "MUC module not found on %s", muc_host);
	return nil;
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

local function count_occupants(room)
	local n = 0;
	if room.each_occupant then
		for _ in room:each_occupant() do
			n = n + 1;
		end
	end
	return n;
end

local function serialize_room(room)
	local localpart = jid.node(room.jid);
	local occupants = count_occupants(room);
	return {
		jid = room.jid;
		localpart = localpart;
		name = room.get_name and room:get_name() or localpart;
		description = room.get_description and room:get_description() or "";
		occupants = occupants;
		persistent = not not (room.get_persistent and room:get_persistent());
		public = not not (room.get_public and room:get_public());
		members_only = not not (room.get_members_only and room:get_members_only());
		moderated = not not (room.get_moderated and room:get_moderated());
		password_protected = not not (room.get_password and room:get_password() and room:get_password() ~= "");
	};
end

local function serialize_occupants(room)
	local out = {};
	if not room.each_occupant then
		return out;
	end
	for nick_jid, occupant in room:each_occupant() do
		out[#out+1] = {
			nick = jid.resource(nick_jid);
			jid = occupant and occupant.bare_jid or nil;
			role = occupant and occupant.role or nil;
			affiliation = occupant and occupant.affiliation or nil;
		};
	end
	return out;
end

local function handle_list(event)
	local _, code = require_admin(event);
	if code then
		return code;
	end
	local muc = muc_module();
	if not muc then
		event.response.headers["Content-Type"] = "application/json";
		return json.encode({ rooms = list({}), error = "muc component unavailable", host = muc_host });
	end

	local params = decode_query(event.request.url.query);
	local search = params.q or params.query;
	if search then
		search = search:lower();
	end

	local rooms = {};
	for room in muc.each_room() do
		local row = serialize_room(room);
		local hay = table.concat({
			tostring(row.jid or "");
			tostring(row.name or "");
			tostring(row.description or "");
			tostring(row.localpart or "");
		}, " "):lower();
		if not search or hay:find(search, 1, true) then
			rooms[#rooms+1] = row;
		end
	end

	table.sort(rooms, function (a, b)
		return tostring(a.name):lower() < tostring(b.name):lower();
	end);

	event.response.headers["Content-Type"] = "application/json";
	return json.encode({ rooms = list(rooms), host = muc_host });
end

local function room_from_path(event)
	local muc = muc_module();
	if not muc then
		return nil, nil, 503;
	end
	local path = event.request.path or "";
	local node = path:match("/rooms/([^/]+)/?$") or path:match("/rooms/([^/]+)/occupants/?$");
	if not node or node == "" then
		return nil, nil, 404;
	end
	node = node:gsub("%%(%x%x)", function (h)
		return string.char(tonumber(h, 16));
	end);
	local room_jid = node .. "@" .. muc_host;
	local room = muc.get_room_from_jid(room_jid);
	if not room then
		return nil, nil, 404;
	end
	return muc, room, nil;
end

local function handle_get(event)
	local _, code = require_admin(event);
	if code then
		return code;
	end
	local _, room, err = room_from_path(event);
	if err then
		return err;
	end
	local payload = serialize_room(room);
	payload.occupants_list = list(serialize_occupants(room));
	event.response.headers["Content-Type"] = "application/json";
	return json.encode(payload);
end

local function handle_create(event)
	local _, code = require_admin(event);
	if code then
		return code;
	end
	local muc = muc_module();
	if not muc then
		return 503;
	end
	local body = event.request.body;
	if not body or #body == 0 then
		return 400;
	end
	local ok, payload = pcall(json.decode, body);
	if not ok or type(payload) ~= "table" then
		return 400;
	end
	local name = payload.name and tostring(payload.name):match("^%s*(.-)%s*$") or "";
	if name == "" then
		return 400;
	end
	local localpart = payload.localpart and tostring(payload.localpart):match("^%s*(.-)%s*$") or "";
	if localpart == "" then
		localpart = id.short();
	end
	local room_jid = localpart .. "@" .. muc_host;
	if muc.get_room_from_jid(room_jid) then
		return 409;
	end
	local room = muc.create_room(room_jid);
	if not room then
		return 500;
	end
	if room.set_name then
		room:set_name(name);
	end
	if payload.description and room.set_description then
		room:set_description(tostring(payload.description));
	end
	if payload.public ~= nil and room.set_public then
		room:set_public(not not payload.public);
	end
	if payload.persistent ~= nil and room.set_persistent then
		room:set_persistent(not not payload.persistent);
	else
		if room.set_persistent then
			room:set_persistent(true);
		end
	end
	event.response.headers["Content-Type"] = "application/json";
	return json.encode(serialize_room(room));
end

local function handle_destroy(event)
	local _, code = require_admin(event);
	if code then
		return code;
	end
	local _, room, err = room_from_path(event);
	if err then
		return err;
	end
	if room.destroy then
		room:destroy();
	elseif room.clear then
		room:clear();
	else
		return 500;
	end
	return 204;
end

module:provides("http", {
	route = {
		["GET /rooms"] = handle_list;
		["GET /"] = handle_list;
		["POST /rooms"] = handle_create;
		["GET /rooms/*"] = handle_get;
		["DELETE /rooms/*"] = handle_destroy;
	};
});

module:log("info", "SnikketX MUC API ready for %s", muc_host);
