package im.conversations.android.xmpp.model.fallback;

import com.google.common.base.Optional;
import eu.siacs.conversations.xml.LocalizedContent;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.ExtensionFactory;
import im.conversations.android.xmpp.model.Extension;
import im.conversations.android.xmpp.model.stanza.Message;

@XmlElement
public class Fallback extends Extension {
    public Fallback() {
        super(Fallback.class);
    }

    public String getFor() {
        return this.getAttribute("for");
    }

    public static Optional<Range> get(
            final Message message,
            final Class<? extends Extension> extension,
            final Class<? extends Element> element) {
        final var id = ExtensionFactory.id(extension);
        if (id == null) {
            throw new IllegalArgumentException(
                    String.format("%s is not a registered extension", extension.getName()));
        }
        for (final var fallback : message.getExtensions(Fallback.class)) {
            if (id.namespace().equals(fallback.getFor())) {
                if (fallback.isNoChildren()) {
                    return Optional.of(new FullRange());
                }
                final var e = fallback.getExtension(element);
                if (e != null) {
                    return Optional.of(e.getRange());
                }
            }
        }
        return Optional.absent();
    }

    private boolean isNoChildren() {
        return this.getExtensions(Body.class).isEmpty()
                && this.getExtensions(Subject.class).isEmpty();
    }

    public sealed interface Range permits StartEndRange, FullRange {
        boolean isEntire(final LocalizedContent content);

        /** Returns the content with the indicated fallback range removed. */
        String removeFrom(final String content);

        /** Returns the part of the content covered by the fallback range. */
        String substringOf(final String content);
    }

    private static int toIndex(final String content, final int codePointOffset) {
        final var total = content.codePointCount(0, content.length());
        final var clamped = Math.max(0, Math.min(codePointOffset, total));
        return content.offsetByCodePoints(0, clamped);
    }

    private record StartEndRange(int start, int end) implements Range {
        @Override
        public boolean isEntire(final LocalizedContent content) {
            // start and end are code point offsets while length() counts UTF-16 units;
            // convert before comparing so astral characters do not skew the check
            return start == 0 && toIndex(content.content, end) >= content.content.length() - 1;
        }

        @Override
        public String removeFrom(final String content) {
            final var s = toIndex(content, start);
            final var e = Math.max(s, toIndex(content, end));
            return content.substring(0, s) + content.substring(e);
        }

        @Override
        public String substringOf(final String content) {
            final var s = toIndex(content, start);
            final var e = Math.max(s, toIndex(content, end));
            return content.substring(s, e);
        }
    }

    private record FullRange() implements Range {

        @Override
        public boolean isEntire(LocalizedContent content) {
            return true;
        }

        @Override
        public String removeFrom(final String content) {
            return "";
        }

        @Override
        public String substringOf(final String content) {
            return content;
        }
    }
    ;

    public abstract static sealed class Element extends Extension permits Body, Subject {

        public Element(Class<? extends Extension> clazz) {
            super(clazz);
        }

        public Range getRange() {
            final var start = this.getOptionalIntAttribute("start");
            final var end = this.getOptionalIntAttribute("end");
            if (start.isPresent() && end.isPresent()) {
                return new StartEndRange(start.get(), end.get());
            } else {
                return new FullRange();
            }
        }
    }
}
