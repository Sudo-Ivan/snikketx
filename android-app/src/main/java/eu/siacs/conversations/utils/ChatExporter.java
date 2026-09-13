package eu.siacs.conversations.utils;

import android.content.Context;
import android.net.Uri;
import android.util.Log;
import com.google.common.base.Strings;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.persistance.DatabaseBackend;
import java.io.BufferedWriter;
import java.io.IOException;
import java.io.OutputStream;
import java.io.OutputStreamWriter;
import java.io.Writer;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.ZoneId;
import java.time.format.DateTimeFormatter;

/**
 * Streams the full message history of a conversation to a Storage Access Framework document as
 * plain text. Must be called from a background thread; the message list is read from the database
 * directly because the in memory list of a conversation may be paged.
 */
public class ChatExporter {

    private static final DateTimeFormatter TIMESTAMP_FORMAT =
            DateTimeFormatter.ISO_OFFSET_DATE_TIME.withZone(ZoneId.systemDefault());

    public static boolean export(
            final Context context, final Conversation conversation, final Uri uri) {
        final var messages =
                DatabaseBackend.getInstance(context).getMessages(conversation, Integer.MAX_VALUE);
        try (final OutputStream outputStream = context.getContentResolver().openOutputStream(uri);
                final Writer writer =
                        new BufferedWriter(
                                new OutputStreamWriter(outputStream, StandardCharsets.UTF_8))) {
            if (outputStream == null) {
                return false;
            }
            writer.write("# ");
            writer.write(Strings.nullToEmpty(conversation.getName().toString()));
            writer.write(" - exported ");
            writer.write(TIMESTAMP_FORMAT.format(Instant.now()));
            writer.write('\n');
            writer.write('\n');
            for (final Message message : messages) {
                writer.write('[');
                writer.write(TIMESTAMP_FORMAT.format(Instant.ofEpochMilli(message.getTimeSent())));
                writer.write("] ");
                writer.write(Strings.nullToEmpty(UIHelper.getMessageDisplayName(message)));
                writer.write(": ");
                writer.write(bodyOf(message));
                writer.write('\n');
            }
            writer.flush();
            return true;
        } catch (final IOException | RuntimeException e) {
            Log.e(Config.LOGTAG, "unable to export chat", e);
            return false;
        }
    }

    private static String bodyOf(final Message message) {
        if (message.isRetracted()) {
            return "[retracted]";
        }
        if (message.isDeleted()) {
            return "[deleted]";
        }
        if (message.getType() == Message.TYPE_RTP_SESSION) {
            return "[call]";
        }
        if (message.isFileOrImage() || message.treatAsDownloadable()) {
            final var storageLocation = message.getRelativeFilePath();
            final String filename =
                    storageLocation == null || storageLocation.file() == null
                            ? null
                            : storageLocation.file().getName();
            return "[file: " + (Strings.isNullOrEmpty(filename) ? "unknown" : filename) + "]";
        }
        return Strings.nullToEmpty(message.getBody());
    }
}
