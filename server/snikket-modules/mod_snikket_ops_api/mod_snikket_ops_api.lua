-- Operator APIs for the SnikketX web portal: clients, uploads, invites stats,
-- archives, updates and account export packages.
--luacheck: ignore 113/module 143/module

module:depends("http");

local json = require "util.json";
local array = require "util.array";
local usermanager = require "core.usermanager";
local tokens = module:depends("tokenauth");

-- Empty Lua tables encode as JSON objects. Wrap lists so Go gets [].
local function list(t)
	return array(t or {});
end

local www_authenticate_header = ("Bearer realm=%q"):format(module.host.."/"..module.name);
local share_host = "share." .. module.host;
local groups_host = module:get_option_string("groups_muc_host") or ("groups." .. module.host);

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

local function session_username(session)
	if not session then
		return nil;
	end
	local username = session.username;
	if not username and session.token_info then
		username = session.token_info.username;
	end
	return username;
end

local function require_user(event)
	local session = check_credentials(event.request);
	if not session then
		event.response.headers.authorization = www_authenticate_header;
		return nil, 401;
	end
	local username = session_username(session);
	if not username or username == "" then
		return nil, 403;
	end
	return session, nil, username;
end

local function json_ok(event, payload)
	event.response.headers["Content-Type"] = "application/json";
	return json.encode(payload);
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

local function url_decode(s)
	return (tostring(s):gsub("%%(%x%x)", function (h)
		return string.char(tonumber(h, 16));
	end));
end

local function slice(t, first, last)
	local out = {};
	first = first or 1;
	last = math.min(last or #t, #t);
	for i = first, last do
		out[#out+1] = t[i];
	end
	return out;
end

local clients_store = module:open_store("clients");
local push_store = module:open_store("cloud_notify");
local activity_store = module:open_store("account_activity");
local invites_tracking = module:open_store("invites_tracking");
local invites_bootstrap = module:open_store("invites_bootstrap");
local update_store = module:open_store("update_notifications", "map");
local archive_store = module:open_store("archive", "archive");
local offline_store = module:open_store("offline", "archive");
local roster_store = module:open_store("roster");
local vcard_store = module:open_store("vcard");

local function list_usernames()
	local users = {};
	if usermanager.users then
		for username in usermanager.users(module.host) do
			users[#users+1] = username;
		end
	end
	return users;
end

local function client_rows_for_user(username)
	local rows = {};
	local data = clients_store:get(username) or {};
	local push = push_store:get(username) or {};
	local activity = activity_store:get(username) or {};
	local last_active = activity.timestamp or activity.last_active or activity.when;

	for client_id, info in pairs(data) do
		if type(info) == "table" then
			local push_info = push[client_id];
			rows[#rows+1] = {
				user = username;
				client_id = tostring(client_id);
				name = info.name or info.client_name or info.software or nil;
				user_agent = info.user_agent or info.ua or nil;
				resource = info.resource or nil;
				ip = info.ip or info.full_ip or nil;
				first_seen = info.first_seen or info.created or nil;
				last_seen = info.last_seen or info.updated or last_active or nil;
				has_push = push_info ~= nil;
				push_service = type(push_info) == "table" and (push_info.service or push_info.endpoint) or nil;
			};
		end
	end
	return rows;
end

local function handle_clients(event)
	local _, code = require_admin(event);
	if code then return code; end
	local params = decode_query(event.request.url.query);
	local filter_user = params.user;
	local clients = {};
	for _, username in ipairs(list_usernames()) do
		if not filter_user or filter_user == username then
			for _, row in ipairs(client_rows_for_user(username)) do
				clients[#clients+1] = row;
			end
		end
	end
	table.sort(clients, function (a, b)
		return tostring(a.last_seen or 0) > tostring(b.last_seen or 0);
	end);
	return json_ok(event, { clients = list(clients), count = #clients });
end

local function revoke_client_for_user(username, client_id)
	local data = clients_store:get(username) or {};
	if data[client_id] ~= nil then
		data[client_id] = nil;
		clients_store:set(username, data);
	end
	local push = push_store:get(username) or {};
	if push[client_id] ~= nil then
		push[client_id] = nil;
		push_store:set(username, push);
	end
	local sessions = prosody.hosts[module.host] and prosody.hosts[module.host].sessions;
	if sessions and sessions[username] and sessions[username].sessions then
		for resource, session in pairs(sessions[username].sessions) do
			if session.client_id == client_id or resource == client_id then
				if session.close then
					session:close();
				end
			end
		end
	end
end

local function handle_me_clients(event)
	local _, code, username = require_user(event);
	if code then return code; end
	local clients = client_rows_for_user(username);
	table.sort(clients, function (a, b)
		return tostring(a.last_seen or 0) > tostring(b.last_seen or 0);
	end);
	return json_ok(event, { clients = list(clients), count = #clients, user = username });
end

local function handle_me_revoke_client(event)
	local _, code, username = require_user(event);
	if code then return code; end
	local path = event.request.path or "";
	local client_id = path:match("/me/clients/([^/]+)/?$");
	if not client_id or client_id == "" then
		return 404;
	end
	client_id = url_decode(client_id);
	revoke_client_for_user(username, client_id);
	return 204;
end

local function handle_me_revoke_all(event)
	local _, code, username = require_user(event);
	if code then return code; end
	local clients = client_rows_for_user(username);
	for _, row in ipairs(clients) do
		if row.client_id then
			revoke_client_for_user(username, row.client_id);
		end
	end
	clients_store:set(username, {});
	push_store:set(username, {});
	local sessions = prosody.hosts[module.host] and prosody.hosts[module.host].sessions;
	if sessions and sessions[username] and sessions[username].sessions then
		for _, session in pairs(sessions[username].sessions) do
			if session.close then
				session:close();
			end
		end
	end
	return json_ok(event, { revoked = #clients, user = username });
end

local function handle_revoke_client(event)
	local _, code = require_admin(event);
	if code then return code; end
	local path = event.request.path or "";
	local username, client_id = path:match("/clients/([^/]+)/([^/]+)/?$");
	if not username or not client_id then
		return 404;
	end
	username = url_decode(username);
	client_id = url_decode(client_id);
	revoke_client_for_user(username, client_id);
	return 204;
end

local function collect_uploads()
	local used_bytes = 0;
	local files = {};
	local upload_stats = {};
	local share_session = prosody.hosts[share_host];
	if not share_session then
		return used_bytes, files, upload_stats, false;
	end
	local ok, store = pcall(function ()
		return module:context(share_host):open_store("upload_stats");
	end);
	if ok and store then
		upload_stats = store:get(nil) or store:get() or {};
	end
	local ok2, archive = pcall(function ()
		return module:context(share_host):open_store("uploads", "archive");
	end);
	if ok2 and archive and archive.find then
		local iter = archive:find(nil, { reverse = true, limit = 500 });
		if iter then
			for id, item, when in iter do
				local size = 0;
				local uploader = nil;
				local name = id;
				if type(item) == "table" then
					size = tonumber(item.size or item.bytes or item.length) or 0;
					uploader = item.uploader or item.user or item.username;
					name = item.filename or item.name or id;
				end
				used_bytes = used_bytes + size;
				files[#files+1] = {
					id = id;
					name = name;
					size = size;
					uploader = uploader;
					when = when and math.floor(when) or nil;
				};
			end
		end
	end
	return used_bytes, files, upload_stats, true;
end

local function handle_uploads(event)
	local _, code = require_admin(event);
	if code then return code; end

	local used_bytes, files, upload_stats, available = collect_uploads();
	table.sort(files, function (a, b)
		return (a.size or 0) > (b.size or 0);
	end);

	local orphans = {};
	local by_user_map = {};
	for _, file in ipairs(files) do
		if not file.uploader or file.uploader == "" then
			orphans[#orphans+1] = file;
		else
			local key = tostring(file.uploader);
			local row = by_user_map[key];
			if not row then
				row = { user = key, bytes = 0, files = 0, orphans = 0 };
				by_user_map[key] = row;
			end
			row.bytes = row.bytes + (file.size or 0);
			row.files = row.files + 1;
		end
	end
	local by_user = {};
	for _, row in pairs(by_user_map) do
		by_user[#by_user+1] = row;
	end
	table.sort(by_user, function (a, b)
		return (a.bytes or 0) > (b.bytes or 0);
	end);

	return json_ok(event, {
		host = share_host;
		available = available;
		used_bytes = used_bytes;
		file_count = #files;
		orphan_count = #orphans;
		largest = list(slice(files, 1, 25));
		orphans = list(slice(orphans, 1, 50));
		by_user = list(slice(by_user, 1, 100));
		stats = upload_stats;
		global_quota_gb = tonumber(os.getenv("SNIKKET_UPLOAD_STORAGE_GB"));
		daily_quota_gb = tonumber(os.getenv("SNIKKET_DAILY_UPLOAD_LIMIT_PER_USER_GB"));
		retention_days = tonumber(os.getenv("SNIKKET_RETENTION_DAYS")) or 7;
	});
end

local function handle_uploads_purge(event)
	local _, code = require_admin(event);
	if code then return code; end

	local params = decode_query(event.request.url.query);
	local mode = params.mode or "orphans";
	local filter_user = params.user;
	local retention_days = tonumber(os.getenv("SNIKKET_RETENTION_DAYS")) or 7;
	local cutoff = os.time() - (retention_days * 86400);
	local removed = 0;

	local ok, archive = pcall(function ()
		return module:context(share_host):open_store("uploads", "archive");
	end);
	if not ok or not archive or not archive.find or not archive.delete then
		return json_ok(event, { removed = 0, note = "upload archive unavailable" });
	end

	local iter = archive:find(nil, { reverse = false, limit = 2000 });
	if iter then
		for id, item, when in iter do
			local uploader = type(item) == "table" and (item.uploader or item.user or item.username) or nil;
			local drop = false;
			if mode == "orphans" and (not uploader or uploader == "") then
				drop = true;
			elseif mode == "expired" and when and when < cutoff then
				drop = true;
			elseif mode == "user" and filter_user and uploader and tostring(uploader) == filter_user then
				drop = true;
			elseif mode == "user_orphans" and filter_user and (not uploader or uploader == "") then
				-- no-op for true orphans when filtering by user
				drop = false;
			end
			if drop then
				archive:delete(nil, id);
				removed = removed + 1;
			end
		end
	end
	return json_ok(event, { removed = removed, mode = mode, user = filter_user });
end

local function handle_invite_stats(event)
	local _, code = require_admin(event);
	if code then return code; end

	local outstanding = 0;
	local used = 0;
	local by_source = {};
	local tracking = {};

	local ok, inv = pcall(function ()
		return module:depends("invites");
	end);
	if ok and inv and inv.pending_account_invites then
		for invite in inv.pending_account_invites() do
			outstanding = outstanding + 1;
			local source = "unknown";
			if invite.additional_data and invite.additional_data.source then
				source = tostring(invite.additional_data.source);
			elseif invite.source then
				source = tostring(invite.source);
			end
			by_source[source] = (by_source[source] or 0) + 1;
		end
	end

	local track_data = invites_tracking:get() or {};
	if type(track_data) == "table" then
		for token, row in pairs(track_data) do
			if type(row) == "table" then
				used = used + 1;
				local source = tostring(row.source or "unknown");
				by_source[source] = (by_source[source] or 0) + 1;
				tracking[#tracking+1] = {
					token = token;
					source = source;
					when = row.when or row.timestamp or row.used_at;
					user = row.username or row.user or row.jid;
				};
			end
		end
	end

	local bootstrap = invites_bootstrap:get() or {};
	local bootstrap_status = {
		records = type(bootstrap) == "table" and (bootstrap.n or #bootstrap or 0) or 0;
		configured = module:get_option_string("invites_bootstrap_secret") ~= nil;
		index = module:get_option_number("invites_bootstrap_index");
	};

	table.sort(tracking, function (a, b)
		return tonumber(a.when or 0) > tonumber(b.when or 0);
	end);

	local daily_map = {};
	for _, row in ipairs(tracking) do
		local ts = tonumber(row.when or 0) or 0;
		if ts > 0 then
			local day = os.date("!%Y-%m-%d", ts);
			daily_map[day] = (daily_map[day] or 0) + 1;
		end
	end
	local daily = {};
	for i = 29, 0, -1 do
		local day = os.date("!%Y-%m-%d", os.time() - (i * 86400));
		daily[#daily+1] = { day = day, used = daily_map[day] or 0 };
	end

	local total = outstanding + used;
	return json_ok(event, {
		outstanding = outstanding;
		used = used;
		conversion_rate = total > 0 and (used / total) or 0;
		by_source = by_source;
		tracking = list(slice(tracking, 1, 100));
		daily = list(daily);
		bootstrap = bootstrap_status;
	});
end

local function archive_count(store, username)
	if not store or not store.find then
		return 0, nil;
	end
	local n, oldest = 0, nil;
	local iter = store:find(username, { limit = 5000 });
	if not iter then
		return 0, nil;
	end
	for _, _, when in iter do
		n = n + 1;
		if when and (not oldest or when < oldest) then
			oldest = when;
		end
	end
	return n, oldest;
end

local function handle_archives(event)
	local _, code = require_admin(event);
	if code then return code; end

	local users = {};
	local mam_total, offline_total = 0, 0;
	for _, username in ipairs(list_usernames()) do
		local mam_count, mam_oldest = archive_count(archive_store, username);
		local offline_count, offline_oldest = archive_count(offline_store, username);
		mam_total = mam_total + mam_count;
		offline_total = offline_total + offline_count;
		if mam_count > 0 or offline_count > 0 then
			users[#users+1] = {
				username = username;
				mam = mam_count;
				offline = offline_count;
				mam_oldest = mam_oldest and math.floor(mam_oldest) or nil;
				offline_oldest = offline_oldest and math.floor(offline_oldest) or nil;
			};
		end
	end

	local muc_mam_available = false;
	local ok, muc_archive = pcall(function ()
		return module:context(groups_host):open_store("muc_log", "archive");
	end);
	if ok and muc_archive and muc_archive.find then
		muc_mam_available = true;
	end

	table.sort(users, function (a, b)
		return (a.mam + a.offline) > (b.mam + b.offline);
	end);

	return json_ok(event, {
		mam_total = mam_total;
		offline_total = offline_total;
		muc_mam_available = muc_mam_available;
		users = list(slice(users, 1, 100));
		retention_days = tonumber(os.getenv("SNIKKET_RETENTION_DAYS")) or 7;
	});
end

local function handle_updates(event)
	local _, code = require_admin(event);
	if code then return code; end

	local branch = "release";
	local current = {};
	local ok_ver, mod_version = pcall(function ()
		return module:depends("snikket_version");
	end);
	if ok_ver and mod_version and mod_version.snikket_version then
		current.version = mod_version.snikket_version;
		local series, version = tostring(mod_version.snikket_version):match("(%w+) (%S+)$");
		if series then
			branch = series;
			current.branch = series;
			current.level = version and version:match("%d+%.?%d*");
		end
	end

	local latest = {};
	if update_store and update_store.get then
		latest = {
			latest = update_store:get(branch, "latest");
			secure = update_store:get(branch, "secure");
			msg = update_store:get(branch, "msg");
			support_status = update_store:get(branch, "support_status");
		};
	end
	if not next(latest) or (not latest.latest and not latest.secure and not latest.msg) then
		local kv = module:open_store("update_notifications");
		latest = kv:get(branch) or {};
	end

	local check_dns = module:get_option_string("update_check_dns");
	if not check_dns then
		local ok_global, global_mod = pcall(function ()
			return module:context("*");
		end);
		if ok_global and global_mod and global_mod.get_option_string then
			check_dns = global_mod:get_option_string("update_check_dns");
		end
	end

	return json_ok(event, {
		current = current;
		branch = branch;
		latest = latest.latest or latest.version;
		secure = latest.secure;
		message = latest.msg or latest.message;
		support_status = latest.support_status;
		check_enabled = check_dns ~= nil and check_dns ~= "";
	});
end

local function username_from_path(path, prefix)
	local node = path:match(prefix .. "/([^/]+)/?$");
	if not node then
		return nil;
	end
	return url_decode(node);
end

local function handle_account_export(event)
	local _, code = require_admin(event);
	if code then return code; end
	local username = username_from_path(event.request.path or "", "/export");
	if not username or username == "" then
		return 404;
	end
	if not usermanager.user_exists(username, module.host) then
		return 404;
	end
	local package = {
		format = "snikketx-account-v1";
		host = module.host;
		username = username;
		exported_at = os.time();
		roster = roster_store:get(username);
		vcard = vcard_store:get(username);
		clients = clients_store:get(username);
		activity = activity_store:get(username);
	};
	return json_ok(event, package);
end

local function handle_account_import(event)
	local _, code = require_admin(event);
	if code then return code; end
	local username = username_from_path(event.request.path or "", "/import");
	if not username or username == "" then
		return 404;
	end
	if not usermanager.user_exists(username, module.host) then
		return 404;
	end
	local body = event.request.body;
	if not body or #body == 0 then
		return 400;
	end
	local ok, payload = pcall(json.decode, body);
	if not ok or type(payload) ~= "table" then
		return 400;
	end
	local written = {};
	if payload.roster ~= nil then
		roster_store:set(username, payload.roster);
		written[#written+1] = "roster";
	end
	if payload.vcard ~= nil then
		vcard_store:set(username, payload.vcard);
		written[#written+1] = "vcard";
	end
	return json_ok(event, { username = username, written = list(written) });
end

module:provides("http", {
	route = {
		["GET /clients"] = handle_clients;
		["DELETE /clients/*"] = handle_revoke_client;
		["GET /me/clients"] = handle_me_clients;
		["DELETE /me/clients"] = handle_me_revoke_all;
		["DELETE /me/clients/*"] = handle_me_revoke_client;
		["GET /uploads"] = handle_uploads;
		["POST /uploads/purge"] = handle_uploads_purge;
		["GET /invites/stats"] = handle_invite_stats;
		["GET /archives"] = handle_archives;
		["GET /updates"] = handle_updates;
		["GET /export/*"] = handle_account_export;
		["PUT /import/*"] = handle_account_import;
	};
});

module:log("info", "SnikketX ops API ready");
