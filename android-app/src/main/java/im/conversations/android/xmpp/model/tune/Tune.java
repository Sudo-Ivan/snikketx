package im.conversations.android.xmpp.model.tune;

import com.google.common.base.Strings;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;

@XmlElement
public class Tune extends Extension {

    public Tune() {
        super(Tune.class);
    }

    public Tune(final String artist, final String title) {
        this();
        setArtist(artist);
        setTitle(title);
    }

    public String getArtist() {
        return Strings.emptyToNull(this.findChildContent("artist"));
    }

    public void setArtist(final String artist) {
        setChildContent("artist", artist);
    }

    public String getTitle() {
        return Strings.emptyToNull(this.findChildContent("title"));
    }

    public void setTitle(final String title) {
        setChildContent("title", title);
    }

    public String getSource() {
        return Strings.emptyToNull(this.findChildContent("source"));
    }

    public void setSource(final String source) {
        setChildContent("source", source);
    }

    public String getTrack() {
        return Strings.emptyToNull(this.findChildContent("track"));
    }

    public void setTrack(final String track) {
        setChildContent("track", track);
    }

    public String getUri() {
        return Strings.emptyToNull(this.findChildContent("uri"));
    }

    public void setUri(final String uri) {
        setChildContent("uri", uri);
    }

    public String getDescription() {
        final var artist = getArtist();
        final var title = getTitle();
        if (!Strings.isNullOrEmpty(artist) && !Strings.isNullOrEmpty(title)) {
            return artist + " - " + title;
        } else if (!Strings.isNullOrEmpty(title)) {
            return title;
        } else if (!Strings.isNullOrEmpty(artist)) {
            return artist;
        }
        final var source = getSource();
        return Strings.isNullOrEmpty(source) ? null : source;
    }

    private void setChildContent(final String name, final String content) {
        if (!Strings.isNullOrEmpty(content)) {
            this.addChild(name).setContent(content);
        }
    }
}
