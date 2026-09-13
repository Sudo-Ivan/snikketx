-- Audit trail for mod_snikketx_bots. Security-relevant operations
-- (create/delete/token lifecycle/auth failures/room joins) are appended
-- to a bounded in-memory ring and mirrored into a keyval store so they
-- survive restarts. Also logged at info level so they land in syslog.
--luacheck: ignore 111 113/module 131 143/module

local M = {};

local config;
local store;

local entries = {}; -- bounded array, newest last
local write_seq = 0;

-- Store a trimmed snapshot periodically rather than on every write; the
-- ring is authoritative for reads within a run.
local function persist()
	store:set("log", entries);
end

function M.record(entry)
	entry.ts = os.time();
	write_seq = write_seq + 1;
	entry.id = write_seq;
	entries[#entries + 1] = entry;
	local max = config.audit_size or 1000;
	if #entries > max then
		table.remove(entries, 1);
	end
	module:log("info", "bots-audit: %s%s%s%s",
		entry.action or "?",
		entry.actor and (" by " .. entry.actor) or "",
		entry.bot and (" on " .. entry.bot) or "",
		entry.detail and (" (" .. entry.detail .. ")") or "");
	persist();
end

-- filter: { bot = name, since = ts, limit = n }
function M.list(filter)
	filter = filter or {};
	local out = {};
	local limit = filter.limit or 100;
	for i = #entries, 1, -1 do
		local e = entries[i];
		if (not filter.bot or e.bot == filter.bot)
			and (not filter.since or e.ts >= filter.since) then
			out[#out + 1] = e;
			if #out >= limit then break; end
		end
	end
	return out;
end

function M.init(cfg, store_handle)
	config = cfg;
	store = store_handle;
	local saved = store:get("log");
	if type(saved) == "table" then
		entries = saved;
		write_seq = entries[#entries] and entries[#entries].id or 0;
	end
end

return M;
