package im.conversations.android.xmpp.model.folders;

import eu.siacs.conversations.xmpp.Jid;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;
import org.jspecify.annotations.Nullable;

@XmlElement(name = "conversation")
public class ConversationEntry extends Extension {

    public ConversationEntry() {
        super(ConversationEntry.class);
    }

    @Nullable
    public Jid getJid() {
        return getAttributeAsJid("jid");
    }

    @Nullable
    public String getFolderName() {
        return getAttribute("name");
    }

    public void setJid(final Jid jid) {
        setAttribute("jid", jid);
    }

    public void setName(final String name) {
        setAttribute("name", name);
    }
}
