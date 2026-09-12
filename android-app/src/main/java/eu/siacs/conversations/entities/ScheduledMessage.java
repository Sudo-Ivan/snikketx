package eu.siacs.conversations.entities;

import android.content.ContentValues;
import android.database.Cursor;

/**
 * A text message that should be sent at a future point in time. The body is stored in plaintext and
 * encrypted through the regular send path once the scheduled time is reached. Scheduled messages
 * fire while the app or service is running, or on next launch. There are no exact alarm wakeups.
 */
public class ScheduledMessage {

    public static final String TABLENAME = "scheduled_messages";
    public static final String UUID = "uuid";
    public static final String ACCOUNT = "accountUuid";
    public static final String CONVERSATION = "conversationUuid";
    public static final String BODY = "body";
    public static final String SCHEDULED_AT = "scheduledAt";
    public static final String ENCRYPTION = "encryption";

    private final String uuid;
    private final String accountUuid;
    private final String conversationUuid;
    private final String body;
    private final long scheduledAt;
    private final int encryption;

    public ScheduledMessage(
            final String uuid,
            final String accountUuid,
            final String conversationUuid,
            final String body,
            final long scheduledAt,
            final int encryption) {
        this.uuid = uuid;
        this.accountUuid = accountUuid;
        this.conversationUuid = conversationUuid;
        this.body = body;
        this.scheduledAt = scheduledAt;
        this.encryption = encryption;
    }

    public String getUuid() {
        return uuid;
    }

    public String getAccountUuid() {
        return accountUuid;
    }

    public String getConversationUuid() {
        return conversationUuid;
    }

    public String getBody() {
        return body;
    }

    public long getScheduledAt() {
        return scheduledAt;
    }

    public int getEncryption() {
        return encryption;
    }

    public ContentValues getContentValues() {
        final ContentValues values = new ContentValues();
        values.put(UUID, uuid);
        values.put(ACCOUNT, accountUuid);
        values.put(CONVERSATION, conversationUuid);
        values.put(BODY, body);
        values.put(SCHEDULED_AT, scheduledAt);
        values.put(ENCRYPTION, encryption);
        return values;
    }

    public static ScheduledMessage fromCursor(final Cursor cursor) {
        return new ScheduledMessage(
                cursor.getString(cursor.getColumnIndexOrThrow(UUID)),
                cursor.getString(cursor.getColumnIndexOrThrow(ACCOUNT)),
                cursor.getString(cursor.getColumnIndexOrThrow(CONVERSATION)),
                cursor.getString(cursor.getColumnIndexOrThrow(BODY)),
                cursor.getLong(cursor.getColumnIndexOrThrow(SCHEDULED_AT)),
                cursor.getInt(cursor.getColumnIndexOrThrow(ENCRYPTION)));
    }
}
