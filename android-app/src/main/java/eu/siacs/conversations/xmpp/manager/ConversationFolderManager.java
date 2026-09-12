package eu.siacs.conversations.xmpp.manager;

import android.util.Log;
import androidx.annotation.NonNull;
import com.google.common.base.Strings;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.MoreExecutors;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.XmppConnection;
import im.conversations.android.xmpp.NodeConfiguration;
import im.conversations.android.xmpp.model.folders.Folders;
import im.conversations.android.xmpp.model.pubsub.Items;
import java.util.Map;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;

/**
 * Syncs conversation folder assignments across the account's devices via a private PEP node.
 *
 * <p>There is no ratified or experimental XEP for conversation folders, so this uses the SnikketX
 * namespaced node org.snikketx.folders:0 with a single item (id 'current') holding the full map of
 * conversation bare JID to folder name. The node is published with access model 'whitelist' so it
 * is never visible to contacts.
 *
 * <p>Merge semantics: a remote entry is applied only to local conversations that have no folder set
 * (local folder == null). An explicit local 'no folder' is indistinguishable from unset, so remote
 * wins only when local is null. Folder removals therefore do not propagate reliably; this is a
 * known and accepted limitation.
 */
public class ConversationFolderManager extends AbstractManager {

    private static final long PUBLISH_DEBOUNCE_MS = 750;

    private final XmppConnectionService service;
    private final ScheduledExecutorService publishScheduler =
            Executors.newSingleThreadScheduledExecutor();
    private ScheduledFuture<?> pendingPublish;

    public ConversationFolderManager(
            final XmppConnectionService service, final XmppConnection connection) {
        super(service.getApplicationContext(), connection);
        this.service = service;
    }

    public boolean hasFeature() {
        final var pep = getManager(PepManager.class);
        return pep.isAvailable() && pep.hasPublishOptions();
    }

    public void fetchAndMerge() {
        final var future =
                getManager(PubSubManager.class)
                        .fetchItems(
                                getAccount().getJid().asBareJid(),
                                Namespace.SNIKKETX_FOLDERS,
                                Folders.class);
        Futures.addCallback(
                future,
                new FutureCallback<>() {
                    @Override
                    public void onSuccess(final Map<String, Folders> items) {
                        Log.d(
                                Config.LOGTAG,
                                getAccount().getJid().asBareJid()
                                        + ": fetched "
                                        + items.size()
                                        + " folder item(s)");
                        for (final var folders : items.values()) {
                            merge(folders);
                        }
                    }

                    @Override
                    public void onFailure(@NonNull final Throwable throwable) {
                        Log.d(
                                Config.LOGTAG,
                                getAccount().getJid().asBareJid()
                                        + ": could not fetch conversation folders",
                                throwable);
                    }
                },
                MoreExecutors.directExecutor());
    }

    /**
     * Schedules a coalesced publish of the local folder map. Multiple calls within the debounce
     * window result in a single publish of the latest state.
     */
    public void schedulePublish() {
        if (!hasFeature()) {
            return;
        }
        synchronized (this) {
            if (this.pendingPublish != null) {
                this.pendingPublish.cancel(false);
            }
            this.pendingPublish =
                    this.publishScheduler.schedule(
                            this::publishSafely, PUBLISH_DEBOUNCE_MS, TimeUnit.MILLISECONDS);
        }
    }

    private void publishSafely() {
        try {
            publish();
        } catch (final Exception e) {
            Log.d(
                    Config.LOGTAG,
                    getAccount().getJid().asBareJid() + ": could not publish folders",
                    e);
        }
    }

    private void publish() {
        final var account = getAccount();
        final var folders = new Folders();
        for (final var conversation : service.getConversations()) {
            if (!account.getUuid().equals(conversation.getAccount().getUuid())) {
                continue;
            }
            final var folder = conversation.getFolder();
            if (Strings.isNullOrEmpty(folder)) {
                continue;
            }
            final var entry = folders.addConversation();
            entry.setJid(conversation.getAddress().asBareJid());
            entry.setName(folder);
        }
        final var future =
                getManager(PepManager.class)
                        .publishSingleton(
                                folders, Namespace.SNIKKETX_FOLDERS, NodeConfiguration.WHITELIST);
        Futures.addCallback(
                future,
                new FutureCallback<>() {
                    @Override
                    public void onSuccess(final Void result) {
                        Log.d(
                                Config.LOGTAG,
                                account.getJid().asBareJid() + ": published conversation folders");
                    }

                    @Override
                    public void onFailure(@NonNull final Throwable throwable) {
                        Log.d(
                                Config.LOGTAG,
                                account.getJid().asBareJid()
                                        + ": could not publish conversation folders",
                                throwable);
                    }
                },
                MoreExecutors.directExecutor());
    }

    public void handleItems(final Items items) {
        for (final var folders : items.getItemMap(Folders.class).values()) {
            merge(folders);
        }
    }

    private void merge(final Folders folders) {
        final var account = getAccount();
        boolean changed = false;
        for (final var entry : folders.getConversations()) {
            final var jid = Jid.Invalid.getNullForInvalid(entry.getJid());
            final var name = Strings.emptyToNull(entry.getFolderName());
            if (jid == null || name == null) {
                continue;
            }
            final var conversation = service.find(account, jid.asBareJid());
            if (conversation == null) {
                continue;
            }
            if (conversation.getFolder() == null) {
                conversation.setFolder(name);
                service.updateConversation(conversation);
                changed = true;
            }
        }
        if (changed) {
            service.updateConversationUi();
        }
    }
}
