package im.conversations.android.xmpp.model.fallback;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import eu.siacs.conversations.xml.LocalizedContent;
import eu.siacs.conversations.xml.Namespace;
import im.conversations.android.xmpp.model.reply.Reply;
import java.util.Random;
import org.junit.Test;

public class FallbackTest {

    private static Fallback.Range range(final int start, final int end) {
        final var body = new Body();
        body.setAttribute("start", start);
        body.setAttribute("end", end);
        return body.getRange();
    }

    private static LocalizedContent content(final String content) {
        return LocalizedContent.of(content, "en", 1);
    }

    @Test
    public void substringAndRemoveAreComplementary() {
        final var range = range(0, 4);
        assertEquals("> hi", range.substringOf("> hireal"));
        assertEquals("real", range.removeFrom("> hireal"));
    }

    @Test
    public void rangeInMiddle() {
        final var range = range(2, 5);
        assertEquals("cde", range.substringOf("abcdef"));
        assertEquals("abf", range.removeFrom("abcdef"));
    }

    @Test
    public void astralCharacterCountsAsOneCodePoint() {
        // 'a' + U+1F600 + 'b' is 3 code points but 4 UTF-16 units
        final var range = range(1, 2);
        assertEquals("\uD83D\uDE00", range.substringOf("a\uD83D\uDE00b"));
        assertEquals("ab", range.removeFrom("a\uD83D\uDE00b"));
    }

    @Test
    public void endBeyondContentIsClamped() {
        final var range = range(0, 100);
        assertEquals("abc", range.substringOf("abc"));
        assertEquals("", range.removeFrom("abc"));
    }

    @Test
    public void negativeStartIsClamped() {
        final var range = range(-5, 2);
        assertEquals("ab", range.substringOf("abc"));
        assertEquals("c", range.removeFrom("abc"));
    }

    @Test
    public void invertedRangeIsEmpty() {
        final var range = range(3, 1);
        assertEquals("", range.substringOf("abcd"));
        assertEquals("abcd", range.removeFrom("abcd"));
    }

    @Test
    public void isEntireForAsciiContent() {
        assertTrue(range(0, 3).isEntire(content("abc")));
        assertTrue(range(0, 100).isEntire(content("abc")));
        // legacy clients may send end == length - 1; that is still treated as entire
        assertTrue(range(0, 2).isEntire(content("abc")));
        assertFalse(range(0, 1).isEntire(content("abc")));
        assertFalse(range(1, 3).isEntire(content("abc")));
    }

    @Test
    public void isEntireForAstralContent() {
        // two emoji are 2 code points but 4 UTF-16 units; a range covering both
        // code points covers the whole content
        assertTrue(range(0, 2).isEntire(content("\uD83D\uDE00\uD83D\uDE01")));
        // a range covering only the first emoji is not entire
        assertFalse(range(0, 1).isEntire(content("\uD83D\uDE00\uD83D\uDE01")));
        // single emoji: 1 code point, 2 UTF-16 units
        assertTrue(range(0, 1).isEntire(content("\uD83D\uDE00")));
    }

    @Test
    public void missingAttributesYieldFullRange() {
        final var body = new Body();
        final var range = body.getRange();
        assertTrue(range.isEntire(content("anything")));
        assertEquals("", range.removeFrom("anything"));
        assertEquals("anything", range.substringOf("anything"));
    }

    @Test
    public void partialAttributesYieldFullRange() {
        final var body = new Body();
        body.setAttribute("start", 2);
        final var range = body.getRange();
        assertTrue(range.isEntire(content("anything")));
    }

    @Test
    public void nonIntegerAttributesYieldFullRange() {
        final var body = new Body();
        body.setAttribute("start", "zero");
        body.setAttribute("end", "ten");
        assertTrue(body.getRange().isEntire(content("anything")));
    }

    @Test
    public void getFindsRangeForMatchingNamespace() {
        final var message = new im.conversations.android.xmpp.model.stanza.Message();
        final var reply = new Reply();
        reply.setId("reply-id");
        message.addExtension(reply);
        final var fallback = new Fallback();
        fallback.setAttribute("for", Namespace.REPLY);
        final var body = new Body();
        body.setAttribute("start", 0);
        body.setAttribute("end", 5);
        fallback.addExtension(body);
        message.addExtension(fallback);

        final var range = Fallback.get(message, Reply.class, Body.class);
        assertTrue(range.isPresent());
        assertEquals("> hi\n", range.get().substringOf("> hi\nreal"));
        assertEquals("real", range.get().removeFrom("> hi\nreal"));
    }

    @Test
    public void getReturnsFullRangeForBodilessFallback() {
        final var message = new im.conversations.android.xmpp.model.stanza.Message();
        message.addExtension(new Reply());
        final var fallback = new Fallback();
        fallback.setAttribute("for", Namespace.REPLY);
        message.addExtension(fallback);

        final var range = Fallback.get(message, Reply.class, Body.class);
        assertTrue(range.isPresent());
        assertTrue(range.get().isEntire(content("body")));
        assertEquals("", range.get().removeFrom("body"));
    }

    @Test
    public void getIgnoresFallbackForOtherNamespaces() {
        final var message = new im.conversations.android.xmpp.model.stanza.Message();
        message.addExtension(new Reply());
        final var fallback = new Fallback();
        fallback.setAttribute("for", Namespace.REACTIONS);
        final var body = new Body();
        body.setAttribute("start", 0);
        body.setAttribute("end", 4);
        fallback.addExtension(body);
        message.addExtension(fallback);

        assertTrue(!Fallback.get(message, Reply.class, Body.class).isPresent());
    }

    @Test
    public void getIsAbsentWithoutFallback() {
        final var message = new im.conversations.android.xmpp.model.stanza.Message();
        message.addExtension(new Reply());
        assertTrue(!Fallback.get(message, Reply.class, Body.class).isPresent());
    }

    @Test
    public void rangeFuzzDoesNotThrow() {
        final var random = new Random(42);
        final char[] alphabet = "ab\uD83D\uDE00> \n\u00DF".toCharArray();
        for (int i = 0; i < 2000; ++i) {
            final var builder = new StringBuilder();
            final int length = random.nextInt(40);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            final var range = range(random.nextInt(50) - 10, random.nextInt(50) - 10);
            final var sub = range.substringOf(builder.toString());
            final var removed = range.removeFrom(builder.toString());
            // removing and reinserting the covered span reproduces the input
            assertEquals(builder.toString().length(), removed.length() + sub.length());
        }
    }
}
