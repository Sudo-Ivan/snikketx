package eu.siacs.conversations.utils;

import java.util.List;
import org.junit.Assert;
import org.junit.Test;

public class MessageStylingTest {

    private static List<MessageStyling.Style> ofType(
            final String text, final MessageStyling.Type type) {
        final var styles = MessageStyling.parse(text);
        final var filtered = new java.util.ArrayList<MessageStyling.Style>();
        for (final var style : styles) {
            if (style.getType() == type) {
                filtered.add(style);
            }
        }
        return filtered;
    }

    private static MessageStyling.Style only(final String text, final MessageStyling.Type type) {
        final var styles = ofType(text, type);
        Assert.assertEquals(1, styles.size());
        return styles.get(0);
    }

    @Test
    public void plainTextIsNotStyled() {
        Assert.assertTrue(MessageStyling.parse("hello world").isEmpty());
    }

    @Test
    public void bold() {
        final var style = only("this is *bold* text", MessageStyling.Type.BOLD);
        Assert.assertEquals("bold", style.getContent("this is *bold* text"));
    }

    @Test
    public void boldAtStartAndEnd() {
        Assert.assertEquals(1, ofType("*bold*", MessageStyling.Type.BOLD).size());
        Assert.assertEquals(1, ofType("*bold* tail", MessageStyling.Type.BOLD).size());
        Assert.assertEquals(1, ofType("head *bold*", MessageStyling.Type.BOLD).size());
    }

    @Test
    public void italic() {
        final var style = only("some _italic_ span", MessageStyling.Type.ITALIC);
        Assert.assertEquals("italic", style.getContent("some _italic_ span"));
    }

    @Test
    public void strikethrough() {
        final var style = only("a ~strike~ b", MessageStyling.Type.STRIKETHROUGH);
        Assert.assertEquals("strike", style.getContent("a ~strike~ b"));
    }

    @Test
    public void monospace() {
        final var style = only("run `rm -rf /` now", MessageStyling.Type.MONOSPACE);
        Assert.assertEquals("rm -rf /", style.getContent("run `rm -rf /` now"));
    }

    @Test
    public void preformattedBlock() {
        final String text = "look at this\n```\ncode line 1\ncode line 2\n```\ndone";
        final var style = only(text, MessageStyling.Type.PRE);
        Assert.assertEquals("code line 1\ncode line 2", style.getContent(text));
    }

    @Test
    public void preformattedBlockWithoutClosingFence() {
        final String text = "```\ncode line";
        final var style = only(text, MessageStyling.Type.PRE);
        Assert.assertEquals("code line", style.getContent(text));
    }

    @Test
    public void blockQuote() {
        final String text = "said\n> quoted line one\n> quoted line two\nreply";
        final var style = only(text, MessageStyling.Type.QUOTE);
        Assert.assertEquals("> quoted line one\n> quoted line two", style.getContent(text));
    }

    @Test
    public void nestedQuote() {
        final String text = ">> deep quote";
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.QUOTE).size());
    }

    @Test
    public void unclosedDirectiveIsLiteral() {
        Assert.assertTrue(ofType("this is *not bold", MessageStyling.Type.BOLD).isEmpty());
        Assert.assertTrue(ofType("this is _not italic", MessageStyling.Type.ITALIC).isEmpty());
        Assert.assertTrue(ofType("~oops", MessageStyling.Type.STRIKETHROUGH).isEmpty());
        Assert.assertTrue(ofType("`code", MessageStyling.Type.MONOSPACE).isEmpty());
    }

    @Test
    public void midWordDirectivesAreLiteral() {
        Assert.assertTrue(MessageStyling.parse("2*3*4").isEmpty());
        Assert.assertTrue(ofType("snake_case_name_here", MessageStyling.Type.BOLD).isEmpty());
        Assert.assertTrue(ofType("snake_case_name_here", MessageStyling.Type.ITALIC).isEmpty());
        Assert.assertTrue(ofType("file~name~test", MessageStyling.Type.STRIKETHROUGH).isEmpty());
    }

    @Test
    public void directivesFollowedByWhitespaceAreLiteral() {
        Assert.assertTrue(ofType("a * b", MessageStyling.Type.BOLD).isEmpty());
        Assert.assertTrue(ofType("* b*", MessageStyling.Type.BOLD).isEmpty());
    }

    @Test
    public void nestedStyles() {
        final String text = "*bold and _italic_ inside*";
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.BOLD).size());
        final var italic = only(text, MessageStyling.Type.ITALIC);
        Assert.assertEquals("italic", italic.getContent(text));
    }

    @Test
    public void combinedStyles() {
        final String text = "*bold* and _italic_ and ~strike~ and `code`";
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.BOLD).size());
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.ITALIC).size());
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.STRIKETHROUGH).size());
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.MONOSPACE).size());
    }

    @Test
    public void directiveAdjacentToEmoji() {
        final String text = "*bold* \uD83D\uDE00";
        Assert.assertEquals(1, ofType(text, MessageStyling.Type.BOLD).size());
        final String leading = "\uD83D\uDE00 *bold*";
        Assert.assertEquals(1, ofType(leading, MessageStyling.Type.BOLD).size());
        final String inside = "*\uD83D\uDE00*";
        final var style = only(inside, MessageStyling.Type.BOLD);
        Assert.assertEquals("\uD83D\uDE00", style.getContent(inside));
    }

    @Test
    public void spansDoNotCrossLines() {
        final String text = "*not\nbold*";
        Assert.assertTrue(ofType(text, MessageStyling.Type.BOLD).isEmpty());
    }

    @Test
    public void tripleDirectiveNests() {
        final String text = "***strong***";
        final var styles = ofType(text, MessageStyling.Type.BOLD);
        Assert.assertEquals(3, styles.size());
        Assert.assertEquals("strong", styles.get(0).getContent(text));
    }
}
