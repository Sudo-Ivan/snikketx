-- WebSocket transport for mod_snikketx_bots. Performs the RFC 6455
-- upgrade inside a plain http route, takes over the socket, and speaks a
-- small JSON protocol: server pushes bot events as text frames, the
-- client sends action frames that mirror the REST actions.
--luacheck: ignore 111 113/module 131 143/module

local json = require "prosody.util.json";
local hashes = require "prosody.util.hashes";
local encodings = require "prosody.util.encodings";
local dbuffer = require "prosody.util.dbuffer";
local ws_frames = require "prosody.net.websocket.frames";

local parse_frame = ws_frames.parse;
local build_frame = ws_frames.build;
local build_close = ws_frames.build_close;
local parse_close = ws_frames.parse_close;

local base64 = encodings.base64.encode;
local sha1 = hashes.sha1;
local WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

local M = {};

local config;
local events;
local dispatch; -- function(bot, tok, msg) -> ok, result_or_err
local sessions = {}; -- conn -> { bot, unsubscribe, buf }

local function ws_send(conn, opcode, data)
	conn:write(build_frame({ FIN = true; opcode = opcode; data = data }));
end

local function ws_send_json(conn, obj)
	local ok, data = pcall(json.encode, obj);
	if not ok then return; end
	ws_send(conn, 0x1, data);
end

local function ws_close(conn, code, reason)
	local s = sessions[conn];
	if s and s.closed then return; end
	if s then s.closed = true; end
	conn:write(build_close(code, reason or ""));
	conn:close();
end

local function handle_text(conn, s, data)
	if #data > (config.max_body or 65536) then
		ws_close(conn, 1009, "message too large");
		return;
	end
	local msg, err = json.decode(data);
	if type(msg) ~= "table" or type(msg.op) ~= "string" then
		ws_send_json(conn, { type = "result"; id = nil; ok = false; error = err or "bad-message" });
		return;
	end
	local ok, result = dispatch(s.bot, s.tok, msg);
	ws_send_json(conn, { type = "result"; id = msg.id; ok = ok; data = ok and result or nil; error = not ok and result or nil });
end

local listener = {};

function listener.onincoming(conn, data)
	local s = sessions[conn];
	if not s then return; end
	if not s.buf:write(data) then
		ws_close(conn, 1009, "frame buffer exceeded");
		return;
	end
	while true do
		local frame, length, partial = parse_frame(s.buf);
		if not frame then
			if partial and partial.length and partial.length > (config.ws_frame_limit or 131072) then
				ws_close(conn, 1009, "frame too large");
				return;
			end
			break;
		end
		s.buf:discard(length);
		local opcode = frame.opcode;
		if opcode == 0x9 then -- ping
			frame.opcode = 0xA;
			frame.MASK = false;
			conn:write(build_frame(frame));
		elseif opcode == 0xA then -- pong
			-- keepalive response, nothing to do
		elseif opcode == 0x8 then -- close
			local code = 1000;
			local parsed = parse_close(frame.data);
			if parsed and parsed.code then code = parsed.code; end
			ws_close(conn, code);
			return;
		elseif opcode == 0x1 or opcode == 0x0 then -- text / continuation
			s.frag = (s.frag or "") .. frame.data;
			if frame.FIN then
				local whole = s.frag;
				s.frag = nil;
				handle_text(conn, s, whole);
				if s.closed then return; end
			end
		elseif opcode == 0x2 then -- binary
			ws_close(conn, 1003, "binary frames not supported");
			return;
		end
	end
end

function listener.ondisconnect(conn)
	local s = sessions[conn];
	if s then
		sessions[conn] = nil;
		if s.unsubscribe then s.unsubscribe(); end
	end
end

function listener.ondetach(conn)
	listener.ondisconnect(conn);
end

-- Called from the HTTP route after auth has already been validated.
-- Returns "" after preparing the 101 upgrade.
function M.handle_request(event, bot, tok)
	local request, response = event.request, event.response;
	local conn = response.conn;

	local key = request.headers.sec_websocket_key;
	if not key or request.method ~= "GET" then
		return 400;
	end

	response.status_code = 101;
	response.headers.upgrade = "websocket";
	response.headers.connection = "Upgrade";
	response.headers.sec_webSocket_accept = base64(sha1(key .. WS_GUID));

	local s = {
		bot = bot;
		tok = tok;
		buf = dbuffer.new(config.ws_frame_limit or 131072, 8);
	};
	sessions[conn] = s;

	s.unsubscribe = events.subscribe(bot, {
		send = function (ev)
			if s.closed then return false; end
			ws_send_json(conn, ev);
			return true;
		end;
	});

	conn:setlistener(listener);

	-- The 101 response is only flushed after this handler returns, so
	-- anything written now would land on the wire before the handshake.
	-- Defer the hello frame to the next event loop tick.
	module:add_timer(0, function ()
		module:log("debug", "ws hello timer fired for %s (closed=%s)", bot.name, tostring(s.closed));
		if s.closed then return; end
		ws_send_json(conn, {
			type = "hello";
			bot = bot.name;
			jid = bot.jid;
			seq = events.newest_seq(bot);
			oldest = events.oldest_seq(bot);
		});
	end);

	return "";
end

function M.close_for(bot)
	for conn, s in pairs(sessions) do
		if s.bot == bot then
			ws_close(conn, 1000, "bot closed");
			sessions[conn] = nil;
		end
	end
end

function M.count()
	local n = 0;
	for _ in pairs(sessions) do n = n + 1; end
	return n;
end

function M.init(cfg, events_mod, dispatch_fn)
	config = cfg;
	events = events_mod;
	dispatch = dispatch_fn;
end

return M;
