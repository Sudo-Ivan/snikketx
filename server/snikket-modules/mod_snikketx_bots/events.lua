-- Event fan-out for mod_snikketx_bots: per-bot sequence numbers, a bounded
-- in-memory ring buffer (for Last-Event-ID resume), live subscribers
-- (SSE connections, websocket sessions) and the webhook delivery queue.
-- Events are volatile by design: the ring exists to bridge short client
-- reconnects, not as history.
--luacheck: ignore 111 113/module 131 143/module

local json = require "prosody.util.json";

local M = {};

local config;
local registry;
local webhook; -- optional, injected by main module

-- Per-bot runtime state: { seq = n, ring = {}, ring_start = n, subs = {} }
local state = {};

local function bot_state(bot)
	local s = state[bot.name];
	if not s then
		s = { seq = 0; ring = {}; ring_start = 1; subs = {} };
		state[bot.name] = s;
	end
	return s;
end

local function push_ring(s, event)
	local max = config.event_buffer;
	s.ring[#s.ring + 1] = event;
	if #s.ring > max then
		table.remove(s.ring, 1);
		s.ring_start = s.seq - #s.ring + 1;
	end
end

-- Encode one event as an SSE frame. Exported for the SSE handler and
-- replay path; live subscribers receive the raw event table instead.
function M.sse_frame(event)
	local ok, data = pcall(json.encode, event);
	if not ok then return nil; end
	return ("id: %d\nevent: %s\ndata: %s\n\n"):format(event.seq, event.type, data);
end

local function deliver(sub, event)
	local ok, res = pcall(sub.send, event);
	if not ok or res == false then
		return false;
	end
	return true;
end

-- Emit an event for a bot. fanout order: ring (always), live subscribers,
-- webhook queue. Returns the event table or nil if the bot is disabled.
function M.emit(bot, event)
	if bot.disabled then return nil; end
	local s = bot_state(bot);
	s.seq = s.seq + 1;
	event.v = 1;
	event.seq = s.seq;
	event.bot = bot.name;
	event.ts = os.time();
	push_ring(s, event);

	bot.stats = bot.stats or { events = 0; events_dropped = 0; sends = 0; sends_denied = 0 };
	bot.stats.events = bot.stats.events + 1;

	local dead = {};
	for sub in pairs(s.subs) do
		if not deliver(sub, event) then
			dead[#dead + 1] = sub;
		end
	end
	for _, sub in ipairs(dead) do
		s.subs[sub] = nil;
	end

	if webhook and bot.webhook and not bot.webhook.disabled then
		webhook.enqueue(bot, event);
	end
	return event;
end

-- Replay buffered events newer than since_seq through the deliver
-- callback, which receives raw event tables. Returns true if the buffer
-- still reaches back that far (or since_seq is nil), plus replayed count.
function M.replay(bot, since_seq, deliver_fn)
	local s = bot_state(bot);
	local count = 0;
	local contiguous = true;
	if since_seq then
		if s.seq > 0 and since_seq < s.ring_start - 1 then
			contiguous = false; -- gap: events fell out of the buffer
		end
		for i = 1, #s.ring do
			local event = s.ring[i];
			if event.seq > since_seq then
				if not deliver_fn(event) then break; end
				count = count + 1;
			end
		end
	else
		for i = 1, #s.ring do
			if not deliver_fn(s.ring[i]) then break; end
			count = count + 1;
		end
	end
	return contiguous, count;
end

function M.subscribe(bot, sub)
	local s = bot_state(bot);
	s.subs[sub] = true;
	return function ()
		s.subs[sub] = nil;
	end;
end

function M.subscriber_count(bot)
	local s = state[bot.name];
	if not s then return 0; end
	local n = 0;
	for _ in pairs(s.subs) do n = n + 1; end
	return n;
end

function M.oldest_seq(bot)
	local s = state[bot.name];
	return s and s.ring_start or 0;
end

function M.newest_seq(bot)
	local s = state[bot.name];
	return s and s.seq or 0;
end

-- Push a final event to live subscribers then detach them all. Used when
-- a bot is deleted so stream clients see the lifecycle end. The conn
-- stays open until the client reads the event and closes.
function M.close(bot, event)
	event.type = event.type or "deleted";
	local seq = M.emit(bot, event);
	local s = state[bot.name];
	if s then
		for sub in pairs(s.subs) do
			s.subs[sub] = nil;
		end
	end
	return seq;
end

function M.init(cfg, reg, webhook_mod)
	config = cfg;
	registry = reg;
	webhook = webhook_mod;
end

return M;
