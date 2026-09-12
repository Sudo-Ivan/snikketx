package im.conversations.android.xmpp.model.folders;

import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;
import java.util.Collection;

@XmlElement
public class Folders extends Extension {

    public Folders() {
        super(Folders.class);
    }

    public Collection<ConversationEntry> getConversations() {
        return getExtensions(ConversationEntry.class);
    }

    public ConversationEntry addConversation() {
        return addExtension(new ConversationEntry());
    }
}
