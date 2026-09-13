package eu.siacs.conversations.entities;

import android.util.Log;
import com.google.common.base.Strings;
import com.google.gson.JsonSyntaxException;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.xmpp.Jid;
import im.conversations.android.json.Services;
import org.jspecify.annotations.Nullable;

/**
 * Persisted representation of a XEP-0461 reply reference. The id is the id assigned by the group
 * chat (stanza-id) for public MUC messages and the origin-id or stanza id otherwise. The author
 * display name and the preview are denormalized so a preview can be rendered even when the
 * referenced message is not loaded.
 */
public record InReplyTo(
        @Nullable Jid to, String id, @Nullable String author, @Nullable String preview) {

    public static InReplyTo ofString(final String string) {
        if (Strings.isNullOrEmpty(string)) {
            return null;
        }
        try {
            return Services.GSON.fromJson(string, InReplyTo.class);
        } catch (final JsonSyntaxException | IllegalArgumentException e) {
            Log.w(Config.LOGTAG, "could not parse inReplyTo");
            return null;
        }
    }

    public String toJson() {
        return Services.GSON.toJson(this);
    }
}
