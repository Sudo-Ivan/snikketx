package eu.siacs.conversations.utils;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import android.content.Context;
import eu.siacs.conversations.entities.InReplyTo;
import java.util.Random;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class ReplyUtilsTest {

    private final Context context = RuntimeEnvironment.getApplication();

    @Test
    public void fallbackQuoteOfNullIsEmpty() {
        assertEquals("", ReplyUtils.fallbackQuote(context, null));
    }

    @Test
    public void fallbackQuoteWithAuthorAndPreview() {
        final var inReplyTo = new InReplyTo(null, "id-1", "Alice", "hello");
        assertEquals("> Alice wrote:\n> hello\n", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void fallbackQuoteWithAuthorOnly() {
        final var inReplyTo = new InReplyTo(null, "id-1", "Alice", null);
        assertEquals("> Alice wrote:\n", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void fallbackQuoteWithPreviewOnly() {
        final var inReplyTo = new InReplyTo(null, "id-1", null, "hello");
        assertEquals("> hello\n", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void fallbackQuoteWithoutAuthorOrPreviewIsEmpty() {
        final var inReplyTo = new InReplyTo(null, "id-1", null, null);
        assertEquals("", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void fallbackQuoteQuotesEveryPreviewLine() {
        final var inReplyTo = new InReplyTo(null, "id-1", null, "line one\nline two");
        assertEquals("> line one\n> line two\n", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void fallbackQuotePreservesEmptyPreviewLines() {
        final var inReplyTo = new InReplyTo(null, "id-1", null, "a\n\nb");
        assertEquals("> a\n> \n> b\n", ReplyUtils.fallbackQuote(context, inReplyTo));
    }

    @Test
    public void unquoteOfNullIsNull() {
        assertNull(ReplyUtils.unquote(null));
    }

    @Test
    public void unquoteOfEmptyIsNull() {
        assertNull(ReplyUtils.unquote(""));
        assertNull(ReplyUtils.unquote(">"));
        assertNull(ReplyUtils.unquote("> "));
        assertNull(ReplyUtils.unquote("> > >"));
    }

    @Test
    public void unquoteStripsSingleMarker() {
        assertEquals("hello", ReplyUtils.unquote("> hello"));
        assertEquals("hello", ReplyUtils.unquote(">hello"));
    }

    @Test
    public void unquoteStripsNestedMarkers() {
        assertEquals("deep", ReplyUtils.unquote("> > > deep"));
        assertEquals("deep", ReplyUtils.unquote(">>>deep"));
    }

    @Test
    public void unquoteJoinsQuotedLines() {
        assertEquals("a\nb", ReplyUtils.unquote("> a\n> b"));
    }

    @Test
    public void unquoteKeepsUnquotedLines() {
        assertEquals("wrote:\nquoted\nreal", ReplyUtils.unquote("> wrote:\n> quoted\nreal"));
    }

    @Test
    public void unquoteDropsBlankLines() {
        assertEquals("a\nb", ReplyUtils.unquote("a\n\nb"));
        assertEquals("a\nb", ReplyUtils.unquote("> a\n\n> b"));
    }

    @Test
    public void unquoteHandlesUnicode() {
        assertEquals("\uD83D\uDE00 emoji", ReplyUtils.unquote("> \uD83D\uDE00 emoji"));
    }

    @Test
    public void quoteAndUnquoteRoundTrip() {
        final var inReplyTo = new InReplyTo(null, "id-1", "Alice", "first\nsecond");
        final var quoted = ReplyUtils.fallbackQuote(context, inReplyTo);
        assertEquals("Alice wrote:\nfirst\nsecond", ReplyUtils.unquote(quoted));
    }

    @Test
    public void unquoteFuzzDoesNotThrow() {
        final var random = new Random(42);
        final char[] alphabet = "> ab\n\t\uD83D\uDE00\u00A0".toCharArray();
        for (int i = 0; i < 2000; ++i) {
            final int length = random.nextInt(80);
            final var builder = new StringBuilder(length);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            ReplyUtils.unquote(builder.toString());
        }
        // a long pathological input of only markers
        assertNull(ReplyUtils.unquote(">".repeat(10_000)));
    }
}
