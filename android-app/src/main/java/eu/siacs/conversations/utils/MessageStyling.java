package eu.siacs.conversations.utils;

import android.graphics.Color;
import android.graphics.Typeface;
import android.text.ParcelableSpan;
import android.text.Spannable;
import android.text.Spanned;
import android.text.style.ForegroundColorSpan;
import android.text.style.StrikethroughSpan;
import android.text.style.StyleSpan;
import android.text.style.TypefaceSpan;
import androidx.annotation.ColorInt;
import eu.siacs.conversations.ui.text.QuoteSpan;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.List;

public final class MessageStyling {

    public enum Type {
        BOLD,
        ITALIC,
        STRIKETHROUGH,
        MONOSPACE,
        PRE,
        QUOTE
    }

    private static final String SPAN_DIRECTIVES = "*_~`";
    private static final String OPENING_BOUNDARY_PUNCTUATION = "\"'({[<";
    private static final String FENCE = "```";

    private MessageStyling() {}

    public static List<Style> parse(final CharSequence text) {
        final var styles = new ArrayList<Style>();
        parseBlocks(text, 0, text.length(), styles);
        return styles;
    }

    public static void format(final Spannable editable, @ColorInt final int textColor) {
        format(editable, 0, editable.length(), textColor);
    }

    public static void format(
            final Spannable editable,
            final int start,
            final int end,
            @ColorInt final int textColor) {
        final var styles = new ArrayList<Style>();
        parseBlocks(editable, start, end, styles);
        for (final Style style : styles) {
            final var span = spanForType(style.getType());
            if (span == null) {
                continue;
            }
            if (style.getContentStart() < style.getContentEnd()) {
                editable.setSpan(
                        span,
                        style.getContentStart(),
                        style.getContentEnd(),
                        Spanned.SPAN_EXCLUSIVE_EXCLUSIVE);
            }
            makeDirectiveOpaque(editable, style.getStart(), style.getContentStart(), textColor);
            makeDirectiveOpaque(editable, style.getContentEnd(), style.getEnd(), textColor);
        }
    }

    private static void parseBlocks(
            final CharSequence text, final int start, final int end, final List<Style> styles) {
        int i = start;
        while (i < end) {
            final int lineEnd = lineEnd(text, i, end);
            if (isFenceStart(text, i, lineEnd)) {
                i = parsePreformatted(text, i, lineEnd, end, styles);
            } else if (text.charAt(i) == '>') {
                i = parseQuote(text, i, end, styles);
            } else {
                parseSpans(text, i, lineEnd, styles);
                i = lineEnd + 1;
            }
        }
    }

    private static int parsePreformatted(
            final CharSequence text,
            final int fenceStart,
            final int firstLineEnd,
            final int end,
            final List<Style> styles) {
        final int contentStart = Math.min(firstLineEnd + 1, end);
        int i = contentStart;
        while (i < end) {
            final int lineEnd = lineEnd(text, i, end);
            if (isFenceOnly(text, i, lineEnd)) {
                final int contentEnd = Math.max(contentStart, i - 1);
                styles.add(new Style(Type.PRE, fenceStart, lineEnd, contentStart, contentEnd));
                return lineEnd + 1;
            }
            i = lineEnd + 1;
        }
        styles.add(new Style(Type.PRE, fenceStart, end, contentStart, end));
        return end;
    }

    private static int parseQuote(
            final CharSequence text, final int start, final int end, final List<Style> styles) {
        int i = start;
        int lastLineEnd = start;
        while (i < end && text.charAt(i) == '>') {
            final int lineEnd = lineEnd(text, i, end);
            int inner = i;
            while (inner < lineEnd && text.charAt(inner) == '>') {
                ++inner;
            }
            if (inner < lineEnd && text.charAt(inner) == ' ') {
                ++inner;
            }
            parseSpans(text, inner, lineEnd, styles);
            lastLineEnd = lineEnd;
            i = lineEnd + 1;
        }
        styles.add(new Style(Type.QUOTE, start, lastLineEnd, start, lastLineEnd));
        return i;
    }

    private static void parseSpans(
            final CharSequence text, final int start, final int end, final List<Style> styles) {
        final Deque<int[]> opens = new ArrayDeque<>();
        for (int i = start; i < end; ++i) {
            final char c = text.charAt(i);
            if (SPAN_DIRECTIVES.indexOf(c) < 0) {
                continue;
            }
            if (i > start && !Character.isWhitespace(text.charAt(i - 1))) {
                final int[] open = peekMatching(opens, c);
                if (open != null && i - open[1] > 1) {
                    opens.remove(open);
                    styles.add(new Style(typeFor(c), open[1], i + 1, open[1] + 1, i));
                    continue;
                }
            }
            if (isOpeningDirective(text, i, start, end, opens)) {
                opens.push(new int[] {c, i});
            }
        }
    }

    private static int[] peekMatching(final Deque<int[]> opens, final char c) {
        for (final int[] open : opens) {
            if (open[0] == c) {
                return open;
            }
        }
        return null;
    }

    private static boolean isOpeningDirective(
            final CharSequence text,
            final int i,
            final int start,
            final int end,
            final Deque<int[]> opens) {
        if (i + 1 >= end || Character.isWhitespace(text.charAt(i + 1))) {
            return false;
        }
        if (i == start) {
            return true;
        }
        final char previous = text.charAt(i - 1);
        return Character.isWhitespace(previous)
                || isPendingOpeningDirective(opens, i - 1)
                || OPENING_BOUNDARY_PUNCTUATION.indexOf(previous) >= 0;
    }

    private static boolean isPendingOpeningDirective(final Deque<int[]> opens, final int position) {
        for (final int[] open : opens) {
            if (open[1] == position) {
                return true;
            }
        }
        return false;
    }

    private static boolean isFenceStart(final CharSequence text, final int start, final int end) {
        return end - start >= FENCE.length()
                && text.charAt(start) == '`'
                && text.charAt(start + 1) == '`'
                && text.charAt(start + 2) == '`';
    }

    private static boolean isFenceOnly(final CharSequence text, final int start, final int end) {
        int s = start;
        int e = end;
        while (s < e && Character.isWhitespace(text.charAt(s))) {
            ++s;
        }
        while (e > s && Character.isWhitespace(text.charAt(e - 1))) {
            --e;
        }
        return e - s == FENCE.length() && isFenceStart(text, s, e);
    }

    private static int lineEnd(final CharSequence text, final int start, final int end) {
        int i = start;
        while (i < end && text.charAt(i) != '\n') {
            ++i;
        }
        return i;
    }

    private static Type typeFor(final char c) {
        return switch (c) {
            case '*' -> Type.BOLD;
            case '_' -> Type.ITALIC;
            case '~' -> Type.STRIKETHROUGH;
            case '`' -> Type.MONOSPACE;
            default -> throw new IllegalArgumentException("unknown styling directive " + c);
        };
    }

    private static ParcelableSpan spanForType(final Type type) {
        return switch (type) {
            case BOLD -> new StyleSpan(Typeface.BOLD);
            case ITALIC -> new StyleSpan(Typeface.ITALIC);
            case STRIKETHROUGH -> new StrikethroughSpan();
            case MONOSPACE, PRE -> new TypefaceSpan("monospace");
            case QUOTE -> null;
        };
    }

    private static void makeDirectiveOpaque(
            final Spannable editable,
            final int start,
            final int end,
            @ColorInt final int fallbackTextColor) {
        if (start >= end) {
            return;
        }
        final QuoteSpan[] quoteSpans = editable.getSpans(start, end, QuoteSpan.class);
        @ColorInt
        final int textColor = quoteSpans.length > 0 ? quoteSpans[0].getColor() : fallbackTextColor;
        editable.setSpan(
                new ForegroundColorSpan(transformColor(textColor)),
                start,
                end,
                Spanned.SPAN_EXCLUSIVE_EXCLUSIVE);
    }

    private static @ColorInt int transformColor(@ColorInt final int color) {
        return Color.argb(
                Math.round(Color.alpha(color) * 0.45f),
                Color.red(color),
                Color.green(color),
                Color.blue(color));
    }

    public static class Style {

        private final Type type;
        private final int start;
        private final int end;
        private final int contentStart;
        private final int contentEnd;

        private Style(
                final Type type,
                final int start,
                final int end,
                final int contentStart,
                final int contentEnd) {
            this.type = type;
            this.start = start;
            this.end = end;
            this.contentStart = contentStart;
            this.contentEnd = contentEnd;
        }

        public Type getType() {
            return type;
        }

        public int getStart() {
            return start;
        }

        public int getEnd() {
            return end;
        }

        public int getContentStart() {
            return contentStart;
        }

        public int getContentEnd() {
            return contentEnd;
        }

        public String getContent(final CharSequence text) {
            return text.subSequence(contentStart, contentEnd).toString();
        }
    }
}
