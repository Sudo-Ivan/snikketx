package eu.siacs.conversations.utils;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import java.util.Random;
import org.junit.Test;

public class OutgoingMessageParserTest {

    @Test
    public void plainTextPassesThrough() {
        final var parsed = OutgoingMessageParser.parse("hello world");
        assertEquals("hello world", parsed.body());
        assertNull(parsed.spoilerHint());
        assertFalse(parsed.attention());
        assertFalse(parsed.isEmpty());
    }

    @Test
    public void spoilerHintIsExtracted() {
        final var parsed = OutgoingMessageParser.parse("||spoiler||hidden body");
        assertEquals("hidden body", parsed.body());
        assertEquals("spoiler", parsed.spoilerHint());
        assertFalse(parsed.attention());
    }

    @Test
    public void spoilerBodyMayContainFurtherBars() {
        final var parsed = OutgoingMessageParser.parse("||hint||a||b");
        assertEquals("hint", parsed.spoilerHint());
        assertEquals("a||b", parsed.body());
    }

    @Test
    public void emptySpoilerHintIsAllowed() {
        final var parsed = OutgoingMessageParser.parse("||||body");
        assertEquals("", parsed.spoilerHint());
        assertEquals("body", parsed.body());
    }

    @Test
    public void onlyBarsProduceEmptyHintAndEmptyBody() {
        final var parsed = OutgoingMessageParser.parse("||||");
        assertEquals("", parsed.spoilerHint());
        assertEquals("", parsed.body());
        // a non null hint counts as content even when the body is empty
        assertFalse(parsed.isEmpty());
    }

    @Test
    public void unclosedSpoilerIsPlainText() {
        final var parsed = OutgoingMessageParser.parse("||no closing bars");
        assertEquals("||no closing bars", parsed.body());
        assertNull(parsed.spoilerHint());
    }

    @Test
    public void loneLeadingBarsArePlainText() {
        final var parsed = OutgoingMessageParser.parse("||");
        assertEquals("||", parsed.body());
        assertNull(parsed.spoilerHint());
    }

    @Test
    public void barsNotAtStartArePlainText() {
        final var parsed = OutgoingMessageParser.parse(" ||hint||body");
        assertEquals(" ||hint||body", parsed.body());
        assertNull(parsed.spoilerHint());
    }

    @Test
    public void nudgeWithoutTextIsAttention() {
        final var parsed = OutgoingMessageParser.parse("/nudge");
        assertTrue(parsed.attention());
        assertEquals("", parsed.body());
        assertFalse(parsed.isEmpty());
    }

    @Test
    public void attentionAliasIsRecognized() {
        assertTrue(OutgoingMessageParser.parse("/attention").attention());
        assertTrue(OutgoingMessageParser.parse("/attention wake up").attention());
    }

    @Test
    public void nudgeTrailingTextBecomesBody() {
        final var parsed = OutgoingMessageParser.parse("/nudge hello there");
        assertTrue(parsed.attention());
        assertEquals("hello there", parsed.body());
    }

    @Test
    public void similarCommandsAreNotAttention() {
        assertFalse(OutgoingMessageParser.parse("/nudges").attention());
        assertFalse(OutgoingMessageParser.parse("/nudgex").attention());
        assertFalse(OutgoingMessageParser.parse(" /nudge").attention());
        assertFalse(OutgoingMessageParser.parse("a /nudge").attention());
    }

    @Test
    public void spoilerAndAttentionCombine() {
        final var parsed = OutgoingMessageParser.parse("||hint||/nudge pay attention");
        assertEquals("hint", parsed.spoilerHint());
        assertTrue(parsed.attention());
        assertEquals("pay attention", parsed.body());
    }

    @Test
    public void nudgeInsideSpoilerHintIsNotAttention() {
        final var parsed = OutgoingMessageParser.parse("||/nudge||body");
        assertEquals("/nudge", parsed.spoilerHint());
        assertEquals("body", parsed.body());
        assertFalse(parsed.attention());
    }

    @Test
    public void unicodeBodiesArePreserved() {
        final var parsed = OutgoingMessageParser.parse("||titre||corps \uD83D\uDE00 texte");
        assertEquals("titre", parsed.spoilerHint());
        assertEquals("corps \uD83D\uDE00 texte", parsed.body());
    }

    @Test
    public void fuzzDoesNotThrow() {
        final var random = new Random(42);
        final char[] alphabet = "||/nudge attentionabc \uD83D\uDE00\n\t".toCharArray();
        for (int i = 0; i < 2000; ++i) {
            final int length = random.nextInt(64);
            final var builder = new StringBuilder(length);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            final var parsed = OutgoingMessageParser.parse(builder.toString());
            assertNotNull(parsed.body());
            // the resulting body is always a substring of the input
            assertTrue(parsed.isEmpty() || parsed.body().length() <= builder.length());
        }
    }

    @Test
    public void hugeInputIsHandled() {
        final var input = "||" + "h".repeat(100_000) + "||" + "b".repeat(100_000);
        final var parsed = OutgoingMessageParser.parse(input);
        assertEquals(100_000, parsed.spoilerHint().length());
        assertEquals(100_000, parsed.body().length());
    }
}
