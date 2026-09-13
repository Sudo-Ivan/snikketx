package eu.siacs.conversations.utils;

import android.content.Context;
import com.google.common.base.Strings;
import eu.siacs.conversations.R;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.entities.InReplyTo;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.xmpp.Jid;
import org.jspecify.annotations.Nullable;

public final class ReplyUtils {

    private ReplyUtils() {}

    /**
     * Builds the XEP-0461 reply reference for a message the user is replying to. Returns null when
     * the message can not be referenced, e.g. a group chat message that has no stanza-id assigned
     * by the group chat yet.
     */
    @Nullable
    public static InReplyTo create(final Message referenced) {
        if (referenced == null) {
            return null;
        }
        final var conversational = referenced.getConversation();
        final String id;
        if (conversational.getMode() == Conversational.MODE_MULTI
                && !referenced.isPrivateMessage()) {
            // in public group chats the id assigned by the group chat itself must be used
            id = referenced.getServerMsgId();
        } else {
            final var remoteMsgId = referenced.getRemoteMsgId();
            id = Strings.isNullOrEmpty(remoteMsgId) ? referenced.getUuid() : remoteMsgId;
        }
        if (Strings.isNullOrEmpty(id)) {
            return null;
        }
        final Jid to;
        if (referenced.getStatus() == Message.STATUS_RECEIVED) {
            to = referenced.getCounterpart();
        } else if (conversational.getMode() == Conversational.MODE_MULTI
                && conversational instanceof Conversation conversation) {
            to = conversation.getMucOptions().getSelf().getFullJid();
        } else {
            to = conversational.getAccount().getJid().asBareJid();
        }
        final String preview = Strings.emptyToNull(MessageUtils.prepareQuote(referenced));
        final String author = UIHelper.getMessageDisplayName(referenced);
        return new InReplyTo(to, id, author, preview);
    }

    /**
     * Builds the XEP-0428 fallback quote that is prepended to the actual body on the wire so that
     * clients without reply support still see a classic quote.
     */
    public static String fallbackQuote(final Context context, final InReplyTo inReplyTo) {
        if (inReplyTo == null) {
            return "";
        }
        final var builder = new StringBuilder();
        if (!Strings.isNullOrEmpty(inReplyTo.author())) {
            builder.append("> ")
                    .append(context.getString(R.string.reply_fallback_header, inReplyTo.author()))
                    .append('\n');
        }
        if (!Strings.isNullOrEmpty(inReplyTo.preview())) {
            for (final var line : inReplyTo.preview().split("\n")) {
                builder.append("> ").append(line).append('\n');
            }
        }
        return builder.toString();
    }

    /**
     * Removes the quote markers from a fallback excerpt received on the wire so it can be shown as
     * a plain text preview.
     */
    @Nullable
    public static String unquote(@Nullable final String excerpt) {
        if (excerpt == null) {
            return null;
        }
        final var builder = new StringBuilder();
        for (final var line : excerpt.split("\n", -1)) {
            var stripped = line;
            while (stripped.startsWith(">")) {
                stripped = stripped.substring(1).stripLeading();
            }
            if (stripped.isEmpty()) {
                continue;
            }
            if (builder.length() > 0) {
                builder.append('\n');
            }
            builder.append(stripped);
        }
        return Strings.emptyToNull(builder.toString());
    }
}
