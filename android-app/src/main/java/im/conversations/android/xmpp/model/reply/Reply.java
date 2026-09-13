package im.conversations.android.xmpp.model.reply;

import com.google.common.base.Strings;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;

@XmlElement(namespace = Namespace.REPLY)
public class Reply extends Extension {

    public Reply() {
        super(Reply.class);
    }

    public String getId() {
        return Strings.emptyToNull(this.getAttribute("id"));
    }

    public Jid getTo() {
        final var to = Strings.emptyToNull(this.getAttribute("to"));
        if (to == null) {
            return null;
        }
        try {
            return Jid.of(to);
        } catch (final IllegalArgumentException e) {
            return null;
        }
    }

    public void setId(final String id) {
        this.setAttribute("id", id);
    }

    public void setTo(final Jid to) {
        this.setAttribute("to", to);
    }
}
