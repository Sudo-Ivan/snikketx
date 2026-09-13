-- Advertise the service data policy (XEP-0504) as a service discovery
-- extension form (XEP-0128) so clients can show users how their data is
-- handled: retention, deletion, encryption, access.
--
-- The policy values come from the 'data_policy' option. Keep them honest:
-- they are a promise to the user, not a feature flag.

local dataforms = require "prosody.util.dataforms";

local policy = module:get_option("data_policy", {});

local form_layout = dataforms.new({
	{ name = "FORM_TYPE"; type = "hidden"; value = "urn:xmpp:data-policy:0" };
	{ name = "auth_data"; type = "list-single" };
	{ name = "data_transmission"; type = "list-single" };
	{ name = "encryption_algorithm"; type = "text-single" };
	{ name = "data_retention"; type = "text-single" };
	{ name = "data_deletion"; type = "boolean" };
	{ name = "encryption_at_rest"; type = "boolean" };
	{ name = "tos"; type = "text-single" };
	{ name = "data_export"; type = "boolean" };
	{ name = "access_policy"; type = "list-multi" };
	{ name = "full_erasure"; type = "boolean" };
	{ name = "backup_frequency"; type = "text-single" };
	{ name = "backup_retention"; type = "text-single" };
	{ name = "extra_info"; type = "text-multi" };
});

function module.load()
	module:add_item("extension", form_layout:form(policy, "result"));
end
