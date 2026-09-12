package eu.siacs.conversations.services;

import android.annotation.TargetApi;
import android.content.Intent;
import android.content.pm.ShortcutManager;
import android.graphics.Bitmap;
import android.net.Uri;
import android.os.Build;
import android.os.PersistableBundle;
import android.util.Log;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.core.app.Person;
import androidx.core.content.pm.ShortcutInfoCompat;
import androidx.core.content.pm.ShortcutManagerCompat;
import androidx.core.graphics.drawable.IconCompat;
import com.google.common.base.Joiner;
import com.google.common.collect.ImmutableMap;
import com.google.common.collect.ImmutableSet;
import com.google.common.collect.Lists;
import com.google.common.collect.Maps;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.entities.Contact;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.MucOptions;
import eu.siacs.conversations.ui.ConversationsActivity;
import eu.siacs.conversations.ui.StartConversationActivity;
import eu.siacs.conversations.utils.ReplacingSerialSingleThreadExecutor;
import eu.siacs.conversations.utils.SerialSingleThreadExecutor;
import eu.siacs.conversations.xmpp.Jid;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;

public class ShortcutService {

    public static final char ID_SEPARATOR = '#';

    // category declared in the share-target element of res/xml/shortcuts.xml. matching this
    // category makes published conversation shortcuts eligible as direct share targets in the
    // system share sheet
    public static final String CATEGORY_TEXT_SHARE_TARGET =
            "org.snikketx.android.category.TEXT_SHARE_TARGET";

    private static final String CATEGORY_SHARE_TARGET =
            "eu.siacs.conversations.category.SHARE_TARGET";

    private static final int MAX_FREQUENT_CONTACTS = 4;

    private final XmppConnectionService xmppConnectionService;
    private final ReplacingSerialSingleThreadExecutor replacingSerialSingleThreadExecutor =
            new ReplacingSerialSingleThreadExecutor(ShortcutService.class.getSimpleName());
    private final SerialSingleThreadExecutor pushExecutor =
            new SerialSingleThreadExecutor(ShortcutService.class.getSimpleName() + "Push");

    // conversation shortcuts pushed via push(), most recently pushed last
    private final LinkedHashMap<String, ShortcutInfoCompat> pushedConversations =
            new LinkedHashMap<>();

    public ShortcutService(final XmppConnectionService xmppConnectionService) {
        this.xmppConnectionService = xmppConnectionService;
    }

    public void refresh() {
        refresh(false);
    }

    public void refresh(final boolean forceUpdate) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N_MR1) {
            final Runnable r = () -> refreshImpl(forceUpdate);
            replacingSerialSingleThreadExecutor.execute(r);
        }
    }

    @TargetApi(25)
    public void report(Contact contact) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N_MR1) {
            ShortcutManager shortcutManager =
                    xmppConnectionService.getSystemService(ShortcutManager.class);
            shortcutManager.reportShortcutUsed(getShortcutId(contact));
        }
    }

    @TargetApi(25)
    private void refreshImpl(final boolean forceUpdate) {
        final var frequentContacts = xmppConnectionService.databaseBackend.getFrequentContacts(30);
        final var accounts =
                ImmutableMap.copyOf(
                        Maps.uniqueIndex(xmppConnectionService.getAccounts(), Account::getUuid));
        final var contactBuilder = new ImmutableMap.Builder<FrequentContact, Contact>();
        final var count = new AtomicInteger();
        for (final var frequentContact : frequentContacts) {
            final Account account = accounts.get(frequentContact.account);
            if (account == null) {
                continue;
            }
            final var contact = account.getRoster().getContact(frequentContact.contact);
            if (contact.isSelf()) {
                continue;
            }
            if (count.getAndIncrement() < MAX_FREQUENT_CONTACTS) {
                contactBuilder.put(frequentContact, contact);
            }
        }
        final var contacts = contactBuilder.build();
        final int capacity =
                Math.max(
                        1,
                        ShortcutManagerCompat.getMaxShortcutCountPerActivity(
                                xmppConnectionService));
        final var newDynamicShortcuts = new ArrayList<ShortcutInfoCompat>();
        final var seen = new HashSet<String>();
        for (final var entry : contacts.entrySet()) {
            final var contact = entry.getValue();
            final var conversation = entry.getKey().conversation;
            final var shortcut = getShortcutInfo(contact, conversation);
            if (seen.add(shortcut.getId())) {
                newDynamicShortcuts.add(shortcut);
            }
        }
        // setDynamicShortcuts replaces the entire dynamic shortcut list. merge in pushed
        // conversation shortcuts (most recently used first) so they are not lost
        synchronized (pushedConversations) {
            for (final var shortcut :
                    Lists.reverse(new ArrayList<>(pushedConversations.values()))) {
                if (newDynamicShortcuts.size() >= capacity) {
                    break;
                }
                if (seen.add(shortcut.getId())) {
                    newDynamicShortcuts.add(shortcut);
                }
            }
        }
        final var current = ShortcutManagerCompat.getDynamicShortcuts(xmppConnectionService);
        // keep already published conversation shortcuts that are neither frequent contacts nor
        // tracked pushes, for example shortcuts pushed before a process restart
        for (final var shortcut : current) {
            if (newDynamicShortcuts.size() >= capacity) {
                break;
            }
            if (shortcut.getId().indexOf(ID_SEPARATOR) >= 0 && seen.add(shortcut.getId())) {
                newDynamicShortcuts.add(shortcut);
            }
        }
        final boolean needsUpdate = forceUpdate || shortcutsChanged(newDynamicShortcuts, current);
        if (!needsUpdate) {
            Log.d(Config.LOGTAG, "skipping shortcut update");
            return;
        }
        if (ShortcutManagerCompat.setDynamicShortcuts(xmppConnectionService, newDynamicShortcuts)) {
            Log.d(Config.LOGTAG, "updated dynamic shortcuts");
        } else {
            Log.d(Config.LOGTAG, "unable to update dynamic shortcuts");
        }
    }

    /**
     * Publishes or updates the dynamic shortcut for the given conversation and reports it as used.
     * Used when a conversation is opened or receives a new message so it is offered as a direct
     * share target and stays eligible for bubbles.
     */
    public void push(final Conversation conversation) {
        // building the shortcut involves decoding an avatar from disk and generating an
        // adaptive icon. keep that off the calling (usually main) thread
        pushExecutor.execute(
                () -> {
                    final var shortcut = getShortcutInfo(conversation);
                    if (shortcut == null) {
                        return;
                    }
                    push(shortcut);
                    ShortcutManagerCompat.reportShortcutUsed(
                            xmppConnectionService, shortcut.getId());
                });
    }

    public void push(final ShortcutInfoCompat shortcut) {
        final List<String> evicted;
        synchronized (pushedConversations) {
            pushedConversations.remove(shortcut.getId());
            pushedConversations.put(shortcut.getId(), shortcut);
            evicted = evictOverflow();
        }
        pushExecutor.execute(
                () -> {
                    if (!evicted.isEmpty()) {
                        // removeLongLivedShortcuts evicts from the dynamic list but keeps
                        // shortcuts the user pinned to the launcher
                        ShortcutManagerCompat.removeLongLivedShortcuts(
                                xmppConnectionService, evicted);
                    }
                    if (!ShortcutManagerCompat.pushDynamicShortcut(
                            xmppConnectionService, shortcut)) {
                        Log.d(Config.LOGTAG, "unable to push dynamic shortcut " + shortcut.getId());
                    }
                });
    }

    // caller must hold a lock on pushedConversations
    private List<String> evictOverflow() {
        final int capacity =
                Math.max(
                        1,
                        ShortcutManagerCompat.getMaxShortcutCountPerActivity(
                                xmppConnectionService));
        final var evicted = new ArrayList<String>();
        final var iterator = pushedConversations.keySet().iterator();
        while (pushedConversations.size() > capacity && iterator.hasNext()) {
            evicted.add(iterator.next());
            iterator.remove();
        }
        return evicted;
    }

    public void remove(final Conversation conversation) {
        final var id = getShortcutId(conversation);
        synchronized (pushedConversations) {
            pushedConversations.remove(id);
        }
        pushExecutor.execute(
                () ->
                        ShortcutManagerCompat.removeDynamicShortcuts(
                                xmppConnectionService, List.of(id)));
    }

    public void removeAll(final Account account) {
        final var prefix = account.getJid().asBareJid().toString() + ID_SEPARATOR;
        synchronized (pushedConversations) {
            pushedConversations.keySet().removeIf(id -> id.startsWith(prefix));
        }
        pushExecutor.execute(
                () -> {
                    final var stale = new ArrayList<String>();
                    for (final var shortcut :
                            ShortcutManagerCompat.getDynamicShortcuts(xmppConnectionService)) {
                        if (shortcut.getId().startsWith(prefix)) {
                            stale.add(shortcut.getId());
                        }
                    }
                    if (!stale.isEmpty()) {
                        ShortcutManagerCompat.removeDynamicShortcuts(xmppConnectionService, stale);
                    }
                });
    }

    public ShortcutInfoCompat getShortcutInfo(final Contact contact) {
        final var conversation = xmppConnectionService.find(contact);
        final var uuid = conversation == null ? null : conversation.getUuid();
        return getShortcutInfo(contact, uuid);
    }

    @Nullable
    public ShortcutInfoCompat getShortcutInfo(final Conversation conversation) {
        if (conversation.getMode() == Conversation.MODE_SINGLE) {
            final var contact = conversation.getContact();
            if (contact == null || contact.isSelf()) {
                return null;
            }
            return getShortcutInfo(contact, conversation.getUuid());
        }
        return getShortcutInfo(conversation.getMucOptions());
    }

    public ShortcutInfoCompat getShortcutInfo(final Contact contact, final String conversation) {
        final var icon = xmppConnectionService.getAvatarService().getAdaptive(contact);
        final ShortcutInfoCompat.Builder builder =
                new ShortcutInfoCompat.Builder(xmppConnectionService, getShortcutId(contact))
                        .setShortLabel(contact.getDisplayName())
                        .setIntent(getShortcutIntent(contact))
                        .setIsConversation()
                        .setLongLived(true)
                        .setPerson(getPerson(contact, icon));
        builder.setIcon(icon);
        if (conversation != null) {
            setConversation(builder, conversation);
        }
        return builder.build();
    }

    public ShortcutInfoCompat getShortcutInfo(final MucOptions mucOptions) {
        final var icon = xmppConnectionService.getAvatarService().getAdaptive(mucOptions);
        final ShortcutInfoCompat.Builder builder =
                new ShortcutInfoCompat.Builder(xmppConnectionService, getShortcutId(mucOptions))
                        .setShortLabel(mucOptions.getConversation().getName())
                        .setIntent(getShortcutIntent(mucOptions))
                        .setIsConversation()
                        .setLongLived(true)
                        .setPerson(getPerson(mucOptions, icon));
        builder.setIcon(icon);
        setConversation(builder, mucOptions.getConversation().getUuid());
        return builder.build();
    }

    private static Person getPerson(final Contact contact, @Nullable final IconCompat icon) {
        final var builder =
                new Person.Builder()
                        .setName(contact.getDisplayName())
                        .setKey(getShortcutId(contact));
        final Uri systemAccount = contact.getSystemAccount();
        if (systemAccount != null) {
            builder.setUri(systemAccount.toString());
        }
        if (icon != null) {
            builder.setIcon(icon);
        }
        return builder.build();
    }

    private static Person getPerson(final MucOptions mucOptions, @Nullable final IconCompat icon) {
        final var builder =
                new Person.Builder()
                        .setName(mucOptions.getConversation().getName())
                        .setKey(getShortcutId(mucOptions));
        if (icon != null) {
            builder.setIcon(icon);
        }
        return builder.build();
    }

    private static void setConversation(
            final ShortcutInfoCompat.Builder builder, @NonNull final String conversation) {
        builder.setCategories(ImmutableSet.of(CATEGORY_SHARE_TARGET, CATEGORY_TEXT_SHARE_TARGET));
        final var extras = new PersistableBundle();
        extras.putString(ConversationsActivity.EXTRA_CONVERSATION, conversation);
        builder.setExtras(extras);
    }

    private static boolean shortcutsChanged(
            final List<ShortcutInfoCompat> expected, final List<ShortcutInfoCompat> current) {
        if (expected.size() != current.size()) {
            return true;
        }
        for (int i = 0; i < expected.size(); ++i) {
            final var a = expected.get(i);
            final var b = current.get(i);
            if (!a.getId().equals(b.getId())) {
                return true;
            }
            if (!String.valueOf(a.getShortLabel()).equals(String.valueOf(b.getShortLabel()))) {
                return true;
            }
        }
        return false;
    }

    private static String getShortcutId(final Conversation conversation) {
        if (conversation.getMode() == Conversation.MODE_SINGLE) {
            return getShortcutId(conversation.getContact());
        }
        return getShortcutId(conversation.getMucOptions());
    }

    private static String getShortcutId(final Contact contact) {
        return Joiner.on(ID_SEPARATOR)
                .join(
                        contact.getAccount().getJid().asBareJid().toString(),
                        contact.getAddress().asBareJid().toString());
    }

    private static String getShortcutId(final MucOptions mucOptions) {
        final Account account = mucOptions.getAccount();
        final Jid jid = mucOptions.getConversation().getAddress();
        return Joiner.on(ID_SEPARATOR)
                .join(account.getJid().asBareJid().toString(), jid.asBareJid().toString());
    }

    private Intent getShortcutIntent(final MucOptions mucOptions) {
        final Account account = mucOptions.getAccount();
        return getShortcutIntent(
                account,
                Uri.parse(
                        String.format(
                                "xmpp:%s?join",
                                mucOptions.getConversation().getAddress().asBareJid().toString())));
    }

    private Intent getShortcutIntent(final Contact contact) {
        return getShortcutIntent(
                contact.getAccount(),
                Uri.parse("xmpp:" + contact.getAddress().asBareJid().toString()));
    }

    private Intent getShortcutIntent(final Account account, final Uri uri) {
        Intent intent = new Intent(xmppConnectionService, StartConversationActivity.class);
        intent.setAction(Intent.ACTION_VIEW);
        intent.setData(uri);
        intent.putExtra("account", account.getJid().asBareJid().toString());
        intent.setFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        return intent;
    }

    @NonNull
    public Intent createShortcut(final Contact contact, final boolean legacy) {
        Intent intent;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !legacy) {
            final var shortcut = getShortcutInfo(contact);
            intent =
                    ShortcutManagerCompat.createShortcutResultIntent(
                            xmppConnectionService, shortcut);
        } else {
            intent = createShortcutResultIntent(contact);
        }
        return intent;
    }

    @NonNull
    private Intent createShortcutResultIntent(final Contact contact) {
        AvatarService avatarService = xmppConnectionService.getAvatarService();
        Bitmap icon = avatarService.getRoundedShortcutWithIcon(contact);
        Intent intent = new Intent();
        intent.putExtra(Intent.EXTRA_SHORTCUT_NAME, contact.getDisplayName());
        intent.putExtra(Intent.EXTRA_SHORTCUT_ICON, icon);
        intent.putExtra(Intent.EXTRA_SHORTCUT_INTENT, getShortcutIntent(contact));
        return intent;
    }

    public static class FrequentContact {
        private final String conversation;
        private final String account;
        private final Jid contact;

        public FrequentContact(final String conversation, final String account, final Jid contact) {
            this.conversation = conversation;
            this.account = account;
            this.contact = contact;
        }
    }
}
