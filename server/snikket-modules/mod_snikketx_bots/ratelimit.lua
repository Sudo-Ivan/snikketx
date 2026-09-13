-- Token bucket rate limiter for mod_snikketx_bots.
-- Buckets are keyed by an arbitrary string and refill at rate tokens/sec
-- up to a burst capacity. Nothing here persists; limits reset on reload.
--luacheck: ignore 111 113/module 131 143/module

local bucket_mt = {};
bucket_mt.__index = bucket_mt;

function bucket_mt:take(cost)
	cost = cost or 1;
	local now = os.time();
	local elapsed = now - self.updated;
	if elapsed > 0 then
		self.tokens = math.min(self.capacity, self.tokens + elapsed * self.rate);
		self.updated = now;
	end
	if self.tokens >= cost then
		self.tokens = self.tokens - cost;
		return true;
	end
	-- Roughly how long until one token is available again
	self.retry_after = math.ceil((cost - self.tokens) / self.rate);
	return false;
end

local function new_bucket(rate_per_minute, burst)
	local rate = rate_per_minute / 60;
	local capacity = burst or math.max(rate_per_minute, rate * 5);
	return setmetatable({
		rate = rate;
		capacity = capacity;
		tokens = capacity;
		updated = os.time();
		retry_after = 1;
	}, bucket_mt);
end

-- Registry of named buckets so callers do not manage bucket lifetimes.
-- table keyed by arbitrary string -> bucket created with the rate given
-- at first use. Buckets are cheap; periodic GC is the caller's choice.
local registry_mt = {};
registry_mt.__index = registry_mt;

function registry_mt:take(key, cost)
	local bucket = self.buckets[key];
	if not bucket then
		bucket = new_bucket(self.rate_per_minute, self.burst);
		self.buckets[key] = bucket;
	end
	return bucket:take(cost);
end

function registry_mt:retry_after(key)
	local bucket = self.buckets[key];
	return bucket and bucket.retry_after or 1;
end

-- Drop buckets that have fully refilled and gone quiet; keeps the table
-- bounded when keys are churny (per-token keys, per-IP keys, etc).
function registry_mt:sweep()
	local now = os.time();
	for key, bucket in pairs(self.buckets) do
		if bucket.tokens >= bucket.capacity and (now - bucket.updated) > 600 then
			self.buckets[key] = nil;
		end
	end
end

local function new_registry(rate_per_minute, burst)
	return setmetatable({
		rate_per_minute = rate_per_minute;
		burst = burst;
		buckets = {};
	}, registry_mt);
end

return {
	new_bucket = new_bucket;
	new_registry = new_registry;
};
