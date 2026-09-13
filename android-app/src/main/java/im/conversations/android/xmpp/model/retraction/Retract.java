package im.conversations.android.xmpp.model.retraction;

import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;
import im.conversations.android.xmpp.model.moderation.Moderated;

@XmlElement
public class Retract extends Extension {

    public Retract() {
        super(Retract.class);
    }

    public Retract(final String id) {
        this();
        this.setId(id);
    }

    public String getId() {
        return this.getAttribute("id");
    }

    public void setId(final String id) {
        this.setAttribute("id", id);
    }

    public Moderated getModerated() {
        return this.getExtension(Moderated.class);
    }
}
