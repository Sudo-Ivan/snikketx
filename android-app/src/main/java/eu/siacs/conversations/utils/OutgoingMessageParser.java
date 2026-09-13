package eu.siacs.conversations.utils;

import org.jspecify.annotations.Nullable;

/**
 * Parses the pseudo syntax accepted by the message input field before a message is created.
 *
 * <p>The text between the first two pairs of vertical bars becomes the XEP-0382 spoiler hint,
 * everything after the second pair is the hidden body. A leading /nudge or /attention sends a
 * XEP-0224 attention request, any trailing text becomes the message body.
 */
public final class OutgoingMessageParser {

    private OutgoingMessageParser() {}

    public record Parsed(String body, @Nullable String spoilerHint, boolean attention) {

        /** True when parsing produced nothing worth sending. */
        public boolean isEmpty() {
            return body.isEmpty() && spoilerHint == null && !attention;
        }
    }

    public static Parsed parse(final String input) {
        String body = input;
        String spoilerHint = null;
        if (body.startsWith("||")) {
            final int end = body.indexOf("||", 2);
            if (end >= 2) {
                spoilerHint = body.substring(2, end);
                body = body.substring(end + 2);
            }
        }
        final boolean attention =
                body.equals("/nudge")
                        || body.equals("/attention")
                        || body.startsWith("/nudge ")
                        || body.startsWith("/attention ");
        if (attention) {
            final int space = body.indexOf(' ');
            body = space < 0 ? "" : body.substring(space + 1);
        }
        return new Parsed(body, spoilerHint, attention);
    }
}
