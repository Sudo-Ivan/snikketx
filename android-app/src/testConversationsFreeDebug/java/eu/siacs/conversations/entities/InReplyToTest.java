package eu.siacs.conversations.entities;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import eu.siacs.conversations.xmpp.Jid;
import java.util.Random;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class InReplyToTest {

    @Test
    public void jsonRoundTrip() {
        final var original =
                new InReplyTo(Jid.of("romeo@example.net"), "id-1", "Romeo", "a preview");
        final var parsed = InReplyTo.ofString(original.toJson());
        assertEquals(original, parsed);
    }

    @Test
    public void jsonRoundTripWithNullFields() {
        final var original = new InReplyTo(null, "id-2", null, null);
        final var parsed = InReplyTo.ofString(original.toJson());
        assertEquals(original, parsed);
        assertNull(parsed.to());
        assertNull(parsed.author());
        assertNull(parsed.preview());
    }

    @Test
    public void jsonRoundTripWithFullJidAndUnicode() {
        final var original =
                new InReplyTo(
                        Jid.of("room@example.org/\u00FCser"), "id-3", "p\u00E4ddy", "\uD83D\uDE00");
        final var parsed = InReplyTo.ofString(original.toJson());
        assertEquals(original.to().toString(), parsed.to().toString());
        assertEquals(original, parsed);
    }

    @Test
    public void ofStringRejectsEmptyInput() {
        assertNull(InReplyTo.ofString(null));
        assertNull(InReplyTo.ofString(""));
    }

    @Test
    public void ofStringRejectsMalformedJson() {
        assertNull(InReplyTo.ofString("not json"));
        assertNull(InReplyTo.ofString("{"));
        assertNull(InReplyTo.ofString("[1,2,3]"));
    }

    @Test
    public void ofStringRejectsNonStringJid() {
        // the Jid type adapter only accepts strings or null
        assertNull(InReplyTo.ofString("{\"to\":123,\"id\":\"x\"}"));
        assertNull(InReplyTo.ofString("{\"to\":[\"a@b.c\"],\"id\":\"x\"}"));
    }

    @Test
    public void ofStringRejectsInvalidJid() {
        assertNull(InReplyTo.ofString("{\"to\":\"\",\"id\":\"x\"}"));
        assertNull(InReplyTo.ofString("{\"to\":\"@\",\"id\":\"x\"}"));
    }

    @Test
    public void ofStringParsesPartialObject() {
        final var parsed = InReplyTo.ofString("{\"id\":\"only-id\"}");
        assertEquals("only-id", parsed.id());
        assertNull(parsed.to());
    }

    @Test
    public void ofStringFuzzDoesNotThrow() {
        final var random = new Random(42);
        final char[] alphabet = "{}[]\":,idtoa\uD83D\uDE00 ".toCharArray();
        for (int i = 0; i < 2000; ++i) {
            final int length = random.nextInt(48);
            final var builder = new StringBuilder(length);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            InReplyTo.ofString(builder.toString());
        }
    }
}
