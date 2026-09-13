package eu.siacs.conversations.parser;

import android.content.Context;
import android.os.Build;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.util.Log;
import android.util.Pair;
import com.google.common.base.Strings;
import com.google.common.primitives.Longs;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.crypto.axolotl.AxolotlService;
import eu.siacs.conversations.crypto.axolotl.BrokenSessionException;
import eu.siacs.conversations.crypto.axolotl.NotEncryptedForThisDeviceException;
import eu.siacs.conversations.crypto.axolotl.OutdatedSenderException;
import eu.siacs.conversations.crypto.axolotl.XmppAxolotlMessage;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.entities.Contact;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.entities.InReplyTo;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.http.HttpConnectionManager;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.utils.CryptoHelper;
import eu.siacs.conversations.utils.ReplyUtils;
import eu.siacs.conversations.xml.Element;
import eu.siacs.conversations.xml.LocalizedContent;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.XmppConnection;
import eu.siacs.conversations.xmpp.jingle.JingleRtpConnection;
import eu.siacs.conversations.xmpp.manager.ActivityManager;
import eu.siacs.conversations.xmpp.manager.ChatStateManager;
import eu.siacs.conversations.xmpp.manager.DeliveryReceiptManager;
import eu.siacs.conversations.xmpp.manager.DisplayedManager;
import eu.siacs.conversations.xmpp.manager.JingleManager;
import eu.siacs.conversations.xmpp.manager.JingleMessageManager;
import eu.siacs.conversations.xmpp.manager.MessageArchiveManager;
import eu.siacs.conversations.xmpp.manager.ModerationManager;
import eu.siacs.conversations.xmpp.manager.MultiUserChatManager;
import eu.siacs.conversations.xmpp.manager.PubSubManager;
import eu.siacs.conversations.xmpp.manager.ReactionManager;
import eu.siacs.conversations.xmpp.manager.RosterManager;
import eu.siacs.conversations.xmpp.manager.StanzaIdManager;
import im.conversations.android.xmpp.model.Extension;
import im.conversations.android.xmpp.model.axolotl.Encrypted;
import im.conversations.android.xmpp.model.axolotl.Payload;
import im.conversations.android.xmpp.model.carbons.Received;
import im.conversations.android.xmpp.model.carbons.Sent;
import im.conversations.android.xmpp.model.conference.DirectInvite;
import im.conversations.android.xmpp.model.correction.Replace;
import im.conversations.android.xmpp.model.fallback.Body;
import im.conversations.android.xmpp.model.fallback.Fallback;
import im.conversations.android.xmpp.model.forward.Forwarded;
import im.conversations.android.xmpp.model.jmi.JingleMessage;
import im.conversations.android.xmpp.model.mam.Result;
import im.conversations.android.xmpp.model.markers.Displayed;
import im.conversations.android.xmpp.model.markers.Markable;
import im.conversations.android.xmpp.model.muc.user.MucUser;
import im.conversations.android.xmpp.model.nick.Nick;
import im.conversations.android.xmpp.model.occupant.OccupantId;
import im.conversations.android.xmpp.model.oob.OutOfBandData;
import im.conversations.android.xmpp.model.pubsub.event.Event;
import im.conversations.android.xmpp.model.reactions.Reactions;
import im.conversations.android.xmpp.model.reply.Reply;
import im.conversations.android.xmpp.model.retraction.Retract;
import im.conversations.android.xmpp.model.retraction.Retracted;
import java.util.UUID;
import java.util.function.Consumer;

public class MessageParser extends AbstractParser
        implements Consumer<im.conversations.android.xmpp.model.stanza.Message> {

    public MessageParser(final XmppConnectionService service, final XmppConnection connection) {
        super(service, connection);
    }

    private Message parseAxolotlChat(
            final Encrypted axolotlMessage,
            final Jid from,
            final Conversation conversation,
            final int status,
            final boolean checkedForDuplicates,
            final boolean postpone) {
        final AxolotlService service = conversation.getAccount().getAxolotlService();
        final XmppAxolotlMessage xmppAxolotlMessage;
        try {
            xmppAxolotlMessage = XmppAxolotlMessage.fromElement(axolotlMessage, from.asBareJid());
        } catch (final Exception e) {
            Log.d(
                    Config.LOGTAG,
                    conversation.getAccount().getJid().asBareJid()
                            + ": invalid omemo message received "
                            + e.getMessage());
            return null;
        }
        if (xmppAxolotlMessage.hasPayload()) {
            final XmppAxolotlMessage.XmppAxolotlPlaintextMessage plaintextMessage;
            try {
                plaintextMessage =
                        service.processReceivingPayloadMessage(xmppAxolotlMessage, postpone);
            } catch (BrokenSessionException e) {
                if (checkedForDuplicates) {
                    if (service.trustedOrPreviouslyResponded(from.asBareJid())) {
                        service.reportBrokenSessionException(e, postpone);
                        return new Message(
                                conversation, "", Message.ENCRYPTION_AXOLOTL_FAILED, status);
                    } else {
                        Log.d(
                                Config.LOGTAG,
                                "ignoring broken session exception because contact was not"
                                        + " trusted");
                        return new Message(
                                conversation, "", Message.ENCRYPTION_AXOLOTL_FAILED, status);
                    }
                } else {
                    Log.d(
                            Config.LOGTAG,
                            "ignoring broken session exception because checkForDuplicates failed");
                    return null;
                }
            } catch (NotEncryptedForThisDeviceException e) {
                return new Message(
                        conversation, "", Message.ENCRYPTION_AXOLOTL_NOT_FOR_THIS_DEVICE, status);
            } catch (OutdatedSenderException e) {
                return new Message(conversation, "", Message.ENCRYPTION_AXOLOTL_FAILED, status);
            }
            if (plaintextMessage != null) {
                Message finishedMessage =
                        new Message(
                                conversation,
                                plaintextMessage.getPlaintext(),
                                Message.ENCRYPTION_AXOLOTL,
                                status);
                finishedMessage.setFingerprint(plaintextMessage.getFingerprint());
                Log.d(
                        Config.LOGTAG,
                        AxolotlService.getLogprefix(finishedMessage.getConversation().getAccount())
                                + " Received Message with session fingerprint: "
                                + plaintextMessage.getFingerprint());
                return finishedMessage;
            }
        } else {
            Log.d(
                    Config.LOGTAG,
                    conversation.getAccount().getJid().asBareJid()
                            + ": received OMEMO key transport message");
            service.processReceivingKeyTransportMessage(xmppAxolotlMessage, postpone);
        }
        return null;
    }

    private boolean handleErrorMessage(
            final Account account,
            final im.conversations.android.xmpp.model.stanza.Message packet) {
        if (packet.getType() == im.conversations.android.xmpp.model.stanza.Message.Type.ERROR) {
            if (packet.fromServer(account)) {
                final var forwarded =
                        getForwardedMessagePacket(packet, "received", Namespace.CARBONS);
                if (forwarded != null) {
                    return handleErrorMessage(account, forwarded.first);
                }
            }
            final Jid from = packet.getFrom();
            final String id = packet.getId();
            if (from != null && id != null) {
                if (id.startsWith(JingleRtpConnection.JINGLE_MESSAGE_PROPOSE_ID_PREFIX)) {
                    final String sessionId =
                            id.substring(
                                    JingleRtpConnection.JINGLE_MESSAGE_PROPOSE_ID_PREFIX.length());
                    getManager(JingleManager.class)
                            .updateProposedSessionDiscovered(
                                    from, sessionId, JingleManager.DeviceDiscoveryState.FAILED);
                    return true;
                }
                if (id.startsWith(JingleRtpConnection.JINGLE_MESSAGE_PROCEED_ID_PREFIX)) {
                    final String sessionId =
                            id.substring(
                                    JingleRtpConnection.JINGLE_MESSAGE_PROCEED_ID_PREFIX.length());
                    final String message = extractErrorMessage(packet);
                    getManager(JingleManager.class).failProceed(from, sessionId, message);
                    return true;
                }
                mXmppConnectionService.markMessage(
                        account,
                        from.asBareJid(),
                        id,
                        Message.STATUS_SEND_FAILED,
                        extractErrorMessage(packet));
                final Element error = packet.findChild("error");
                final boolean pingWorthyError =
                        error != null
                                && (error.hasChild("not-acceptable")
                                        || error.hasChild("remote-server-timeout")
                                        || error.hasChild("remote-server-not-found"));
                if (pingWorthyError) {
                    Conversation conversation = mXmppConnectionService.find(account, from);
                    if (conversation != null
                            && conversation.getMode() == Conversational.MODE_MULTI) {
                        if (getManager(MultiUserChatManager.class)
                                .getOrCreateState(conversation)
                                .online()) {
                            Log.d(
                                    Config.LOGTAG,
                                    account.getJid().asBareJid()
                                            + ": received ping worthy error for seemingly online"
                                            + " muc at "
                                            + from);
                            getManager(MultiUserChatManager.class).pingAndRejoin(conversation);
                        }
                    }
                }
            }
            return true;
        }
        return false;
    }

    @Override
    public void accept(final im.conversations.android.xmpp.model.stanza.Message original) {
        final var account = this.getAccount();
        if (handleErrorMessage(account, original)) {
            return;
        }
        final im.conversations.android.xmpp.model.stanza.Message packet;
        Long timestamp = null;
        boolean isCarbon = false;
        String serverMsgId = null;
        final var result = original.getExtension(Result.class);
        final String queryId = result == null ? null : result.getQueryId();
        final MessageArchiveManager.Query query =
                queryId == null ? null : getManager(MessageArchiveManager.class).findQuery(queryId);
        final boolean offlineMessagesRetrieved = connection.isOfflineMessagesRetrieved();
        if (query != null
                && getManager(MessageArchiveManager.class).validFrom(query, original.getFrom())) {
            final var f = result.getForwarded();
            final var stamp = f == null ? null : f.getStamp();
            final var m = f == null ? null : f.getMessage();
            if (stamp == null || m == null) {
                return;
            }

            timestamp = stamp.toEpochMilli();
            packet = m;
            serverMsgId = result.getId();
            query.incrementMessageCount();

            if (query.isImplausibleFrom(packet.getFrom())) {
                Log.d(Config.LOGTAG, "found implausible from in MUC MAM archive");
                return;
            }

            if (handleErrorMessage(account, packet)) {
                return;
            }
        } else if (query != null) {
            Log.d(
                    Config.LOGTAG,
                    account.getJid().asBareJid()
                            + ": received mam result with invalid from ("
                            + original.getFrom()
                            + ") or queryId ("
                            + queryId
                            + ")");
            return;
        } else if (original.fromServer(account)
                && original.getType()
                        != im.conversations.android.xmpp.model.stanza.Message.Type.GROUPCHAT) {
            Pair<im.conversations.android.xmpp.model.stanza.Message, Long> f;
            f = getForwardedMessagePacket(original, Received.class);
            f = f == null ? getForwardedMessagePacket(original, Sent.class) : f;
            packet = f != null ? f.first : original;
            if (handleErrorMessage(account, packet)) {
                return;
            }
            timestamp = f != null ? f.second : null;
            isCarbon = f != null;
        } else {
            packet = original;
        }

        if (timestamp == null) {
            timestamp =
                    AbstractParser.parseTimestamp(original, AbstractParser.parseTimestamp(packet));
        }
        final LocalizedContent body = packet.getBody();
        final Element mucUserElement = packet.findChild("x", Namespace.MUC_USER);
        final boolean isTypeGroupChat =
                packet.getType()
                        == im.conversations.android.xmpp.model.stanza.Message.Type.GROUPCHAT;
        final var encrypted =
                packet.getOnlyExtension(im.conversations.android.xmpp.model.pgp.Encrypted.class);
        final String pgpEncrypted = encrypted == null ? null : encrypted.getContent();

        final var oob = packet.getExtension(OutOfBandData.class);
        final String oobUrl = oob != null ? oob.getURL() : null;
        final Element stickerElement = findStickerElement(packet);
        final var replace = packet.getExtension(Replace.class);
        final var replacementId = replace == null ? null : replace.getId();
        final var axolotlEncrypted = packet.getOnlyExtension(Encrypted.class);
        final var reply = packet.getExtension(Reply.class);
        final var replyId = reply == null ? null : reply.getId();
        final Element spoilerElement = packet.findChild("spoiler", Namespace.SPOILER);
        final boolean hasVoiceMessage =
                packet.findChild("voice-message", Namespace.VOICE_MESSAGE) != null;
        // XEP-0224 asks for attention requests in delayed or archived stanzas to be ignored
        final boolean liveAttention =
                packet.findChild("attention", Namespace.ATTENTION) != null
                        && query == null
                        && !packet.hasChild("delay", "urn:xmpp:delay");
        // TODO this can probably be refactored to be final
        int status;
        final Jid counterpart;
        final Jid to = packet.getTo();
        final Jid from = packet.getFrom();
        final Element originId = packet.findChild("origin-id", Namespace.STANZA_IDS);
        final String remoteMsgId;
        if (originId != null && originId.getAttribute("id") != null) {
            remoteMsgId = originId.getAttribute("id");
        } else {
            remoteMsgId = packet.getId();
        }
        boolean notify = false;

        if (from == null || !Jid.Invalid.isValid(from) || !Jid.Invalid.isValid(to)) {
            Log.e(Config.LOGTAG, "encountered invalid message from='" + from + "' to='" + to + "'");
            return;
        }
        if (query != null && !query.muc() && isTypeGroupChat) {
            Log.e(
                    Config.LOGTAG,
                    account.getJid().asBareJid()
                            + ": received group chat ("
                            + from
                            + ") message on regular MAM request. skipping");
            return;
        }
        final boolean selfAddressed;
        if (packet.fromAccount(account)) {
            status = Message.STATUS_SEND;
            selfAddressed = to == null || account.getJid().asBareJid().equals(to.asBareJid());
            if (selfAddressed) {
                counterpart = from;
            } else {
                counterpart = to;
            }
        } else {
            status = Message.STATUS_RECEIVED;
            counterpart = from;
            selfAddressed = false;
        }

        if (packet.hasExtension(MucUser.class)
                && packet.getExtension(MucUser.class)
                        .hasExtension(im.conversations.android.xmpp.model.muc.user.Invite.class)) {
            if (getManager(MultiUserChatManager.class).handleMediatedInvite(packet)) {
                return;
            }
        }
        if (packet.hasExtension(DirectInvite.class)) {
            if (getManager(MultiUserChatManager.class).handleDirectInvite(packet)) {
                return;
            }
        }

        if (original.hasExtension(MucUser.class)) {
            if (getManager(MultiUserChatManager.class).handleStatusMessage(original)) {
                return;
            }
        }
        final boolean bodyIsFallback;
        if (body != null && packet.hasExtension(Reactions.class)) {
            final var range = Fallback.get(packet, Reactions.class, Body.class);
            bodyIsFallback = range.isPresent() && range.get().isEntire(body);
        } else if (body != null && packet.hasExtension(Retract.class)) {
            final var range = Fallback.get(packet, Retract.class, Body.class);
            bodyIsFallback = range.isPresent() && range.get().isEntire(body);
        } else {
            bodyIsFallback = false;
        }

        final Fallback.Range replyFallbackRange;
        if (replyId != null && body != null) {
            final var range = Fallback.get(packet, Reply.class, Body.class);
            replyFallbackRange = range.isPresent() ? range.get() : null;
        } else {
            replyFallbackRange = null;
        }
        // the body stripped of an XEP-0428 reply fallback quote; for pgp or omemo encrypted
        // messages the fallback applies to the decrypted payload instead of this body
        final String bodyContent;
        final String replyPreview;
        if (replyFallbackRange != null && axolotlEncrypted == null && pgpEncrypted == null) {
            replyPreview = ReplyUtils.unquote(replyFallbackRange.substringOf(body.content));
            bodyContent = replyFallbackRange.removeFrom(body.content).stripLeading();
        } else {
            replyPreview = null;
            bodyContent = body == null ? null : body.content;
        }
        final LocalizedContent effectiveBody =
                body == null ? null : LocalizedContent.of(bodyContent, body.language, body.count);

        if ((body != null && !bodyIsFallback)
                || pgpEncrypted != null
                || (axolotlEncrypted != null && axolotlEncrypted.hasExtension(Payload.class))
                || oobUrl != null
                || stickerElement != null) {
            final boolean conversationIsProbablyMuc =
                    isTypeGroupChat
                            || mucUserElement != null
                            || connection
                                    .getMucServersWithholdAccount()
                                    .contains(counterpart.getDomain());
            final Conversation conversation =
                    mXmppConnectionService.findOrCreateConversation(
                            account,
                            counterpart.asBareJid(),
                            conversationIsProbablyMuc,
                            false,
                            query,
                            false);
            final boolean conversationMultiMode = conversation.getMode() == Conversation.MODE_MULTI;

            if (serverMsgId == null) {
                serverMsgId =
                        getManager(StanzaIdManager.class)
                                .get(packet, isTypeGroupChat, conversation);
            }

            if (selfAddressed) {
                // don’t store serverMsgId on reflections for edits
                final var reflectedServerMsgId =
                        Strings.isNullOrEmpty(replacementId) ? serverMsgId : null;
                if (mXmppConnectionService.markMessage(
                        conversation,
                        remoteMsgId,
                        Message.STATUS_SEND_RECEIVED,
                        reflectedServerMsgId)) {
                    return;
                }
                status = Message.STATUS_RECEIVED;
                if (remoteMsgId != null
                        && conversation.findMessageWithRemoteId(remoteMsgId, counterpart) != null) {
                    return;
                }
            }

            if (isTypeGroupChat) {
                // this should probably remain a counterpart check
                if (getManager(MultiUserChatManager.class)
                        .getOrCreateState(conversation)
                        .isSelf(counterpart)) {
                    status = Message.STATUS_SEND_RECEIVED;
                    isCarbon = true; // not really carbon but received from another resource
                    // don’t store serverMsgId on reflections for edits
                    final var reflectedServerMsgId =
                            Strings.isNullOrEmpty(replacementId) ? serverMsgId : null;
                    if (mXmppConnectionService.markMessage(
                            conversation,
                            remoteMsgId,
                            status,
                            reflectedServerMsgId,
                            effectiveBody)) {
                        return;
                    } else if (remoteMsgId == null || Config.IGNORE_ID_REWRITE_IN_MUC) {
                        if (bodyContent != null) {
                            Message message = conversation.findSentMessageWithBody(bodyContent);
                            if (message != null) {
                                mXmppConnectionService.markMessage(message, status);
                                return;
                            }
                        }
                    }
                } else {
                    final var user =
                            getManager(MultiUserChatManager.class).getMucUser(packet, query);
                    if (user != null) {
                        final var mucOptions =
                                getManager(MultiUserChatManager.class).getState(from.asBareJid());
                        if (mucOptions != null && mucOptions.isOurAccount(user)) {
                            status = Message.STATUS_SEND_RECEIVED;
                            isCarbon = true;
                        } else {
                            status = Message.STATUS_RECEIVED;
                        }
                    } else {
                        status = Message.STATUS_RECEIVED;
                    }
                }
            }
            final Message message;
            if (pgpEncrypted != null) {
                message = new Message(conversation, pgpEncrypted, Message.ENCRYPTION_PGP, status);
            } else if (axolotlEncrypted != null) {
                final Jid origin;
                if (conversationMultiMode) {
                    final var user =
                            getManager(MultiUserChatManager.class).getMucUser(packet, query);
                    origin = user == null ? null : user.getRealJid();
                    if (origin == null) {
                        Log.d(Config.LOGTAG, "received omemo message in anonymous conference");
                        return;
                    }

                } else {
                    origin = from;
                }

                final boolean liveMessage =
                        query == null && !isTypeGroupChat && mucUserElement == null;
                final boolean checkedForDuplicates =
                        liveMessage
                                || (serverMsgId != null
                                        && remoteMsgId != null
                                        && !conversation.possibleDuplicate(
                                                serverMsgId, remoteMsgId));

                message =
                        parseAxolotlChat(
                                axolotlEncrypted,
                                origin,
                                conversation,
                                status,
                                checkedForDuplicates,
                                query != null);
                if (message == null) {
                    if (query != null) {
                        getManager(ChatStateManager.class).process(packet);
                    }
                    if (query != null && status == Message.STATUS_SEND && remoteMsgId != null) {
                        Message previouslySent = conversation.findSentMessageWithUuid(remoteMsgId);
                        if (previouslySent != null
                                && previouslySent.getServerMsgId() == null
                                && serverMsgId != null) {
                            previouslySent.setServerMsgId(serverMsgId);
                            mXmppConnectionService.databaseBackend.updateMessage(
                                    previouslySent, false);
                            Log.d(
                                    Config.LOGTAG,
                                    account.getJid().asBareJid()
                                            + ": encountered previously sent OMEMO message without"
                                            + " serverId. updating...");
                        }
                    }
                    return;
                }
                if (conversationMultiMode) {
                    message.setTrueCounterpart(origin);
                }
            } else if (body == null && oobUrl != null) {
                message = new Message(conversation, oobUrl, Message.ENCRYPTION_NONE, status);
                message.setOob(true);
                if (CryptoHelper.isPgpEncryptedUrl(oobUrl)) {
                    message.setEncryption(Message.ENCRYPTION_DECRYPTED);
                }
            } else if (body == null && stickerElement != null) {
                final String stickerSource = findFileSharingSource(packet);
                if (stickerSource == null) {
                    Log.d(Config.LOGTAG, "received sticker without a retrievable file source");
                    return;
                }
                message = new Message(conversation, stickerSource, Message.ENCRYPTION_NONE, status);
                message.setOob(true);
            } else {
                message = new Message(conversation, bodyContent, Message.ENCRYPTION_NONE, status);
                if (body.count > 1) {
                    message.setBodyLanguage(body.language);
                }
            }

            message.setCounterpart(counterpart);
            message.setRemoteMsgId(remoteMsgId);
            message.setServerMsgId(serverMsgId);
            message.setCarbon(isCarbon);
            message.setTime(timestamp);
            if (replyId != null) {
                var preview = replyPreview;
                if (preview == null
                        && replyFallbackRange != null
                        && message.getEncryption() == Message.ENCRYPTION_AXOLOTL) {
                    // for omemo the fallback range applies to the decrypted plaintext
                    preview = ReplyUtils.unquote(replyFallbackRange.substringOf(message.getBody()));
                    message.setBody(
                            replyFallbackRange.removeFrom(message.getBody()).stripLeading());
                }
                message.setInReplyTo(new InReplyTo(reply.getTo(), replyId, null, preview));
            }
            if (body != null && bodyContent != null && bodyContent.equals(oobUrl)) {
                message.setOob(true);
                if (CryptoHelper.isPgpEncryptedUrl(oobUrl)) {
                    message.setEncryption(Message.ENCRYPTION_DECRYPTED);
                }
            }
            if (stickerElement != null) {
                message.setSticker(
                        stickerElement.getAttribute("pack"), findStickerDescription(packet));
                if (message.getEncryption() == Message.ENCRYPTION_NONE && !message.isOOb()) {
                    final String stickerSource =
                            oobUrl != null ? oobUrl : findFileSharingSource(packet);
                    if (stickerSource != null) {
                        // make sure stickers sent with an emoji fallback body still download
                        message.setBody(stickerSource);
                        message.setOob(true);
                    }
                }
            }
            message.markable = packet.hasExtension(Markable.class);
            if (spoilerElement != null) {
                message.setSpoilerHint(Strings.nullToEmpty(spoilerElement.getContent()));
            }
            if (hasVoiceMessage) {
                message.setVoiceMessage(true);
            }
            if (liveAttention) {
                message.setAttention(true);
                if (status == Message.STATUS_RECEIVED && !selfAddressed) {
                    notifyAttention(conversation);
                }
            }
            if (conversationMultiMode) {
                final var mucOptions =
                        getManager(MultiUserChatManager.class).getOrCreateState(conversation);
                final var occupantId =
                        mucOptions.occupantId() ? packet.getOnlyExtension(OccupantId.class) : null;
                if (occupantId != null) {
                    message.setOccupantId(occupantId.getId());
                }
                final var user = getManager(MultiUserChatManager.class).getMucUser(packet, query);
                final var trueCounterpart = user == null ? null : user.getRealJid();
                message.setTrueCounterpart(trueCounterpart);
                if (!isTypeGroupChat) {
                    message.setType(Message.TYPE_PRIVATE);
                }
            } else {
                updateLastseen(account, from);
            }

            applyFileSharingMetadata(message, packet);

            if (replacementId != null
                    && message.acceptMessageCorrection()
                    && mXmppConnectionService.allowMessageCorrection()) {
                final String occupantIdFilter;
                if (conversationMultiMode) {
                    // a non-null filter ensures that we actually do filter even when we don't have
                    // one
                    occupantIdFilter = Strings.nullToEmpty(message.getOccupantId());
                } else {
                    occupantIdFilter = null;
                }

                final var replacedMessage =
                        conversation.findMessageWithUuidOrRemoteId(
                                replacementId,
                                occupantIdFilter,
                                message.getStatus() == Message.STATUS_RECEIVED);
                if (replacedMessage != null && replacedMessage.acceptMessageCorrection()) {
                    synchronized (replacedMessage) {
                        if (!replacedMessage.putEdited(message)) {
                            Log.d(
                                    Config.LOGTAG,
                                    account.getJid().asBareJid()
                                            + ": skipping already applied edit");
                            return;
                        }
                        final String uuid = replacedMessage.getUuid();
                        replacedMessage.setUuid(UUID.randomUUID().toString());
                        replacedMessage.setEncryption(message.getEncryption());
                        if (replacedMessage.getStatus() == Message.STATUS_RECEIVED) {
                            replacedMessage.markUnread();
                        }
                        getManager(ChatStateManager.class).process(packet);
                        mXmppConnectionService.updateMessage(replacedMessage, uuid);
                        if (replacedMessage.getStatus() == Message.STATUS_RECEIVED
                                && (replacedMessage.trusted()
                                        || replacedMessage
                                                .isPrivateMessage()) // TODO do we really want
                                // to send receipts for all
                                // PMs?
                                && remoteMsgId != null
                                && !selfAddressed
                                && !isTypeGroupChat) {
                            getManager(DeliveryReceiptManager.class).processRequest(packet, query);
                        }
                        if (replacedMessage.getEncryption() == Message.ENCRYPTION_PGP) {
                            conversation
                                    .getAccount()
                                    .getPgpDecryptionService()
                                    .discard(replacedMessage);
                            conversation
                                    .getAccount()
                                    .getPgpDecryptionService()
                                    .decrypt(replacedMessage, false);
                        }
                    }
                    mXmppConnectionService.getNotificationService().updateNotification();
                    return;
                } else {
                    Log.d(Config.LOGTAG, "replaced message not found");
                }
            }

            final var deletion =
                    new AppSettings(mXmppConnectionService).getAutomaticMessageDeletionInstant();
            if (deletion.isPresent() && message.getTimeSent() < deletion.get().toEpochMilli()) {
                Log.d(
                        Config.LOGTAG,
                        account.getJid().asBareJid()
                                + ": skipping message from "
                                + message.getCounterpart().toString()
                                + " because it was sent prior to our deletion date");
                return;
            }

            boolean checkForDuplicates =
                    (isTypeGroupChat && packet.hasChild("delay", "urn:xmpp:delay"))
                            || message.isPrivateMessage()
                            || message.getServerMsgId() != null
                            || (query == null
                                    && getManager(MessageArchiveManager.class)
                                            .isCatchupInProgress(conversation));
            if (checkForDuplicates) {
                // TODO the duplicate message check seems very legacy.
                final Message duplicate = conversation.findDuplicateMessage(message);
                if (duplicate != null) {
                    final boolean serverMsgIdUpdated;
                    if (duplicate.getStatus() != Message.STATUS_RECEIVED
                            && duplicate.getMessageId().equals(message.getRemoteMsgId())
                            && duplicate.getServerMsgId() == null
                            && message.getServerMsgId() != null) {
                        duplicate.setServerMsgId(message.getServerMsgId());
                        if (mXmppConnectionService.databaseBackend.updateMessage(
                                duplicate, false)) {
                            serverMsgIdUpdated = true;
                        } else {
                            serverMsgIdUpdated = false;
                            Log.e(Config.LOGTAG, "failed to update message");
                        }
                    } else {
                        serverMsgIdUpdated = false;
                    }
                    Log.d(
                            Config.LOGTAG,
                            "skipping duplicate message with "
                                    + message.getCounterpart()
                                    + ". serverMsgIdUpdated="
                                    + serverMsgIdUpdated);
                    return;
                }
            }

            if (query != null
                    && query.getPagingOrder() == MessageArchiveManager.PagingOrder.REVERSE) {
                conversation.prepend(query.getActualInThisQuery(), message);
            } else {
                conversation.add(message);
            }
            if (query != null) {
                query.incrementActualMessageCount();
            }

            if (query == null || query.isCatchup()) { // either no mam or catchup
                if (status == Message.STATUS_SEND || status == Message.STATUS_SEND_RECEIVED) {
                    mXmppConnectionService.markRead(conversation);
                    if (query == null) {
                        getManager(ActivityManager.class)
                                .record(from, ActivityManager.ActivityType.MESSAGE);
                    }
                } else {
                    message.markUnread();
                    notify = true;
                }
            }

            if (message.getEncryption() == Message.ENCRYPTION_PGP) {
                notify =
                        conversation
                                .getAccount()
                                .getPgpDecryptionService()
                                .decrypt(message, notify);
            } else if (message.getEncryption() == Message.ENCRYPTION_AXOLOTL_NOT_FOR_THIS_DEVICE
                    || message.getEncryption() == Message.ENCRYPTION_AXOLOTL_FAILED) {
                notify = false;
            }

            if (query == null) {
                getManager(ChatStateManager.class).process(packet);
            }

            if (message.getStatus() == Message.STATUS_RECEIVED
                    && (message.trusted() || message.isPrivateMessage())
                    && remoteMsgId != null
                    && !selfAddressed
                    && !isTypeGroupChat) {
                getManager(DeliveryReceiptManager.class).processRequest(packet, query);
            }

            mXmppConnectionService.databaseBackend.createMessage(message);
            final HttpConnectionManager manager =
                    this.mXmppConnectionService.getHttpConnectionManager();
            final var autoAcceptFileSize =
                    new AppSettings(mXmppConnectionService).getAutoAcceptFileSize();
            if (message.trusted()
                    && message.treatAsDownloadable()
                    && autoAcceptFileSize.isPresent()) {
                manager.createNewDownloadConnection(message);
            } else if (notify) {
                if (query != null && query.isCatchup()) {
                    mXmppConnectionService.getNotificationService().pushFromBacklog(message);
                } else {
                    mXmppConnectionService.getNotificationService().push(message);
                }
            }
            this.mXmppConnectionService.updateConversationUi();
        } else { // no body

            final var conversation = mXmppConnectionService.find(account, counterpart.asBareJid());
            if (axolotlEncrypted != null) {
                final Jid origin;
                if (conversation != null && conversation.getMode() == Conversation.MODE_MULTI) {
                    final var user =
                            getManager(MultiUserChatManager.class).getMucUser(packet, query);
                    origin = user == null ? null : user.getRealJid();
                    if (origin == null) {
                        Log.d(
                                Config.LOGTAG,
                                "omemo key transport message in anonymous conference received");
                        return;
                    }
                } else if (isTypeGroupChat) {
                    return;
                } else {
                    origin = from;
                }
                try {
                    final XmppAxolotlMessage xmppAxolotlMessage =
                            XmppAxolotlMessage.fromElement(axolotlEncrypted, origin.asBareJid());
                    account.getAxolotlService()
                            .processReceivingKeyTransportMessage(xmppAxolotlMessage, query != null);
                    Log.d(
                            Config.LOGTAG,
                            account.getJid().asBareJid()
                                    + ": omemo key transport message received from "
                                    + origin);
                } catch (Exception e) {
                    Log.d(
                            Config.LOGTAG,
                            account.getJid().asBareJid()
                                    + ": invalid omemo key transport message received "
                                    + e.getMessage());
                    return;
                }
            }

            if (query == null) {
                getManager(ChatStateManager.class).process(packet);
            }

            if (liveAttention && status == Message.STATUS_RECEIVED && !selfAddressed) {
                // a bare XEP-0224 attention request without a body still produces a visible row
                final Conversation attentionConversation =
                        conversation != null
                                ? conversation
                                : mXmppConnectionService.findOrCreateConversation(
                                        account,
                                        counterpart.asBareJid(),
                                        isTypeGroupChat || mucUserElement != null,
                                        false,
                                        query,
                                        false);
                final var attentionMessage =
                        new Message(attentionConversation, "", Message.ENCRYPTION_NONE, status);
                attentionMessage.setCounterpart(counterpart);
                attentionMessage.setRemoteMsgId(remoteMsgId);
                attentionMessage.setServerMsgId(serverMsgId);
                attentionMessage.setCarbon(isCarbon);
                attentionMessage.setTime(timestamp);
                attentionMessage.setAttention(true);
                if (attentionConversation.getMode() == Conversational.MODE_MULTI
                        && !isTypeGroupChat) {
                    attentionMessage.setType(Message.TYPE_PRIVATE);
                }
                attentionMessage.markUnread();
                attentionConversation.add(attentionMessage);
                mXmppConnectionService.databaseBackend.createMessage(attentionMessage);
                mXmppConnectionService.getNotificationService().push(attentionMessage);
                notifyAttention(attentionConversation);
                mXmppConnectionService.updateConversationUi();
            }

            if (isTypeGroupChat) {
                if (packet.hasChild("subject")
                        && !packet.hasChild("thread")) { // We already know it has no body per above
                    if (conversation != null && conversation.getMode() == Conversation.MODE_MULTI) {
                        conversation.setHasMessagesLeftOnServer(conversation.countMessages() > 0);
                        final LocalizedContent subject = packet.getSubject();
                        if (subject != null
                                && getManager(MultiUserChatManager.class)
                                        .getOrCreateState(conversation)
                                        .setSubject(subject.content)) {
                            mXmppConnectionService.updateConversation(conversation);
                        }
                        mXmppConnectionService.updateConversationUi();
                        return;
                    }
                }
            }

            // begin JMI parsing
            if (packet.hasExtension(JingleMessage.class)) {
                getManager(JingleMessageManager.class)
                        .processJingleMessage(
                                packet,
                                counterpart,
                                query,
                                offlineMessagesRetrieved,
                                serverMsgId,
                                timestamp,
                                status);
            }

            if (packet.hasExtension(im.conversations.android.xmpp.model.receipts.Received.class)) {
                getManager(DeliveryReceiptManager.class).processReceived(packet, query);
            }

            if (packet.hasExtension(Displayed.class)) {
                getManager(DisplayedManager.class)
                        .processDisplayed(packet, selfAddressed, counterpart, query);
            }

            if (packet.hasExtension(Reactions.class)) {
                getManager(ReactionManager.class).processReactions(packet, counterpart, query);
            }

            if (packet.hasExtension(Retracted.class) && query != null && conversation != null) {
                // the archive already replaced the original message with a tombstone
                final var tombstone =
                        new Message(conversation, "", Message.ENCRYPTION_NONE, status);
                tombstone.setCounterpart(counterpart);
                tombstone.setRemoteMsgId(remoteMsgId);
                tombstone.setServerMsgId(serverMsgId);
                tombstone.setCarbon(isCarbon);
                tombstone.setTime(timestamp);
                tombstone.markRetracted();
                final var existing = conversation.findDuplicateMessage(tombstone);
                if (existing != null) {
                    getManager(ModerationManager.class).applyRetraction(conversation, existing);
                } else {
                    if (query.getPagingOrder() == MessageArchiveManager.PagingOrder.REVERSE) {
                        conversation.prepend(query.getActualInThisQuery(), tombstone);
                    } else {
                        conversation.add(tombstone);
                    }
                    query.incrementActualMessageCount();
                    mXmppConnectionService.databaseBackend.createMessage(tombstone);
                    mXmppConnectionService.updateConversationUi();
                }
            }

            // end no body
        }

        if (packet.hasExtension(Retract.class)) {
            getManager(ModerationManager.class).handleRetraction(packet, counterpart);
        }

        if (original.hasExtension(Event.class)) {
            getManager(PubSubManager.class).handleEvent(original);
        }

        final var nick = packet.getExtension(Nick.class);
        if (nick != null && Jid.Invalid.isValid(from)) {
            if (getManager(MultiUserChatManager.class).isMuc(from)) {
                return;
            }
            final Contact contact = account.getRoster().getContact(from);
            if (contact.setPresenceName(nick.getContent())) {
                connection.getManager(RosterManager.class).writeToDatabaseAsync();
                mXmppConnectionService.getAvatarService().clear(contact);
            }
        }
    }

    /**
     * Gives a XEP-0224 attention request its distinct buzz. Skipped entirely for muted
     * conversations and when the user turned notification vibration off, so a nudge can never
     * become more annoying than a regular message alert.
     */
    private void notifyAttention(final Conversation conversation) {
        if (conversation.isMuted()) {
            return;
        }
        if (!new AppSettings(mXmppConnectionService).isVibrateOnNotification()) {
            return;
        }
        final var vibrator =
                (Vibrator) mXmppConnectionService.getSystemService(Context.VIBRATOR_SERVICE);
        if (vibrator == null || !vibrator.hasVibrator()) {
            return;
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            vibrator.vibrate(VibrationEffect.createOneShot(500, VibrationEffect.DEFAULT_AMPLITUDE));
        } else {
            vibrator.vibrate(500);
        }
    }

    private static Pair<im.conversations.android.xmpp.model.stanza.Message, Long>
            getForwardedMessagePacket(
                    final im.conversations.android.xmpp.model.stanza.Message original,
                    Class<? extends Extension> clazz) {
        final var extension = original.getExtension(clazz);
        final var forwarded = extension == null ? null : extension.getExtension(Forwarded.class);
        if (forwarded == null) {
            return null;
        }
        final Long timestamp = AbstractParser.parseTimestamp(forwarded, null);
        final var forwardedMessage = forwarded.getMessage();
        if (forwardedMessage == null) {
            return null;
        }
        return new Pair<>(forwardedMessage, timestamp);
    }

    private static Pair<im.conversations.android.xmpp.model.stanza.Message, Long>
            getForwardedMessagePacket(
                    final im.conversations.android.xmpp.model.stanza.Message original,
                    final String name,
                    final String namespace) {
        final Element wrapper = original.findChild(name, namespace);
        final var forwardedElement =
                wrapper == null ? null : wrapper.findChild("forwarded", Namespace.FORWARD);
        if (forwardedElement instanceof Forwarded forwarded) {
            final Long timestamp = AbstractParser.parseTimestamp(forwarded, null);
            final var forwardedMessage = forwarded.getMessage();
            if (forwardedMessage == null) {
                return null;
            }
            return new Pair<>(forwardedMessage, timestamp);
        }
        return null;
    }

    private static Element findStickerElement(
            final im.conversations.android.xmpp.model.stanza.Message packet) {
        final var sticker = packet.findChild("sticker", Namespace.STICKERS);
        if (sticker != null) {
            return sticker;
        }
        // some implementations wrap the sticker into a fasten apply-to element
        final var applyTo = packet.findChild("apply-to", Namespace.FASTEN);
        return applyTo == null ? null : applyTo.findChild("sticker");
    }

    private static String findStickerDescription(
            final im.conversations.android.xmpp.model.stanza.Message packet) {
        final var sharing = packet.findChild("file-sharing", Namespace.SFS);
        final var file =
                sharing == null ? null : sharing.findChild("file", Namespace.FILE_METADATA);
        return file == null ? null : Strings.emptyToNull(file.findChildContent("desc"));
    }

    private static String findFileSharingSource(
            final im.conversations.android.xmpp.model.stanza.Message packet) {
        final var sharing = packet.findChild("file-sharing", Namespace.SFS);
        final var sources = sharing == null ? null : sharing.findChild("sources");
        if (sources == null) {
            return null;
        }
        for (final Element source : sources.getChildren()) {
            final var target = source.getAttribute("target");
            if ("url-data".equals(source.getName())
                    && target != null
                    && (target.startsWith("https://") || target.startsWith("http://"))) {
                return target;
            }
        }
        return null;
    }

    private static final int MAX_SFS_THUMBNAIL_URI_LENGTH = 64 * 1024;

    /**
     * Applies XEP-0447 stateless file sharing metadata to a freshly received file message. Stores a
     * known size and image dimensions in the file params so the bubble can be sized before the
     * download starts and remembers a data uri thumbnail (XEP-0264) for the placeholder. Malformed
     * values are ignored.
     */
    private static void applyFileSharingMetadata(
            final Message message,
            final im.conversations.android.xmpp.model.stanza.Message packet) {
        final int encryption = message.getEncryption();
        final boolean axolotl = encryption == Message.ENCRYPTION_AXOLOTL;
        if (encryption != Message.ENCRYPTION_NONE
                && encryption != Message.ENCRYPTION_DECRYPTED
                && !axolotl) {
            return;
        }
        final var sharing = packet.findChild("file-sharing", Namespace.SFS);
        final var file =
                sharing == null ? null : sharing.findChild("file", Namespace.FILE_METADATA);
        if (file == null) {
            return;
        }
        if (!message.isOOb() && !axolotl) {
            // some senders skip the oob element and only use the body as a fallback that
            // repeats the file-sharing source
            final var source = findFileSharingSource(packet);
            if (source != null && source.equals(message.getBody())) {
                message.setOob(true);
            } else {
                return;
            }
        }
        final var params = message.getFileParams();
        if (params.url == null) {
            return;
        }
        final Long size = parseSfsSize(file.findChildContent("size"));
        final int[] dimensions = parseSfsDimensions(file.findChildContent("dimensions"));
        if (size != null) {
            final var body = new StringBuilder(params.url).append('|').append(size);
            if (dimensions != null) {
                body.append('|').append(dimensions[0]).append('|').append(dimensions[1]);
            }
            message.setBody(body.toString());
        }
        if (dimensions != null) {
            message.setType(
                    message.isPrivateMessage() ? Message.TYPE_PRIVATE_FILE : Message.TYPE_FILE);
        }
        final var thumbnail = findThumbnailDataUri(file);
        if (thumbnail != null) {
            message.setInlineThumbnail(thumbnail);
        }
    }

    private static Long parseSfsSize(final String content) {
        final Long size = Longs.tryParse(Strings.nullToEmpty(content));
        return size != null && size > 0 ? size : null;
    }

    private static int[] parseSfsDimensions(final String content) {
        if (Strings.isNullOrEmpty(content)) {
            return null;
        }
        final var parts = content.trim().split("x");
        if (parts.length != 2) {
            return null;
        }
        try {
            final int width = Integer.parseInt(parts[0].trim());
            final int height = Integer.parseInt(parts[1].trim());
            if (width > 0 && height > 0 && width <= 0x8000 && height <= 0x8000) {
                return new int[] {width, height};
            }
        } catch (final NumberFormatException e) {
            // malformed metadata is ignored
        }
        return null;
    }

    private static String findThumbnailDataUri(final Element file) {
        for (final Element child : file.getChildren()) {
            if (!"thumbnail".equals(child.getName())
                    || !Namespace.THUMBS.equals(child.getNamespace())) {
                continue;
            }
            final var uri = child.getAttribute("uri");
            if (uri != null
                    && uri.length() <= MAX_SFS_THUMBNAIL_URI_LENGTH
                    && uri.startsWith("data:image/")
                    && uri.contains(";base64,")) {
                return uri;
            }
        }
        return null;
    }
}
