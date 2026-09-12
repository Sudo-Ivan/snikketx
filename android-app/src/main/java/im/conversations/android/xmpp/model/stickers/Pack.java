package im.conversations.android.xmpp.model.stickers;

import com.google.common.base.Strings;
import com.google.common.collect.ImmutableList;
import eu.siacs.conversations.xml.Element;
import eu.siacs.conversations.xml.Namespace;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;
import java.util.Collection;
import java.util.List;

@XmlElement
public class Pack extends Extension {

    public Pack() {
        super(Pack.class);
    }

    public String getPackName() {
        return Strings.emptyToNull(findChildContent("name"));
    }

    public String getPackSummary() {
        return Strings.emptyToNull(findChildContent("summary"));
    }

    public boolean isRestricted() {
        return findChild("restricted") != null;
    }

    public Collection<Element> getItems() {
        final ImmutableList.Builder<Element> items = ImmutableList.builder();
        for (final Element child : getChildren()) {
            if ("item".equals(child.getName())) {
                items.add(child);
            }
        }
        return items.build();
    }

    public static Element fileMetadata(final Element item) {
        return item == null ? null : item.findChild("file", Namespace.FILE_METADATA);
    }

    public static List<String> sourceUrls(final Element item) {
        if (item == null) {
            return List.of();
        }
        final var sources = item.findChild("sources", Namespace.SFS);
        if (sources == null) {
            return List.of();
        }
        final ImmutableList.Builder<String> urls = ImmutableList.builder();
        for (final Element source : sources.getChildren()) {
            final String target = source.getAttribute("target");
            if ("url-data".equals(source.getName())
                    && !Strings.isNullOrEmpty(target)
                    && (target.startsWith("https://") || target.startsWith("http://"))) {
                urls.add(target);
            }
        }
        return urls.build();
    }
}
