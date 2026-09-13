package eu.siacs.conversations.entities;

import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.graphics.Color;
import com.google.common.base.Preconditions;
import com.google.common.base.Splitter;
import com.google.common.base.Strings;
import com.google.common.collect.Collections2;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.ImmutableSet;
import com.google.common.collect.Iterables;
import com.google.common.collect.Lists;
import com.google.common.primitives.Longs;
import de.gultsch.common.MiniUri;
import de.gultsch.common.Patterns;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.crypto.axolotl.AxolotlService;
import eu.siacs.conversations.crypto.axolotl.FingerprintStatus;
import eu.siacs.conversations.http.URL;
import eu.siacs.conversations.persistance.FileBackend;
import eu.siacs.conversations.services.AvatarService;
import eu.siacs.conversations.utils.CryptoHelper;
import eu.siacs.conversations.utils.Emoticons;
import eu.siacs.conversations.utils.MessageUtils;
import eu.siacs.conversations.utils.MimeUtils;
import eu.siacs.conversations.utils.UIHelper;
import eu.siacs.conversations.xmpp.Jid;
import im.conversations.android.json.Services;
import java.io.File;
import java.time.Instant;
import java.util.Collection;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArraySet;
import org.jspecify.annotations.NonNull;

public class Message extends AbstractEntity
        implements AvatarService.Avatar, MucOptions.IdentifiableUser {

    public static final String TABLENAME = "messages";

    public static final int STATUS_RECEIVED = 0;
    public static final int STATUS_UNSEND = 1;
    public static final int STATUS_SEND = 2;
    public static final int STATUS_SEND_FAILED = 3;
    public static final int STATUS_WAITING = 5;
    public static final int STATUS_OFFERED = 6;
    public static final int STATUS_SEND_RECEIVED = 7;
    public static final int STATUS_SEND_DISPLAYED = 8;

    public static final int ENCRYPTION_NONE = 0;
    public static final int ENCRYPTION_PGP = 1;
    public static final int ENCRYPTION_OTR = 2;
    public static final int ENCRYPTION_DECRYPTED = 3;
    public static final int ENCRYPTION_DECRYPTION_FAILED = 4;
    public static final int ENCRYPTION_AXOLOTL = 5;
    public static final int ENCRYPTION_AXOLOTL_NOT_FOR_THIS_DEVICE = 6;
    public static final int ENCRYPTION_AXOLOTL_FAILED = 7;

    public static final int TYPE_TEXT = 0;
    public static final int TYPE_IMAGE = 1;
    public static final int TYPE_FILE = 2;
    public static final int TYPE_STATUS = 3;
    public static final int TYPE_PRIVATE = 4;
    public static final int TYPE_PRIVATE_FILE = 5;
    public static final int TYPE_RTP_SESSION = 6;

    public static final String CONVERSATION = "conversationUuid";
    public static final String COUNTERPART = "counterpart";
    public static final String TRUE_COUNTERPART = "trueCounterpart";
    public static final String BODY = "body";
    public static final String BODY_LANGUAGE = "bodyLanguage";
    public static final String TIME_SENT = "timeSent";
    public static final String ENCRYPTION = "encryption";
    public static final String STATUS = "status";
    public static final String TYPE = "type";
    public static final String CARBON = "carbon";
    public static final String OOB = "oob";
    public static final String EDITED = "edited";
    public static final String REMOTE_MSG_ID = "remoteMsgId";
    public static final String SERVER_MSG_ID = "serverMsgId";
    public static final String RELATIVE_FILE_PATH = "relativeFilePath";
    public static final String SHARED_STORAGE = "sharedStorage";
    public static final String FINGERPRINT = "axolotl_fingerprint";
    public static final String READ = "read";
    public static final String ERROR_MESSAGE = "errorMsg";
    public static final String READ_BY_MARKERS = "readByMarkers";
    public static final String MARKABLE = "markable";
    public static final String DELETED = "deleted";
    public static final String RETRACTED = "retracted";
    public static final String OCCUPANT_ID = "occupantId";
    public static final String REACTIONS = "reactions";
    public static final String IN_REPLY_TO = "inReplyTo";
    public static final String SPOILER_HINT = "spoilerHint";
    public static final String WIRE_FLAGS = "wireFlags";
    public static final int WIRE_FLAG_VOICE_MESSAGE = 1;
    public static final int WIRE_FLAG_ATTENTION = 2;
    public static final String EXPIRE = "expire";
    public static final String ME_COMMAND = "/me ";

    public static final String ERROR_MESSAGE_CANCELLED = "eu.siacs.conversations.cancelled";

    public boolean markable = false;
    protected String conversationUuid;
    protected Jid counterpart;
    protected Jid trueCounterpart;
    protected String body;
    protected String encryptedBody;
    protected long timeSent;
    protected int encryption;
    protected int status;
    protected int type;
    protected boolean deleted = false;
    protected boolean retracted = false;
    protected boolean carbon = false;
    protected boolean oob = false;
    protected List<Edit> edits = Collections.emptyList();
    protected StorageLocation storageLocation;
    protected boolean read = true;
    protected String remoteMsgId = null;
    private String bodyLanguage = null;
    protected String serverMsgId = null;
    private final Conversational conversation;
    protected Transferable transferable = null;
    private Message mNextMessage = null;
    private Message mPreviousMessage = null;
    private String axolotlFingerprint = null;
    private String errorMessage = null;
    private final Set<ReadByMarker> readByMarkers = new CopyOnWriteArraySet<>();
    private String occupantId;
    private String stickerPackId = null;
    private String stickerDescription = null;
    private String inlineThumbnail = null;
    private Collection<Reaction> reactions = Collections.emptyList();
    private InReplyTo inReplyTo = null;
    private String spoilerHint = null;
    private int wireFlags = 0;
    private long expire = 0;

    private Boolean isGeoUri = null;
    private Boolean isEmojisOnly = null;
    private Boolean treatAsDownloadable = null;
    private FileParams fileParams = null;
    private List<MucOptions.User> counterparts;

    protected Message(final Conversational conversation) {
        this.conversation = conversation;
    }

    public Message(final Conversational conversation, final String body, final int encryption) {
        this(conversation, body, encryption, STATUS_UNSEND);
    }

    public Message(
            final Conversational conversation,
            final String body,
            final int encryption,
            final int status) {
        this(
                conversation,
                java.util.UUID.randomUUID().toString(),
                conversation.getUuid(),
                conversation.getAddress() == null ? null : conversation.getAddress().asBareJid(),
                null,
                body,
                System.currentTimeMillis(),
                encryption,
                status,
                TYPE_TEXT,
                false,
                null,
                null,
                null,
                null,
                true,
                Collections.emptyList(),
                false,
                null,
                Collections.emptySet(),
                false,
                false,
                null,
                null,
                Collections.emptyList());
    }

    public Message(
            final Conversation conversation,
            final int status,
            final int type,
            final String remoteMsgId) {
        this(
                conversation,
                java.util.UUID.randomUUID().toString(),
                conversation.getUuid(),
                conversation.getAddress() == null ? null : conversation.getAddress().asBareJid(),
                null,
                null,
                System.currentTimeMillis(),
                Message.ENCRYPTION_NONE,
                status,
                type,
                false,
                remoteMsgId,
                null,
                null,
                null,
                true,
                Collections.emptyList(),
                false,
                null,
                Collections.emptySet(),
                false,
                false,
                null,
                null,
                Collections.emptyList());
    }

    protected Message(
            final Conversational conversation,
            final String uuid,
            final String conversationUUid,
            final Jid counterpart,
            final Jid trueCounterpart,
            final String body,
            final long timeSent,
            final int encryption,
            final int status,
            final int type,
            final boolean carbon,
            final String remoteMsgId,
            final StorageLocation storageLocation,
            final String serverMsgId,
            final String fingerprint,
            final boolean read,
            @NonNull final List<Edit> edited,
            final boolean oob,
            final String errorMessage,
            final Set<ReadByMarker> readByMarkers,
            final boolean markable,
            final boolean deleted,
            final String bodyLanguage,
            final String occupantId,
            @NonNull final Collection<Reaction> reactions) {
        this.conversation = conversation;
        this.uuid = uuid;
        this.conversationUuid = conversationUUid;
        this.counterpart = counterpart;
        this.trueCounterpart = trueCounterpart;
        this.body = body == null ? "" : body;
        this.timeSent = timeSent;
        this.encryption = encryption;
        this.status = status;
        this.type = type;
        this.carbon = carbon;
        this.remoteMsgId = remoteMsgId;
        this.storageLocation = storageLocation;
        this.serverMsgId = serverMsgId;
        this.axolotlFingerprint = fingerprint;
        this.read = read;
        this.edits = edited;
        this.oob = oob;
        this.errorMessage = errorMessage;
        this.readByMarkers.addAll(readByMarkers);
        this.markable = markable;
        this.deleted = deleted;
        this.bodyLanguage = bodyLanguage;
        this.occupantId = occupantId;
        this.reactions = reactions;
    }

    public static Message fromCursor(
            final Context context, final Cursor cursor, final Conversation conversation) {
        final var message =
                new Message(
                        conversation,
                        cursor.getString(cursor.getColumnIndexOrThrow(UUID)),
                        cursor.getString(cursor.getColumnIndexOrThrow(CONVERSATION)),
                        fromString(cursor.getString(cursor.getColumnIndexOrThrow(COUNTERPART))),
                        fromString(
                                cursor.getString(cursor.getColumnIndexOrThrow(TRUE_COUNTERPART))),
                        cursor.getString(cursor.getColumnIndexOrThrow(BODY)),
                        cursor.getLong(cursor.getColumnIndexOrThrow(TIME_SENT)),
                        cursor.getInt(cursor.getColumnIndexOrThrow(ENCRYPTION)),
                        cursor.getInt(cursor.getColumnIndexOrThrow(STATUS)),
                        cursor.getInt(cursor.getColumnIndexOrThrow(TYPE)),
                        cursor.getInt(cursor.getColumnIndexOrThrow(CARBON)) > 0,
                        cursor.getString(cursor.getColumnIndexOrThrow(REMOTE_MSG_ID)),
                        storageLocationFromCursor(context, cursor),
                        cursor.getString(cursor.getColumnIndexOrThrow(SERVER_MSG_ID)),
                        cursor.getString(cursor.getColumnIndexOrThrow(FINGERPRINT)),
                        cursor.getInt(cursor.getColumnIndexOrThrow(READ)) > 0,
                        Edit.ofString(cursor.getString(cursor.getColumnIndexOrThrow(EDITED))),
                        cursor.getInt(cursor.getColumnIndexOrThrow(OOB)) > 0,
                        cursor.getString(cursor.getColumnIndexOrThrow(ERROR_MESSAGE)),
                        ReadByMarker.fromJsonString(
                                cursor.getString(cursor.getColumnIndexOrThrow(READ_BY_MARKERS))),
                        cursor.getInt(cursor.getColumnIndexOrThrow(MARKABLE)) > 0,
                        cursor.getInt(cursor.getColumnIndexOrThrow(DELETED)) > 0,
                        cursor.getString(cursor.getColumnIndexOrThrow(BODY_LANGUAGE)),
                        cursor.getString(cursor.getColumnIndexOrThrow(OCCUPANT_ID)),
                        Reaction.fromString(
                                cursor.getString(cursor.getColumnIndexOrThrow(REACTIONS))));
        message.retracted = cursor.getInt(cursor.getColumnIndexOrThrow(RETRACTED)) > 0;
        message.inReplyTo =
                InReplyTo.ofString(cursor.getString(cursor.getColumnIndexOrThrow(IN_REPLY_TO)));
        message.spoilerHint = cursor.getString(cursor.getColumnIndexOrThrow(SPOILER_HINT));
        message.wireFlags = cursor.getInt(cursor.getColumnIndexOrThrow(WIRE_FLAGS));
        message.expire = cursor.getLong(cursor.getColumnIndexOrThrow(EXPIRE));
        return message;
    }

    protected static StorageLocation storageLocationFromCursor(
            final Context context, final Cursor cursor) {
        final var filePath = cursor.getString(cursor.getColumnIndexOrThrow(RELATIVE_FILE_PATH));
        final var sharedStorage = cursor.getInt(cursor.getColumnIndexOrThrow(SHARED_STORAGE)) > 0;
        if (Strings.isNullOrEmpty(filePath)) {
            return null;
        } else if (filePath.charAt(0) == '/') {
            return new StorageLocation(new File(filePath), sharedStorage);
        } else {
            final var file = FileBackend.getLegacyFileForFilename(context, filePath);
            return new StorageLocation(file, sharedStorage);
        }
    }

    private static Jid fromString(String value) {
        try {
            if (value != null) {
                return Jid.of(value);
            }
        } catch (IllegalArgumentException e) {
            return null;
        }
        return null;
    }

    public static Message createStatusMessage(final Conversation conversation, final String body) {
        final Message message = new Message(conversation);
        message.setType(Message.TYPE_STATUS);
        message.setStatus(Message.STATUS_RECEIVED);
        message.body = body;
        return message;
    }

    public static Message createLoadMoreMessage(Conversation conversation) {
        final Message message = new Message(conversation);
        message.setType(Message.TYPE_STATUS);
        message.body = "LOAD_MORE";
        return message;
    }

    @Override
    public ContentValues getContentValues() {
        final var values = new ContentValues();
        values.put(UUID, uuid);
        values.put(CONVERSATION, conversationUuid);
        if (counterpart == null) {
            values.putNull(COUNTERPART);
        } else {
            values.put(COUNTERPART, counterpart.toString());
        }
        if (trueCounterpart == null) {
            values.putNull(TRUE_COUNTERPART);
        } else {
            values.put(TRUE_COUNTERPART, trueCounterpart.toString());
        }
        values.put(
                BODY,
                body.length() > Config.MAX_STORAGE_MESSAGE_CHARS
                        ? body.substring(0, Config.MAX_STORAGE_MESSAGE_CHARS)
                        : body);
        values.put(TIME_SENT, timeSent);
        values.put(ENCRYPTION, encryption);
        values.put(STATUS, status);
        values.put(TYPE, type);
        values.put(CARBON, carbon ? 1 : 0);
        values.put(REMOTE_MSG_ID, remoteMsgId);
        if (storageLocation != null) {
            values.put(RELATIVE_FILE_PATH, storageLocation.file().getAbsolutePath());
            values.put(SHARED_STORAGE, storageLocation.sharedStorage());
        } else {
            values.putNull(RELATIVE_FILE_PATH);
        }
        values.put(SERVER_MSG_ID, serverMsgId);
        values.put(FINGERPRINT, axolotlFingerprint);
        values.put(READ, read ? 1 : 0);
        values.put(EDITED, Services.GSON.toJson(edits));
        values.put(OOB, oob ? 1 : 0);
        values.put(ERROR_MESSAGE, errorMessage);
        values.put(READ_BY_MARKERS, Services.GSON.toJson(readByMarkers));
        values.put(MARKABLE, markable ? 1 : 0);
        values.put(DELETED, deleted ? 1 : 0);
        values.put(RETRACTED, retracted ? 1 : 0);
        values.put(BODY_LANGUAGE, bodyLanguage);
        values.put(OCCUPANT_ID, occupantId);
        values.put(REACTIONS, Reaction.toString(this.reactions));
        values.put(IN_REPLY_TO, inReplyTo == null ? null : inReplyTo.toJson());
        values.put(SPOILER_HINT, spoilerHint);
        values.put(WIRE_FLAGS, wireFlags);
        values.put(EXPIRE, expire);
        return values;
    }

    public String getConversationUuid() {
        return conversationUuid;
    }

    public Conversational getConversation() {
        return this.conversation;
    }

    public Jid getCounterpart() {
        return counterpart;
    }

    public void setCounterpart(final Jid counterpart) {
        this.counterpart = counterpart;
    }

    public Contact getContact() {
        if (this.conversation.getMode() == Conversation.MODE_SINGLE) {
            return this.conversation.getContact();
        } else if (this.conversation instanceof Conversation c
                && c.getMode() == Conversational.MODE_MULTI) {
            return c.getMucOptions().getUserOrStub(this).getContact();
        } else if (this.counterpart != null) {
            return this.conversation
                    .getAccount()
                    .getRoster()
                    .getContactFromContactList(this.trueCounterpart);
        } else {
            return null;
        }
    }

    public String getBody() {
        return body;
    }

    public synchronized void setBody(final String body) {
        if (body == null) {
            throw new Error("You should not set the message body to null");
        }
        this.body = body;
        this.isGeoUri = null;
        this.isEmojisOnly = null;
        this.treatAsDownloadable = null;
        this.fileParams = null;
    }

    public String getErrorMessage() {
        return errorMessage;
    }

    public boolean setErrorMessage(String message) {
        boolean changed =
                (message != null && !message.equals(errorMessage))
                        || (message == null && errorMessage != null);
        this.errorMessage = message;
        return changed;
    }

    public long getTimeSent() {
        return timeSent;
    }

    public Instant getSentAt() {
        return Instant.ofEpochMilli(this.timeSent);
    }

    public int getEncryption() {
        return encryption;
    }

    public void setEncryption(int encryption) {
        this.encryption = encryption;
    }

    public int getStatus() {
        return status;
    }

    public void setStatus(final int status) {
        final var current = this.status;
        if (current == Message.STATUS_RECEIVED) {
            if (status != Message.STATUS_RECEIVED) {
                throw new AssertionError(
                        "A received message can not be converted to status=" + status);
            }
        } else {
            if (status == Message.STATUS_RECEIVED) {
                throw new AssertionError("A sent message can not be converted to received");
            }
        }
        this.status = status;
    }

    public StorageLocation getRelativeFilePath() {
        return this.storageLocation;
    }

    public void setRelativeFilePath(final StorageLocation storageLocation) {
        this.storageLocation = storageLocation;
    }

    public String getMessageId() {
        if (status == Message.STATUS_RECEIVED || this.remoteMsgId != null) {
            return this.remoteMsgId;
        } else {
            return this.uuid;
        }
    }

    public String getRemoteMsgId() {
        return this.remoteMsgId;
    }

    public void setRemoteMsgId(String id) {
        this.remoteMsgId = id;
    }

    public String getServerMsgId() {
        return this.serverMsgId;
    }

    public void setServerMsgId(String id) {
        this.serverMsgId = id;
    }

    public boolean isRead() {
        return this.read;
    }

    public boolean isDeleted() {
        return this.deleted;
    }

    public void setDeleted(boolean deleted) {
        this.deleted = deleted;
    }

    public boolean isRetracted() {
        return this.retracted;
    }

    /**
     * Marks this message as retracted (XEP-0424). The body, edit history and file reference are
     * removed so that only a tombstone remains.
     */
    public void markRetracted() {
        this.retracted = true;
        this.setBody("");
        this.encryptedBody = null;
        this.axolotlFingerprint = null;
        this.edits = Collections.emptyList();
        this.storageLocation = null;
        this.oob = false;
        this.errorMessage = null;
        this.reactions = Collections.emptyList();
        this.markRead();
    }

    public void markRead() {
        this.read = true;
        if (this.expire < 0) {
            // an armed XEP-0466 timer stores its duration as the negative number of
            // seconds; reading the message starts the countdown
            this.expire = System.currentTimeMillis() + (-this.expire) * 1000L;
        }
    }

    public void markUnread() {
        this.read = false;
    }

    public void setTime(long time) {
        this.timeSent = time;
    }

    public String getEncryptedBody() {
        return this.encryptedBody;
    }

    public void setEncryptedBody(String body) {
        this.encryptedBody = body;
    }

    public int getType() {
        return this.type;
    }

    public void setType(int type) {
        this.type = type;
    }

    /**
     * Marks this message as a XEP-0449 sticker. The pack id refers to the pubsub item id of the
     * sticker pack when known. This flag is transient and intentionally not persisted; after a
     * restart a sticker simply renders as a regular image message.
     */
    public void setSticker(final String packId) {
        setSticker(packId, null);
    }

    public void setSticker(final String packId, final String description) {
        this.stickerPackId = Strings.nullToEmpty(packId);
        this.stickerDescription = Strings.emptyToNull(description);
    }

    public boolean isSticker() {
        return stickerPackId != null;
    }

    public String getStickerPackId() {
        return stickerPackId;
    }

    public String getStickerDescription() {
        return stickerDescription;
    }

    /**
     * Remembers a XEP-0264 data uri thumbnail received via stateless file sharing so the media
     * bubble can show a blurry placeholder before the actual file is downloaded. Transient like the
     * sticker flags and intentionally not persisted.
     */
    public void setInlineThumbnail(final String dataUri) {
        this.inlineThumbnail = dataUri;
    }

    public String getInlineThumbnail() {
        return this.inlineThumbnail;
    }

    /**
     * Marks this message as a XEP-0382 spoiler. A non-null hint (including the empty string)
     * indicates a spoiler; the empty string means no hint was provided. Persisted in the
     * spoilerHint column so a spoiler stays hidden across restarts.
     */
    public void setSpoilerHint(final String hint) {
        this.spoilerHint = hint;
    }

    public boolean isSpoiler() {
        return spoilerHint != null;
    }

    public String getSpoilerHint() {
        return Strings.emptyToNull(spoilerHint);
    }

    /**
     * XEP-0224 attention request (nudge) received or sent with this message. Persisted via the
     * wireFlags column.
     */
    public void setAttention(final boolean attention) {
        setWireFlag(WIRE_FLAG_ATTENTION, attention);
    }

    public boolean isAttention() {
        return (wireFlags & WIRE_FLAG_ATTENTION) != 0;
    }

    /**
     * Proto-XEP voice message marker (urn:xmpp:voice-message). Persisted via the wireFlags column
     * so flagged audio keeps rendering as a voice note after a restart.
     */
    public void setVoiceMessage(final boolean voiceMessage) {
        setWireFlag(WIRE_FLAG_VOICE_MESSAGE, voiceMessage);
    }

    public boolean isVoiceMessage() {
        return (wireFlags & WIRE_FLAG_VOICE_MESSAGE) != 0;
    }

    public void setWireFlags(final int wireFlags) {
        this.wireFlags = wireFlags;
    }

    /**
     * XEP-0466 ephemeral message expiry. A positive value is the absolute time in epoch millis at
     * which the message must be discarded; 0 means the message never expires; a negative value is
     * an armed timer whose countdown (of -expire seconds) starts once the message is marked read.
     */
    public long getExpire() {
        return expire;
    }

    public void setExpire(final long expire) {
        this.expire = expire;
    }

    public void setExpireAfterRead(final long seconds) {
        this.expire = -seconds;
    }

    public boolean isEphemeral() {
        return expire != 0;
    }

    private void setWireFlag(final int flag, final boolean enabled) {
        if (enabled) {
            this.wireFlags |= flag;
        } else {
            this.wireFlags &= ~flag;
        }
    }

    // TODO carbons is mostly unused these days (only the inValidSession() is still using this)
    public boolean isCarbon() {
        return carbon;
    }

    public void setCarbon(boolean carbon) {
        this.carbon = carbon;
    }

    public boolean putEdited(final Message edit) {
        final var body = BodyVersion.of(edit);
        return putEdited(edit.getRemoteMsgId(), edit.getServerMsgId(), edit.getSentAt(), body);
    }

    public void putEdited(final BodyVersion body) {
        final var uuid = java.util.UUID.randomUUID();
        if (!putEdited(uuid.toString(), null, Instant.now(), body)) {
            throw new IllegalStateException("Could not store edit");
        }
        this.remoteMsgId = Objects.requireNonNull(Iterables.getFirst(this.edits, null)).id();
        this.uuid = uuid.toString();
    }

    private boolean putEdited(
            final String id,
            final String serverMsgId,
            final Instant sentAt,
            final BodyVersion body) {
        final List<Edit> versions = getVersions();
        if (Iterables.any(
                versions,
                v ->
                        v != null
                                && ((id != null && id.equals(v.id()))
                                        || (serverMsgId != null
                                                && serverMsgId.equals(v.serverMsgId()))))) {
            return false;
        }
        final var edit = new Edit(id, serverMsgId, sentAt, null, null, null);
        this.edits = new ImmutableList.Builder<Edit>().addAll(versions).add(edit).build();
        this.setBody(body.body);
        this.encryption = body.encryption;
        this.axolotlFingerprint = body.fingerprint;
        return true;
    }

    public boolean hasEditHistory() {
        return getVersions().size() >= 2;
    }

    private List<Edit> getVersions() {
        final var withoutLegacy =
                Collections2.filter(
                        this.edits, e -> e != null && e.id() != null && e.sentAt() != null);
        if (withoutLegacy.isEmpty()) {
            final var id = getMessageId();
            return Collections.singletonList(
                    new Edit(id, serverMsgId, getSentAt(), body, encryption, axolotlFingerprint));
        } else {
            final var body = BodyVersion.of(this);
            return ImmutableList.copyOf(
                    Collections2.transform(
                            withoutLegacy,
                            e -> {
                                Preconditions.checkNotNull(e);
                                if (e.body() == null || e.encryption() == null) {
                                    return e.withBody(body);
                                } else {
                                    return e;
                                }
                            }));
        }
    }

    public List<Message> getVersionsAsMessages() {
        final var versions = getVersions();
        return Lists.transform(
                versions,
                e -> {
                    if (e == null) {
                        return null;
                    }
                    return new MessageVersion(
                            conversation,
                            uuid,
                            conversationUuid,
                            counterpart,
                            trueCounterpart,
                            e.asBodyVersion(),
                            e.sentAt().toEpochMilli(),
                            status,
                            type,
                            carbon,
                            e.id(),
                            e.serverMsgId(),
                            oob,
                            occupantId);
                });
    }

    public String getBodyLanguage() {
        return this.bodyLanguage;
    }

    public void setBodyLanguage(String language) {
        this.bodyLanguage = language;
    }

    public boolean edited() {
        return !this.edits.isEmpty();
    }

    public void setTrueCounterpart(Jid trueCounterpart) {
        this.trueCounterpart = trueCounterpart;
    }

    public Jid getTrueCounterpart() {
        return this.trueCounterpart;
    }

    public Transferable getTransferable() {
        return this.transferable;
    }

    public synchronized void setTransferable(Transferable transferable) {
        this.fileParams = null;
        this.transferable = transferable;
    }

    public boolean addReadByMarker(final ReadByMarker readByMarker) {
        if (readByMarker.realJid() != null) {
            if (readByMarker.realJid().asBareJid().equals(trueCounterpart)) {
                return false;
            }
        } else if (readByMarker.fullJid() != null) {
            if (readByMarker.fullJid().equals(counterpart)) {
                return false;
            }
        }
        if (this.readByMarkers.add(readByMarker)) {
            if (readByMarker.realJid() != null && readByMarker.fullJid() != null) {
                Iterator<ReadByMarker> iterator = this.readByMarkers.iterator();
                while (iterator.hasNext()) {
                    ReadByMarker marker = iterator.next();
                    if (marker.realJid() == null
                            && readByMarker.fullJid().equals(marker.fullJid())) {
                        iterator.remove();
                    }
                }
            }
            return true;
        } else {
            return false;
        }
    }

    public Set<ReadByMarker> getReadByMarkers() {
        return ImmutableSet.copyOf(this.readByMarkers);
    }

    public Set<Jid> getReadyByTrue() {
        return ImmutableSet.copyOf(
                Collections2.transform(
                        Collections2.filter(
                                this.readByMarkers,
                                m -> Objects.requireNonNull(m).realJid() != null),
                        r -> Objects.requireNonNull(r).realJid()));
    }

    boolean similar(Message message) {
        if (!isPrivateMessage() && this.serverMsgId != null && message.getServerMsgId() != null) {
            return this.serverMsgId.equals(message.getServerMsgId())
                    || Edit.wasPreviouslyEditedServerMsgId(edits, message.getServerMsgId());
        } else if (Edit.wasPreviouslyEditedServerMsgId(edits, message.getServerMsgId())) {
            return true;
        } else if (this.body == null || this.counterpart == null) {
            return false;
        } else {
            String body, otherBody;
            if (this.hasFileOnRemoteHost()) {
                body = getFileParams().url;
                otherBody = message.body == null ? null : message.body.trim();
            } else {
                body = this.body;
                otherBody = message.body;
            }
            final boolean matchingCounterpart = this.counterpart.equals(message.getCounterpart());
            if (message.getRemoteMsgId() != null) {
                final boolean hasUuid =
                        CryptoHelper.UUID_PATTERN.matcher(message.getRemoteMsgId()).matches();
                if (hasUuid
                        && matchingCounterpart
                        && Edit.wasPreviouslyEditedRemoteMsgId(edits, message.getRemoteMsgId())) {
                    return true;
                }
                return (message.getRemoteMsgId().equals(this.remoteMsgId)
                                || message.getRemoteMsgId().equals(this.uuid))
                        && matchingCounterpart
                        && (body.equals(otherBody)
                                || (message.getEncryption() == Message.ENCRYPTION_PGP && hasUuid));
            } else {
                return this.remoteMsgId == null
                        && matchingCounterpart
                        && body.equals(otherBody)
                        && Math.abs(this.getTimeSent() - message.getTimeSent()) < 20_000;
            }
        }
    }

    public Message next() {
        if (this.conversation instanceof Conversation c) {
            synchronized (c.messages) {
                if (this.mNextMessage == null) {
                    int index = c.messages.indexOf(this);
                    if (index < 0 || index >= c.messages.size() - 1) {
                        this.mNextMessage = null;
                    } else {
                        this.mNextMessage = c.messages.get(index + 1);
                    }
                }
                return this.mNextMessage;
            }
        } else {
            throw new AssertionError("Calling next should be disabled for stubs");
        }
    }

    public Message prev() {
        if (this.conversation instanceof Conversation c) {
            synchronized (c.messages) {
                if (this.mPreviousMessage == null) {
                    int index = c.messages.indexOf(this);
                    if (index <= 0 || index > c.messages.size()) {
                        this.mPreviousMessage = null;
                    } else {
                        this.mPreviousMessage = c.messages.get(index - 1);
                    }
                }
            }
            return this.mPreviousMessage;
        } else {
            throw new AssertionError("Calling prev should be disabled for stubs");
        }
    }

    public boolean isEditable() {
        return status != STATUS_RECEIVED
                && !isCarbon()
                && type != Message.TYPE_RTP_SESSION
                && type != Message.TYPE_STATUS;
    }

    public boolean acceptMessageCorrection() {
        return type == Message.TYPE_TEXT
                && !this.retracted
                && !this.isGeoUri()
                && !this.treatAsDownloadable();
    }

    public void setCounterparts(List<MucOptions.User> counterparts) {
        this.counterparts = counterparts;
    }

    public List<MucOptions.User> getCounterparts() {
        return this.counterparts;
    }

    @Override
    public int getAvatarBackgroundColor() {
        if (type == Message.TYPE_STATUS
                && getCounterparts() != null
                && getCounterparts().size() > 1) {
            return Color.TRANSPARENT;
        } else {
            return UIHelper.getColorForName(UIHelper.getMessageDisplayName(this));
        }
    }

    @Override
    public String getDisplayName() {
        if (type == Message.TYPE_STATUS
                && getCounterparts() != null
                && getCounterparts().size() > 1) {
            return "";
        } else {
            return UIHelper.getMessageDisplayName(this);
        }
    }

    public boolean isOOb() {
        return oob;
    }

    public void setOccupantId(final String id) {
        this.occupantId = id;
    }

    public String getOccupantId() {
        return this.occupantId;
    }

    public void setInReplyTo(final InReplyTo inReplyTo) {
        this.inReplyTo = inReplyTo;
    }

    public InReplyTo getInReplyTo() {
        return this.inReplyTo;
    }

    public Collection<Reaction> getReactions() {
        return this.reactions;
    }

    public Reaction.Aggregated getAggregatedReactions() {
        return Reaction.aggregated(this.reactions);
    }

    public void setReactions(final Collection<Reaction> reactions) {
        this.reactions = reactions;
    }

    public boolean hasMeCommand() {
        return this.body.trim().startsWith(ME_COMMAND);
    }

    public boolean trusted() {
        final var contact = this.getContact();
        return status > STATUS_RECEIVED
                || (contact != null && (contact.showInContactList() || contact.isSelf()));
    }

    public boolean fixCounterpart() {
        final var fullAddresses = conversation.getContact().getFullAddresses();
        if (counterpart != null && fullAddresses.contains(counterpart)) {
            return true;
        } else if (fullAddresses.isEmpty()) {
            counterpart = null;
            return false;
        } else {
            counterpart = Preconditions.checkNotNull(Iterables.getFirst(fullAddresses, null));
            return true;
        }
    }

    public void setUuid(String uuid) {
        this.uuid = uuid;
    }

    public Collection<String> getEditedServerMessageIds() {
        return Collections2.transform(this.edits, Edit::serverMsgId);
    }

    public void setOob(boolean isOob) {
        this.oob = isOob;
    }

    public String getMimeType() {
        String extension;
        if (storageLocation != null) {
            extension = MimeUtils.extractRelevantExtension(storageLocation.file().getName());
        } else {
            // a downloadable body can carry file params as pipe separated suffix
            final String firstLine = body.split("\n")[0];
            final int pipe = firstLine.indexOf('|');
            final String url = URL.tryParse(pipe < 0 ? firstLine : firstLine.substring(0, pipe));
            if (url == null) {
                return null;
            }
            extension = MimeUtils.extractRelevantExtension(url);
        }
        return MimeUtils.guessMimeTypeFromExtension(extension);
    }

    public synchronized boolean treatAsDownloadable() {
        if (treatAsDownloadable == null) {
            treatAsDownloadable = MessageUtils.treatAsDownloadable(this.body, this.oob);
        }
        return treatAsDownloadable;
    }

    public synchronized boolean bodyIsOnlyEmojis() {
        if (isEmojisOnly == null) {
            isEmojisOnly = Emoticons.isOnlyEmoji(body.replaceAll("\\s", ""));
        }
        return isEmojisOnly;
    }

    public synchronized boolean isGeoUri() {
        if (isGeoUri == null) {
            isGeoUri =
                    Patterns.URI_GEO.matcher(body).matches()
                            && MiniUri.getOrNull(body) instanceof MiniUri.Geo;
        }
        return isGeoUri;
    }

    public synchronized void resetFileParams() {
        this.fileParams = null;
    }

    public synchronized FileParams getFileParams() {
        if (fileParams == null) {
            fileParams = new FileParams();
            if (this.transferable != null) {
                fileParams.size = this.transferable.getFileSize();
            }
            final String[] parts = body == null ? new String[0] : body.split("\\|");
            switch (parts.length) {
                case 1:
                    try {
                        fileParams.size = Long.parseLong(parts[0]);
                    } catch (final NumberFormatException e) {
                        fileParams.url = URL.tryParse(parts[0]);
                    }
                    break;
                case 5:
                    fileParams.runtime = parseInt(parts[4]);
                case 4:
                    fileParams.width = parseInt(parts[2]);
                    fileParams.height = parseInt(parts[3]);
                case 2:
                    fileParams.url = URL.tryParse(parts[0]);
                    fileParams.size = Longs.tryParse(parts[1]);
                    break;
                case 3:
                    fileParams.size = Longs.tryParse(parts[0]);
                    fileParams.width = parseInt(parts[1]);
                    fileParams.height = parseInt(parts[2]);
                    break;
            }
        }
        return fileParams;
    }

    private static int parseInt(String value) {
        try {
            return Integer.parseInt(value);
        } catch (NumberFormatException e) {
            return 0;
        }
    }

    public void untie() {
        this.mNextMessage = null;
        this.mPreviousMessage = null;
    }

    public boolean isPrivateMessage() {
        return type == TYPE_PRIVATE || type == TYPE_PRIVATE_FILE;
    }

    public boolean isFileOrImage() {
        return type == TYPE_FILE || type == TYPE_IMAGE || type == TYPE_PRIVATE_FILE;
    }

    public boolean isTypeText() {
        return type == TYPE_TEXT || type == TYPE_PRIVATE;
    }

    public boolean hasFileOnRemoteHost() {
        return isFileOrImage() && getFileParams().url != null;
    }

    public boolean needsUploading() {
        return isFileOrImage() && getFileParams().url == null;
    }

    @Override
    public Jid mucUserAddress() {
        return this.counterpart;
    }

    @Override
    public Jid mucUserRealAddress() {
        final var address = this.trueCounterpart;
        return address == null ? null : address.asBareJid();
    }

    @Override
    public String mucUserOccupantId() {
        return this.occupantId;
    }

    public static class FileParams {
        public String url;
        public Long size = null;
        public int width = 0;
        public int height = 0;
        public int runtime = 0;

        public long getSize() {
            return size == null ? 0 : size;
        }

        public static FileParams of(final String body) {
            final var fileParams = new FileParams();
            final String[] parts =
                    Strings.isNullOrEmpty(body)
                            ? new String[0]
                            : Splitter.on('|').splitToList(body).toArray(new String[0]);
            switch (parts.length) {
                case 1:
                    try {
                        fileParams.size = Long.parseLong(parts[0]);
                    } catch (final NumberFormatException e) {
                        fileParams.url = URL.tryParse(parts[0]);
                    }
                    break;
                case 5:
                    fileParams.runtime = parseInt(parts[4]);
                case 4:
                    fileParams.width = parseInt(parts[2]);
                    fileParams.height = parseInt(parts[3]);
                case 2:
                    fileParams.url = URL.tryParse(parts[0]);
                    fileParams.size = Longs.tryParse(parts[1]);
                    break;
                case 3:
                    fileParams.size = Longs.tryParse(parts[0]);
                    fileParams.width = parseInt(parts[1]);
                    fileParams.height = parseInt(parts[2]);
                    break;
            }
            return fileParams;
        }
    }

    public void setFingerprint(String fingerprint) {
        this.axolotlFingerprint = fingerprint;
    }

    public String getFingerprint() {
        return axolotlFingerprint;
    }

    public boolean isTrusted() {
        final AxolotlService axolotlService = conversation.getAccount().getAxolotlService();
        final FingerprintStatus s =
                axolotlService != null
                        ? axolotlService.getFingerprintTrust(axolotlFingerprint)
                        : null;
        return s != null && s.isTrusted();
    }

    private int getPreviousEncryption() {
        for (Message iterator = this.prev(); iterator != null; iterator = iterator.prev()) {
            if (iterator.isCarbon() || iterator.getStatus() == STATUS_RECEIVED) {
                continue;
            }
            return iterator.getEncryption();
        }
        return ENCRYPTION_NONE;
    }

    private int getNextEncryption() {
        if (this.conversation instanceof Conversation c) {
            for (Message iterator = this.next(); iterator != null; iterator = iterator.next()) {
                if (iterator.isCarbon() || iterator.getStatus() == STATUS_RECEIVED) {
                    continue;
                }
                return iterator.getEncryption();
            }
            return c.getNextEncryption();
        } else {
            throw new AssertionError(
                    "This should never be called since isInValidSession should be disabled for"
                            + " stubs");
        }
    }

    public boolean isValidInSession() {
        int pastEncryption = getCleanedEncryption(this.getPreviousEncryption());
        int futureEncryption = getCleanedEncryption(this.getNextEncryption());

        boolean inUnencryptedSession =
                pastEncryption == ENCRYPTION_NONE
                        || futureEncryption == ENCRYPTION_NONE
                        || pastEncryption != futureEncryption;

        return inUnencryptedSession || getCleanedEncryption(this.getEncryption()) == pastEncryption;
    }

    private static int getCleanedEncryption(int encryption) {
        if (encryption == ENCRYPTION_DECRYPTED || encryption == ENCRYPTION_DECRYPTION_FAILED) {
            return ENCRYPTION_PGP;
        }
        if (encryption == ENCRYPTION_AXOLOTL_NOT_FOR_THIS_DEVICE
                || encryption == ENCRYPTION_AXOLOTL_FAILED) {
            return ENCRYPTION_AXOLOTL;
        }
        return encryption;
    }

    public static void configurePrivateMessage(final Message message) {
        configurePrivateMessage(message, false);
    }

    public static boolean configurePrivateFileMessage(final Message message) {
        return configurePrivateMessage(message, true);
    }

    private static boolean configurePrivateMessage(final Message message, final boolean isFile) {
        if (message.conversation instanceof Conversation conversation) {
            if (conversation.getMode() == Conversation.MODE_MULTI) {
                final Jid nextCounterpart = conversation.getNextCounterpart();
                return configurePrivateMessage(conversation, message, nextCounterpart, isFile);
            }
        }
        return false;
    }

    public static void configurePrivateMessage(final Message message, final Jid counterpart) {
        if (message.conversation instanceof Conversation conversation) {
            configurePrivateMessage(conversation, message, counterpart, false);
        }
    }

    private static boolean configurePrivateMessage(
            final Conversation conversation,
            final Message message,
            final Jid counterpart,
            final boolean isFile) {
        if (counterpart == null) {
            return false;
        }
        message.setCounterpart(counterpart);
        final var mucOptions = conversation.getMucOptions();
        if (counterpart.equals(mucOptions.getSelf().getFullJid())) {
            message.setTrueCounterpart(conversation.getAccount().getJid().asBareJid());
        } else {
            final var user = mucOptions.getUser(counterpart);
            if (user != null) {
                message.setTrueCounterpart(user.getRealJid());
                message.setOccupantId(user.getOccupantId());
            }
        }
        message.setType(isFile ? Message.TYPE_PRIVATE_FILE : Message.TYPE_PRIVATE);
        return true;
    }

    public record StorageLocation(File file, boolean sharedStorage) {}

    public record BodyVersion(String body, int encryption, String fingerprint) {
        public static BodyVersion of(final Message m) {
            return new BodyVersion(m.getBody(), m.getEncryption(), m.getFingerprint());
        }
    }

    public static class MessageVersion extends Message {

        protected MessageVersion(
                Conversational conversation,
                String uuid,
                String conversationUUid,
                Jid counterpart,
                Jid trueCounterpart,
                BodyVersion body,
                long timeSent,
                int status,
                int type,
                boolean carbon,
                String remoteMsgId,
                String serverMsgId,
                boolean oob,
                String occupantId) {
            super(
                    conversation,
                    uuid,
                    conversationUUid,
                    counterpart,
                    trueCounterpart,
                    body.body(),
                    timeSent,
                    body.encryption(),
                    status,
                    type,
                    carbon,
                    remoteMsgId,
                    null,
                    serverMsgId,
                    body.fingerprint(),
                    true,
                    Collections.emptyList(),
                    oob,
                    null,
                    Collections.emptySet(),
                    false,
                    false,
                    null,
                    occupantId,
                    Collections.emptySet());
        }
    }
}
