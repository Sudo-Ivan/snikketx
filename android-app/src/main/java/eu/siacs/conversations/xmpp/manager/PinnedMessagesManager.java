package eu.siacs.conversations.xmpp.manager;

import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.xmpp.XmppConnection;
import java.time.Instant;
import java.util.List;
import org.jspecify.annotations.Nullable;

/**
 * Tracks pinned messages for a conversation.
 *
 * <p>Pins are currently stored locally in the conversation attributes. There is no ratified or
 * experimental XEP for MUC message pinning yet (XEP-0469 covers bookmark pinning only). Once a
 * protocol lands (e.g. a fastening based pin broadcast or a MUC pubsub node), the pin() and unpin()
 * methods are the places to also emit and, for incoming stanzas, apply wire level updates.
 */
public class PinnedMessagesManager extends AbstractManager {

    private final XmppConnectionService service;

    public PinnedMessagesManager(
            final XmppConnectionService service, final XmppConnection connection) {
        super(service.getApplicationContext(), connection);
        this.service = service;
    }

    /**
     * Whether the current user may pin messages in this conversation. Pinning is currently limited
     * to group chats the user is participating in. When a wire protocol gets added this check
     * should be tightened to whatever the protocol allows (e.g. moderators only).
     */
    public boolean canPin(final Conversation conversation) {
        return conversation.getMode() == Conversational.MODE_MULTI
                && conversation.getMucOptions().participating();
    }

    public List<Conversation.PinnedMessage> getPinnedMessages(final Conversation conversation) {
        return conversation.getPinnedMessages();
    }

    public boolean isPinned(final Conversation conversation, final Message message) {
        return conversation.isMessagePinned(message.getUuid());
    }

    /**
     * Resolves a pinned message reference to an in-memory message. Returns null when the message is
     * not loaded (for example because the conversation has been trimmed).
     */
    @Nullable
    public Message resolve(
            final Conversation conversation, final Conversation.PinnedMessage pinnedMessage) {
        final var message = conversation.findMessageWithUuid(pinnedMessage.uuid());
        if (message != null) {
            return message;
        }
        final var serverMsgId = pinnedMessage.serverMsgId();
        if (serverMsgId != null) {
            return conversation.findMessageWithServerMsgId(serverMsgId);
        }
        return null;
    }

    public void pin(final Conversation conversation, final Message message, final String preview) {
        conversation.pinMessage(
                new Conversation.PinnedMessage(
                        message.getUuid(), message.getServerMsgId(), preview, Instant.now()));
        this.service.updateConversation(conversation);
        // TODO(protocol): broadcast a pin stanza to the MUC once a pinning protocol exists
    }

    public void unpin(final Conversation conversation, final Message message) {
        if (conversation.unpinMessage(message.getUuid())) {
            this.service.updateConversation(conversation);
        }
        // TODO(protocol): broadcast an unpin stanza to the MUC once a pinning protocol exists
    }
}
