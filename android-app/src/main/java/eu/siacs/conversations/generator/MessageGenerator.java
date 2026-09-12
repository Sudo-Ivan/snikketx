package eu.siacs.conversations.generator;

import android.util.Base64;
import com.google.common.hash.Hashing;
import eu.siacs.conversations.crypto.axolotl.AxolotlService;
import eu.siacs.conversations.crypto.axolotl.XmppAxolotlMessage;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.xml.Element;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import im.conversations.android.xmpp.model.correction.Replace;
import im.conversations.android.xmpp.model.hints.Store;
import im.conversations.android.xmpp.model.markers.Markable;
import im.conversations.android.xmpp.model.stickers.Sticker;
import im.conversations.android.xmpp.model.unique.OriginId;

public class MessageGenerator extends AbstractGenerator {
    private static final String OMEMO_FALLBACK_MESSAGE =
            "I sent you an OMEMO encrypted message but your client doesn’t seem to support that."
                    + " Find more information on https://conversations.im/omemo";
    private static final String PGP_FALLBACK_MESSAGE =
            "I sent you a PGP encrypted message but your client doesn’t seem to support that.";

    public MessageGenerator(XmppConnectionService service) {
        super(service);
    }

    private im.conversations.android.xmpp.model.stanza.Message preparePacket(
            final Message message) {
        Conversation conversation = (Conversation) message.getConversation();
        Account account = conversation.getAccount();
        im.conversations.android.xmpp.model.stanza.Message packet =
                new im.conversations.android.xmpp.model.stanza.Message();
        final boolean isWithSelf = conversation.getContact().isSelf();
        if (conversation.getMode() == Conversation.MODE_SINGLE) {
            packet.setTo(message.getCounterpart());
            packet.setType(im.conversations.android.xmpp.model.stanza.Message.Type.CHAT);
            if (!isWithSelf) {
                packet.addChild("request", "urn:xmpp:receipts");
            }
        } else if (message.isPrivateMessage()) {
            packet.setTo(message.getCounterpart());
            packet.setType(im.conversations.android.xmpp.model.stanza.Message.Type.CHAT);
            packet.addChild("x", "http://jabber.org/protocol/muc#user");
            packet.addChild("request", "urn:xmpp:receipts");
        } else {
            packet.setTo(message.getCounterpart().asBareJid());
            packet.setType(im.conversations.android.xmpp.model.stanza.Message.Type.GROUPCHAT);
        }
        if (conversation.isSingleOrPrivateAndNonAnonymous() && !message.isPrivateMessage()) {
            packet.addExtension(new Markable());
        }
        packet.setFrom(account.getJid());
        packet.setId(message.getUuid());
        if (conversation.getMode() == Conversational.MODE_MULTI
                && !message.isPrivateMessage()
                && !conversation.getMucOptions().stableId()) {
            packet.addExtension(new OriginId(message.getUuid()));
        }
        if (message.edited()) {
            packet.addExtension(new Replace(message.getMessageId()));
        }
        return packet;
    }

    public im.conversations.android.xmpp.model.stanza.Message generateAxolotlChat(
            Message message, XmppAxolotlMessage axolotlMessage) {
        im.conversations.android.xmpp.model.stanza.Message packet = preparePacket(message);
        if (axolotlMessage == null) {
            return null;
        }
        packet.setAxolotlMessage(axolotlMessage.toElement());
        packet.setBody(OMEMO_FALLBACK_MESSAGE);
        addStickerElement(packet, message);
        packet.addExtension(new Store());
        packet.addChild("encryption", "urn:xmpp:eme:0")
                .setAttribute("name", "OMEMO")
                .setAttribute("namespace", AxolotlService.PEP_PREFIX);
        return packet;
    }

    public im.conversations.android.xmpp.model.stanza.Message generateKeyTransportMessage(
            Jid to, XmppAxolotlMessage axolotlMessage) {
        im.conversations.android.xmpp.model.stanza.Message packet =
                new im.conversations.android.xmpp.model.stanza.Message();
        packet.setType(im.conversations.android.xmpp.model.stanza.Message.Type.CHAT);
        packet.setTo(to);
        packet.setAxolotlMessage(axolotlMessage.toElement());
        packet.addChild(new Store());
        return packet;
    }

    public im.conversations.android.xmpp.model.stanza.Message generateChat(Message message) {
        im.conversations.android.xmpp.model.stanza.Message packet = preparePacket(message);
        String content;
        if (message.hasFileOnRemoteHost()) {
            final Message.FileParams fileParams = message.getFileParams();
            content = fileParams.url;
            packet.addChild("x", Namespace.OOB).addChild("url").setContent(content);
            if (addStickerElement(packet, message)) {
                packet.addChild(fileSharingElement(message, content));
            }
        } else {
            content = message.getBody();
        }
        packet.setBody(content);
        return packet;
    }

    public im.conversations.android.xmpp.model.stanza.Message generatePgpChat(Message message) {
        final im.conversations.android.xmpp.model.stanza.Message packet = preparePacket(message);
        if (message.hasFileOnRemoteHost()) {
            Message.FileParams fileParams = message.getFileParams();
            final String url = fileParams.url;
            packet.setBody(url);
            packet.addChild("x", Namespace.OOB).addChild("url").setContent(url);
            addStickerElement(packet, message);
        } else {
            packet.setBody(PGP_FALLBACK_MESSAGE);
            if (message.getEncryption() == Message.ENCRYPTION_DECRYPTED) {
                packet.addChild("x", "jabber:x:encrypted").setContent(message.getEncryptedBody());
            } else if (message.getEncryption() == Message.ENCRYPTION_PGP) {
                packet.addChild("x", "jabber:x:encrypted").setContent(message.getBody());
            }
            packet.addChild("encryption", "urn:xmpp:eme:0")
                    .setAttribute("namespace", "jabber:x:encrypted");
        }
        return packet;
    }

    /**
     * Adds the XEP-0449 sticker element to the packet. Returns true if the message is a sticker.
     * Note that for encrypted messages the sticker element is added in plain text; the file itself
     * stays protected by the encrypted payload or transport security.
     */
    private static boolean addStickerElement(
            final im.conversations.android.xmpp.model.stanza.Message packet,
            final Message message) {
        if (!message.isSticker()) {
            return false;
        }
        final var sticker = new Sticker();
        sticker.setPack(message.getStickerPackId());
        packet.addExtension(sticker);
        return true;
    }

    /**
     * Builds a XEP-0447 stateless file sharing element describing the uploaded sticker. Only used
     * for unencrypted messages where the http(s) url is safe to publish in clear text.
     */
    private Element fileSharingElement(final Message message, final String url) {
        final var sharing = new Element("file-sharing", Namespace.SFS);
        final var file = sharing.addChild("file", Namespace.FILE_METADATA);
        final String mime = message.getMimeType();
        if (mime != null) {
            file.addChild("media-type").setContent(mime);
        }
        final String description = message.getStickerDescription();
        if (description != null) {
            file.addChild("desc").setContent(description);
        }
        final var params = message.getFileParams();
        if (params.size != null) {
            file.addChild("size").setContent(String.valueOf(params.size));
        }
        if (params.width > 0 && params.height > 0) {
            file.addChild("dimensions").setContent(params.width + "x" + params.height);
        }
        final String hash = sha256Base64(message);
        if (hash != null) {
            file.addChild("hash", Namespace.HASHES)
                    .setAttribute("algo", "sha-256")
                    .setContent(hash);
        }
        sharing.addChild("sources")
                .addChild("url-data", Namespace.URL_DATA)
                .setAttribute("target", url);
        return sharing;
    }

    private String sha256Base64(final Message message) {
        try {
            final var file = mXmppConnectionService.getFileBackend().getFile(message);
            if (file == null || !file.isFile()) {
                return null;
            }
            final var hash =
                    com.google.common.io.Files.asByteSource(file).hash(Hashing.sha256()).asBytes();
            return Base64.encodeToString(hash, Base64.NO_WRAP);
        } catch (final Exception e) {
            return null;
        }
    }
}
