package im.conversations.android.xmpp.model.mood;

import com.google.common.base.Strings;
import eu.siacs.conversations.xml.Element;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;

@XmlElement
public class Mood extends Extension {

    public Mood() {
        super(Mood.class);
    }

    public Mood(final String mood, final String text) {
        this();
        setMood(mood);
        setText(text);
    }

    public String getMood() {
        for (final Element child : getChildren()) {
            if (!"text".equals(child.getName())) {
                return child.getName();
            }
        }
        return null;
    }

    public void setMood(final String mood) {
        if (!Strings.isNullOrEmpty(mood)) {
            this.addChild(mood);
        }
    }

    public String getText() {
        return Strings.emptyToNull(this.findChildContent("text"));
    }

    public void setText(final String text) {
        if (!Strings.isNullOrEmpty(text)) {
            this.addChild("text").setContent(text);
        }
    }
}
