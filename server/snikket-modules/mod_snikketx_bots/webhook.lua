-- Webhook delivery for mod_snikketx_bots: validates target URLs (SSRF
-- checks), queues events per bot, delivers with HMAC signatures, retries
-- with backoff and trips a circuit breaker on sustained failure.
--
-- Known limitation: Prosody's net.http resolves the hostname itself at
-- connect time and has no request timeout, so a hostile DNS record could
-- re-resolve between our check and the delivery, and a stalled peer is
-- only handled by the watchdog. Deployments that care should restrict
-- outbound egress or set bot_webhook_allowed_hosts.
--luacheck: ignore 111 113/module 131 143/module

local json = require "prosody.util.json";
local http = require "prosody.net.http";
local adns = require "prosody.net.adns";
local ip = require "prosody.util.ip";
local hashes = require "prosody.util.hashes";

local M = {};

local config;
local registry; -- injected by init()
local queue = {}; -- bot name -> { pending = {...}, in_flight = bool, failures = n }
local retry_backoff = { 1, 5, 30, 120, 600 };
local breaker_threshold = 10;

local function parse_url(url)
	-- Strict absolute-URL parser. Rejects userinfo so credentials never
	-- end up in URLs, headers or logs.
	if type(url) ~= "string" or #url > 2048 then
		return nil, "invalid-url";
	end
	local scheme, rest = url:match("^(https?)://(.+)$");
	if not scheme then
		return nil, "invalid-url";
	end
	local authority, path = rest:match("^([^/]*)(/.*)$");
	if not authority then
		authority, path = rest, "/";
	end
	if authority == "" or authority:find("@") then
		return nil, "invalid-url";
	end
	local host, port;
	if authority:sub(1, 1) == "[" then
		host, port = authority:match("^%[([^%]]+)%]:(%d+)$");
		if not host then
			host = authority:match("^%[([^%]]+)%]$");
		end
	else
		host, port = authority:match("^([^:]+):?(%d*)$");
	end
	if not host or host == "" then
		return nil, "invalid-url";
	end
	port = tonumber(port);
	if port and (port < 1 or port > 65535) then
		return nil, "invalid-url";
	end
	return { scheme = scheme; host = host; port = port; path = path };
end

local function ip_is_blocked(ip_str)
	if config.webhook_allow_private then return false; end
	local addr = ip.new_ip(ip_str);
	if not addr then return true; end -- unparseable: block
	if addr:private() then return true; end
	-- util.ip :private() covers loopback, link-local, RFC1918 and other
	-- non-global scopes. Belt and braces for the unspecified addresses.
	if ip_str == "0.0.0.0" or ip_str == "::" then return true; end
	return false;
end

local function host_is_blocked_literal(host)
	if host == "localhost" or host:sub(-10) == ".localhost" or host:sub(-9) == ".internal" then
		return true;
	end
	if ip.new_ip(host) then
		return ip_is_blocked(host);
	end
	return false;
end

-- Async DNS check; every answer must be a public IP unless
-- webhook_allow_private is set.
local function resolve_and_check(host, cb)
	if host_is_blocked_literal(host) then
		return cb(false, "blocked-host");
	end
	if ip.new_ip(host) then
		-- Public literal IP: nothing to resolve.
		return cb(true);
	end
	local pending = 2;
	local saw_record = false;
	local ok = true;
	local function done()
		pending = pending - 1;
		if pending == 0 then
			if not saw_record then
				return cb(false, "nxdomain");
			end
			cb(ok);
		end
	end
	local function check(answer)
		if answer then
			for _, record in ipairs(answer) do
				local addr = record.a or record.aaaa;
				if addr then
					saw_record = true;
					if ip_is_blocked(addr) then
						ok = false;
					end
				end
			end
		end
		done();
	end
	adns.lookup(check, host, "A", "IN");
	adns.lookup(check, host, "AAAA", "IN");
end

-- Validate a webhook URL synchronously for shape, asynchronously for DNS.
-- cb(ok, err_reason).
function M.validate(url, cb)
	if type(url) ~= "string" or url == "" then
		return cb(false, "invalid-url");
	end
	local parsed, err = parse_url(url);
	if not parsed then
		return cb(false, err);
	end
	if parsed.scheme == "http" and config.webhook_require_https then
		return cb(false, "https-required");
	end
	if config.webhook_allowed_hosts then
		local allowed = false;
		for _, h in ipairs(config.webhook_allowed_hosts) do
			if parsed.host == h or parsed.host:sub(-#h - 1) == "." .. h then
				allowed = true;
				break;
			end
		end
		if not allowed then
			return cb(false, "host-not-allowed");
		end
	end
	resolve_and_check(parsed.host, function (ok, reason)
		if not ok then
			return cb(false, reason);
		end
		cb(true);
	end);
end

local function bot_queue(name)
	local q = queue[name];
	if not q then
		q = { pending = {}; in_flight = false; failures = 0; req_id = 0 };
		queue[name] = q;
	end
	return q;
end

local function sign_body(secret, body)
	return "sha256=" .. hashes.hmac_sha256(secret, body, true);
end

local pump; -- forward declaration

-- net.http has no request timeout. The watchdog releases the slot so the
-- queue keeps moving even if the peer stalls forever.
local function watchdog()
	for name, q in pairs(queue) do
		if q.in_flight and q.in_flight_since
			and (os.time() - q.in_flight_since) > (config.webhook_timeout or 30) then
			q.in_flight = false;
			q.failures = q.failures + 1;
			module:log("warn", "bot %s webhook delivery timed out", name);
		end
		pump(name);
	end
	return 15;
end

local function schedule_retry(name, item)
	local delay = retry_backoff[item.attempts] or 600;
	module:add_timer(delay, function ()
		pump(name);
	end);
end

pump = function (name)
	local q = queue[name];
	if not q or q.in_flight or #q.pending == 0 then return; end
	local bot = registry.get(name);
	if not bot or not bot.webhook or bot.webhook.disabled or bot.disabled then
		if q then q.pending = {}; end
		return;
	end

	local item = q.pending[1];
	local event = item.event;
	local ok, body = pcall(json.encode, event);
	if not ok then
		table.remove(q.pending, 1);
		return;
	end

	q.in_flight = true;
	q.in_flight_since = os.time();
	-- Generation counter: a late response for a request the watchdog
	-- already timed out must not consume the item now at the head.
	q.req_id = (q.req_id or 0) + 1;
	local req_id = q.req_id;

	http.request(bot.webhook.url, {
		method = "POST";
		body = body;
		headers = {
			["Content-Type"] = "application/json";
			["User-Agent"] = "snikketx-bots/1";
			["X-Bots-Event"] = event.type;
			["X-Bots-Event-Id"] = tostring(event.seq);
			["X-Bots-Signature"] = bot.webhook.secret and sign_body(bot.webhook.secret, body) or nil;
		};
		suppress_errors = true;
	}, function (_, code)
		if not q.in_flight or req_id ~= q.req_id then
			-- Watchdog already released the slot; discard this response.
			return;
		end
		q.in_flight = false;
		q.in_flight_since = nil;
		if code and code >= 200 and code < 300 then
			table.remove(q.pending, 1);
			q.failures = 0;
			pump(name); -- keep the pipeline moving on success
			return;
		end
		q.failures = q.failures + 1;
		item.attempts = item.attempts + 1;
		if item.attempts >= #retry_backoff then
			table.remove(q.pending, 1); -- dead letter
			bot.stats.webhook_dropped = (bot.stats.webhook_dropped or 0) + 1;
			module:log("warn", "bot %s webhook dead-lettered event seq %s after %d attempts (last code %s)",
				name, tostring(event.seq), item.attempts, tostring(code));
		else
			schedule_retry(name, item);
		end
		if q.failures >= breaker_threshold and bot.webhook then
			bot.webhook.disabled = true;
			registry.save(bot);
			module:log("warn", "bot %s webhook disabled after %d consecutive delivery failures; re-enable via the management API",
				name, q.failures);
		end
	end);
end;

function M.enqueue(bot, event)
	local q = bot_queue(bot.name);
	local max = config.webhook_queue_size or config.event_buffer or 512;
	if #q.pending >= max then
		bot.stats.webhook_dropped = (bot.stats.webhook_dropped or 0) + 1;
		return;
	end
	q.pending[#q.pending + 1] = { event = event; attempts = 0 };
	pump(bot.name);
end

function M.pending(name)
	local q = queue[name];
	return q and #q.pending or 0;
end

function M.remove(name)
	queue[name] = nil;
end

function M.failures(name)
	local q = queue[name];
	return q and q.failures or 0;
end

function M.init(cfg, reg)
	config = cfg;
	registry = reg;
	module:add_timer(15, watchdog);
end

return M;
