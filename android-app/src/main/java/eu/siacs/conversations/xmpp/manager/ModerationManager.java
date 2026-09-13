package eu.siacs.conversations.xmpp.manager;

import android.util.Log;
import com.google.common.base.Strings;
import com.google.common.collect.Collections2;
import com.google.common.collect.ImmutableSet;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.XmppConnection;
import im.conversations.android.xmpp.model.fallback.Fallback;
import im.conversations.android.xmpp.model.hints.Store;
import im.conversations.android.xmpp.model.moderation.Moderate;
import im.conversations.android.xmpp.model.retraction.Retract;
import im.conversations.android.xmpp.model.stanza.Iq;
import im.conversations.android.xmpp.model.stanza.Message;
import java.util.Collection;
import java.util.List;
import java.util.UUID;

public class ModerationManager extends AbstractManager {

    private static final String RETRACTION_FALLBACK =
            "This person attempted to retract a previous message, but it's unsupported by your"
                    + " client.";

    private final XmppConnectionService service;

    public ModerationManager(final XmppConnectionService service, final XmppConnection connection) {
        super(service.getApplicationContext(), connection);
        this.service = service;
    }

    public ListenableFuture<Void> moderate(final eu.siacs.conversations.entities.Message message) {
        final var serverMsgId = message.getServerMsgId();
        final var previous = message.getEditedServerMessageIds();
        if (!previous.isEmpty()) {
            Log.d(
                    Config.LOGTAG,
                    getAccount().getJid()
                            + ": requesting deletion of previous stanza-ids: "
                            + previous);
        }
        final var serverMsgIds =
                new ImmutableSet.Builder<String>().add(serverMsgId).addAll(previous).build();
        final var conversation = message.getConversation();
        final var address = conversation.getAddress().asBareJid();
        final var future = moderate(address, serverMsgIds);
        return Futures.transform(
                future,
                result -> {
                    if (message.getConversation() instanceof Conversation c) {
                        c.remove(message);
                        if (getDatabase().deleteMessage(message.getUuid())) {
                            Log.d(Config.LOGTAG, "deleted local copy of moderated message");
                            deleteAssociatedFile(message);
                        }
                        this.service.updateConversationUi();
                        return null;
                    } else {
                        throw new IllegalStateException("Message was not part of conversation");
                    }
                },
                MoreExecutors.directExecutor());
    }

    private ListenableFuture<List<Iq>> moderate(final Jid address, final Collection<String> ids) {
        final var futures =
                Collections2.transform(
                        ids,
                        id -> {
                            final var iq = new Iq(Iq.Type.SET);
                            iq.setTo(address);
                            final var moderate = iq.addExtension(new Moderate(id));
                            moderate.addExtension(new Retract());
                            return this.connection.sendIqPacket(iq);
                        });
        return Futures.allAsList(futures);
    }

    /**
     * Sends a XEP-0424 message retraction for a message we sent. In group chats this references the
     * stanza-id assigned by the MUC; everywhere else the id of the original message stanza is used.
     */
    public boolean retract(final eu.siacs.conversations.entities.Message message) {
        final var conversational = message.getConversation();
        if (!(conversational instanceof Conversation conversation)) {
            return false;
        }
        final var packet = new Message();
        final String retractId;
        if (conversation.getMode() == Conversational.MODE_MULTI && !message.isPrivateMessage()) {
            packet.setType(Message.Type.GROUPCHAT);
            packet.setTo(conversation.getAddress().asBareJid());
            retractId =
                    Strings.isNullOrEmpty(message.getServerMsgId())
                            ? message.getMessageId()
                            : message.getServerMsgId();
        } else {
            packet.setType(Message.Type.CHAT);
            if (message.isPrivateMessage()) {
                packet.setTo(message.getCounterpart());
                packet.addChild("x", Namespace.MUC_USER);
            } else {
                packet.setTo(conversation.getAddress().asBareJid());
            }
            retractId = message.getMessageId();
        }
        if (packet.getTo() == null || Strings.isNullOrEmpty(retractId)) {
            Log.e(Config.LOGTAG, "could not find id to retract");
            return false;
        }
        packet.setId(UUID.randomUUID().toString());
        packet.addExtension(new Retract(retractId));
        final var fallback = packet.addExtension(new Fallback());
        fallback.setAttribute("for", Namespace.RETRACTION);
        packet.setBody(RETRACTION_FALLBACK);
        packet.addExtension(new Store());
        this.connection.sendMessagePacket(packet);
        applyRetraction(conversation, message);
        return true;
    }

    public void handleRetraction(final Message packet, final Jid counterpart) {
        final var retraction = packet.getExtension(Retract.class);
        if (retraction == null) {
            return;
        }
        if (packet.getType() == Message.Type.GROUPCHAT) {
            handleGroupChatRetraction(packet, retraction);
        } else {
            handleChatRetraction(packet, counterpart, retraction);
        }
    }

    private void handleGroupChatRetraction(final Message message, final Retract retraction) {
        final var account = getAccount();
        final var from = Jid.Invalid.getNullForInvalid(message.getFrom());
        if (from == null) {
            Log.d(Config.LOGTAG, "received retraction without valid from");
            return;
        }
        final var mucOptions = getManager(MultiUserChatManager.class).getState(from.asBareJid());
        if (mucOptions == null) {
            Log.d(
                    Config.LOGTAG,
                    account.getJid().asBareJid() + ": received retraction in MUC w/o state");
            return;
        }
        if (mucOptions.isPrivateAndNonAnonymous()) {
            Log.d(
                    Config.LOGTAG,
                    account.getJid().asBareJid()
                            + ": retractions are only supported in public channels");
            return;
        }
        final var stanzaId = retraction.getId();
        if (stanzaId == null) {
            Log.d(Config.LOGTAG, "retraction was missing stanza-id");
            return;
        }
        final var conversation = mucOptions.getConversation();
        final eu.siacs.conversations.entities.Message retractedMessage;
        final var inMemoryMessage = conversation.findMessageWithServerMsgId(stanzaId);
        if (inMemoryMessage != null) {
            retractedMessage = inMemoryMessage;
        } else {
            retractedMessage = getDatabase().getMessageWithServerMsgId(conversation, stanzaId);
        }
        if (retractedMessage == null) {
            Log.d(Config.LOGTAG, "received retraction for " + stanzaId + ". Message not found.");
            return;
        }
        final var moderated = retraction.getModerated();
        final var by = moderated == null ? null : moderated.getBy();
        if (from.isFullJid()) {
            // an occupant (not the service) asks to retract; only honor retractions issued by the
            // author of the referenced message
            final boolean sentByAuthor;
            if (mucOptions.isSelf(from)) {
                sentByAuthor =
                        retractedMessage.getStatus()
                                != eu.siacs.conversations.entities.Message.STATUS_RECEIVED;
            } else {
                sentByAuthor = from.equals(retractedMessage.getCounterpart());
            }
            if (!sentByAuthor) {
                Log.d(
                        Config.LOGTAG,
                        "ignoring retraction from " + from + " for a message of another author");
                return;
            }
            Log.d(Config.LOGTAG, "received self retraction for " + stanzaId + " in " + from);
            applyRetraction(conversation, retractedMessage);
            return;
        }
        conversation.remove(retractedMessage);
        this.service.getNotificationService().clear(retractedMessage);
        if (getDatabase().deleteMessage(retractedMessage.getUuid())) {
            Log.d(
                    Config.LOGTAG,
                    "received retraction for " + stanzaId + " in " + from + " by " + by);
            deleteAssociatedFile(retractedMessage);
        }
        this.service.updateConversationUi();
    }

    private void handleChatRetraction(
            final Message packet, final Jid counterpart, final Retract retraction) {
        final var account = getAccount();
        final var from = Jid.Invalid.getNullForInvalid(packet.getFrom());
        final var id = retraction.getId();
        if (from == null || counterpart == null || Strings.isNullOrEmpty(id)) {
            Log.d(Config.LOGTAG, "received invalid retraction. id=" + id);
            return;
        }
        final var conversation = this.service.find(account, counterpart.asBareJid());
        if (conversation == null) {
            Log.d(
                    Config.LOGTAG,
                    account.getJid().asBareJid()
                            + ": received retraction for unknown conversation "
                            + counterpart);
            return;
        }
        final eu.siacs.conversations.entities.Message retractedMessage;
        final var inMemoryMessage = conversation.findMessageWithUuidOrRemoteId(id, null, null);
        if (inMemoryMessage != null) {
            retractedMessage = inMemoryMessage;
        } else {
            retractedMessage = getDatabase().getMessageWithUuidOrRemoteId(conversation, id);
        }
        if (retractedMessage == null) {
            Log.d(Config.LOGTAG, "received retraction for " + id + ". Message not found.");
            return;
        }
        if (packet.fromAccount(account)) {
            // a carbon or archive copy of our own retraction may only touch our own messages
            if (retractedMessage.getStatus()
                    == eu.siacs.conversations.entities.Message.STATUS_RECEIVED) {
                Log.d(Config.LOGTAG, "ignoring retraction of a message that is not ours");
                return;
            }
        } else {
            // retractions coming from a contact may only touch messages that contact sent
            if (retractedMessage.getStatus()
                    != eu.siacs.conversations.entities.Message.STATUS_RECEIVED) {
                Log.d(Config.LOGTAG, "ignoring retraction of a message that is ours");
                return;
            }
            if (conversation.getMode() == Conversational.MODE_MULTI
                    && !from.equals(retractedMessage.getCounterpart())) {
                Log.d(
                        Config.LOGTAG,
                        "ignoring retraction from " + from + " for a message of another author");
                return;
            }
        }
        Log.d(
                Config.LOGTAG,
                account.getJid().asBareJid() + ": received retraction for " + id + " from " + from);
        applyRetraction(conversation, retractedMessage);
    }

    /**
     * Turns a message into a retraction tombstone; clears body, edit history and the file
     * reference, removes the notification and persists the change.
     */
    public void applyRetraction(
            final Conversation conversation,
            final eu.siacs.conversations.entities.Message message) {
        if (message.isRetracted()) {
            return;
        }
        // capture the file reference before the tombstone clears it so the file can be deleted
        // once the updated row no longer points at it
        final var storageLocation = message.getRelativeFilePath();
        final var file = storageLocation == null ? null : storageLocation.file();
        if (message.getEncryption() == eu.siacs.conversations.entities.Message.ENCRYPTION_PGP
                && conversation.getAccount().getPgpDecryptionService() != null) {
            conversation.getAccount().getPgpDecryptionService().discard(message);
        }
        message.markRetracted();
        this.service.evictPreview(message.getUuid());
        this.service.getNotificationService().clear(message);
        if (getDatabase().updateMessage(message, true)) {
            Log.d(Config.LOGTAG, "marked message " + message.getUuid() + " as retracted");
        }
        if (file != null) {
            deleteFileIfOrphaned(file);
        }
        this.service.updateConversationUi();
    }

    private void deleteAssociatedFile(final eu.siacs.conversations.entities.Message message) {
        if (message.isFileOrImage()) {
            final var storageLocation = message.getRelativeFilePath();
            if (storageLocation == null) {
                return;
            }
            deleteFileIfOrphaned(storageLocation.file());
        }
    }

    private void deleteFileIfOrphaned(final java.io.File file) {
        final var messagesWithFile = getDatabase().getMessagesWithFile(file);
        if (messagesWithFile.isEmpty() && file.exists()) {
            synchronized (service.FILENAMES_TO_IGNORE_DELETION) {
                service.FILENAMES_TO_IGNORE_DELETION.add(file.getAbsolutePath());
            }
            if (file.delete()) {
                Log.d(Config.LOGTAG, "deleted associated file");
            }
        }
    }
}
