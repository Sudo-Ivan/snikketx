package eu.siacs.conversations.entities;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;

import android.database.MatrixCursor;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class ScheduledMessageTest {

    private static final String[] COLUMNS = {
        ScheduledMessage.UUID,
        ScheduledMessage.ACCOUNT,
        ScheduledMessage.CONVERSATION,
        ScheduledMessage.BODY,
        ScheduledMessage.SCHEDULED_AT,
        ScheduledMessage.ENCRYPTION
    };

    private static ScheduledMessage roundTrip(final ScheduledMessage message) {
        final var values = message.getContentValues();
        final var cursor = new MatrixCursor(COLUMNS);
        cursor.addRow(
                new Object[] {
                    values.get(ScheduledMessage.UUID),
                    values.get(ScheduledMessage.ACCOUNT),
                    values.get(ScheduledMessage.CONVERSATION),
                    values.get(ScheduledMessage.BODY),
                    values.get(ScheduledMessage.SCHEDULED_AT),
                    values.get(ScheduledMessage.ENCRYPTION)
                });
        cursor.moveToFirst();
        return ScheduledMessage.fromCursor(cursor);
    }

    @Test
    public void contentValuesContainAllFields() {
        final var message = new ScheduledMessage("u1", "acc", "conv", "body", 1234L, 2);
        final var values = message.getContentValues();
        assertEquals("u1", values.getAsString(ScheduledMessage.UUID));
        assertEquals("acc", values.getAsString(ScheduledMessage.ACCOUNT));
        assertEquals("conv", values.getAsString(ScheduledMessage.CONVERSATION));
        assertEquals("body", values.getAsString(ScheduledMessage.BODY));
        assertEquals(Long.valueOf(1234L), values.getAsLong(ScheduledMessage.SCHEDULED_AT));
        assertEquals(Integer.valueOf(2), values.getAsInteger(ScheduledMessage.ENCRYPTION));
        assertEquals(6, values.size());
    }

    @Test
    public void cursorRoundTrip() {
        final var original = new ScheduledMessage("u1", "acc", "conv", "hello", 1700000000000L, 1);
        final var restored = roundTrip(original);
        assertEquals(original.getUuid(), restored.getUuid());
        assertEquals(original.getAccountUuid(), restored.getAccountUuid());
        assertEquals(original.getConversationUuid(), restored.getConversationUuid());
        assertEquals(original.getBody(), restored.getBody());
        assertEquals(original.getScheduledAt(), restored.getScheduledAt());
        assertEquals(original.getEncryption(), restored.getEncryption());
    }

    @Test
    public void timeEdgeCases() {
        for (final long scheduledAt : new long[] {0L, -1L, Long.MIN_VALUE, Long.MAX_VALUE}) {
            final var restored =
                    roundTrip(new ScheduledMessage("u", "a", "c", "b", scheduledAt, 0));
            assertEquals(scheduledAt, restored.getScheduledAt());
        }
    }

    @Test
    public void unicodeBodyRoundTrips() {
        final var body = "multi\nline \uD83D\uDE00 \u00DCnicode";
        final var restored = roundTrip(new ScheduledMessage("u", "a", "c", body, 5L, 0));
        assertEquals(body, restored.getBody());
    }

    @Test
    public void missingColumnThrows() {
        final var cursor = new MatrixCursor(new String[] {ScheduledMessage.UUID});
        cursor.addRow(new Object[] {"u1"});
        cursor.moveToFirst();
        assertThrows(IllegalArgumentException.class, () -> ScheduledMessage.fromCursor(cursor));
    }
}
