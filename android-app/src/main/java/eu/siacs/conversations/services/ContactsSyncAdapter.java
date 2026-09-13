package eu.siacs.conversations.services;

import android.Manifest;
import android.accounts.Account;
import android.accounts.AccountManager;
import android.content.AbstractThreadedSyncAdapter;
import android.content.ComponentName;
import android.content.ContentProviderClient;
import android.content.ContentProviderOperation;
import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.content.ServiceConnection;
import android.content.SyncResult;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.net.Uri;
import android.os.Bundle;
import android.os.IBinder;
import android.provider.BaseColumns;
import android.provider.ContactsContract;
import android.util.Log;
import androidx.core.content.ContextCompat;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.BuildConfig;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.android.JabberIdContact;
import eu.siacs.conversations.entities.Contact;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

public class ContactsSyncAdapter extends AbstractThreadedSyncAdapter {

    public static final String ACCOUNT_TYPE = BuildConfig.APPLICATION_ID;

    public static final String MIME_PROFILE =
            "vnd.android.cursor.item/vnd." + BuildConfig.APPLICATION_ID + ".profile";
    public static final String MIME_CALL =
            "vnd.android.cursor.item/vnd." + BuildConfig.APPLICATION_ID + ".call";
    public static final String MIME_VIDEO_CALL =
            "vnd.android.cursor.item/vnd." + BuildConfig.APPLICATION_ID + ".videocall";

    private static final String FIELD_JID = ContactsContract.RawContacts.SYNC1;

    public ContactsSyncAdapter(final Context context, final boolean autoInitialize) {
        super(context, autoInitialize);
    }

    @Override
    public void onPerformSync(
            final Account account,
            final Bundle extras,
            final String authority,
            final ContentProviderClient provider,
            final SyncResult syncResult) {
        final Context context = getContext();
        if (!QuickConversationsService.isContactListIntegration(context)
                || !new AppSettings(context).isContactsSync()
                || ContextCompat.checkSelfPermission(context, Manifest.permission.READ_CONTACTS)
                        != PackageManager.PERMISSION_GRANTED
                || ContextCompat.checkSelfPermission(context, Manifest.permission.WRITE_CONTACTS)
                        != PackageManager.PERMISSION_GRANTED) {
            // feature unavailable, disabled or contacts permission revoked; remove our rows
            removeAllSyncedContacts(context, account);
            return;
        }
        final var bound = bind(context);
        if (bound == null) {
            Log.w(Config.LOGTAG, "contact sync could not bind to XmppConnectionService");
            return;
        }
        try {
            performSync(context, account, bound.service());
        } finally {
            try {
                context.unbindService(bound.connection());
            } catch (final IllegalArgumentException ignored) {
                // connection was never registered or already released
            }
        }
    }

    private record BoundService(XmppConnectionService service, ServiceConnection connection) {}

    private BoundService bind(final Context context) {
        final var latch = new CountDownLatch(1);
        final var serviceReference = new AtomicReference<XmppConnectionService>();
        // must stay a local: onPerformSync can run on multiple threads concurrently, so a
        // shared field would race and unbind another sync's connection
        final var connection =
                new ServiceConnection() {
                    @Override
                    public void onServiceConnected(
                            final ComponentName name, final IBinder iBinder) {
                        final var binder = (XmppConnectionService.XmppConnectionBinder) iBinder;
                        serviceReference.set(binder.getService());
                        latch.countDown();
                    }

                    @Override
                    public void onServiceDisconnected(final ComponentName name) {}
                };
        final var intent = new Intent(context, XmppConnectionService.class);
        intent.setAction(XmppConnectionService.ACTION_CALL_INTEGRATION_SERVICE_STARTED);
        if (!context.bindService(intent, connection, Context.BIND_AUTO_CREATE)) {
            return null;
        }
        try {
            latch.await(10, TimeUnit.SECONDS);
        } catch (final InterruptedException e) {
            Thread.currentThread().interrupt();
        }
        final var service = serviceReference.get();
        if (service == null) {
            // the bind outlived the timeout; drop it so the service is not held forever
            try {
                context.unbindService(connection);
            } catch (final IllegalArgumentException ignored) {
            }
            return null;
        }
        return new BoundService(service, connection);
    }

    private void performSync(
            final Context context, final Account account, final XmppConnectionService service) {
        final Map<String, TargetContact> targets = new HashMap<>();
        for (final eu.siacs.conversations.entities.Account xmppAccount : service.getAccounts()) {
            if (!xmppAccount.isEnabled()) {
                continue;
            }
            for (final Contact contact :
                    xmppAccount.getRoster().getWithSystemAccounts(JabberIdContact.class)) {
                if (!contact.showInContactList() || contact.getSystemAccount() == null) {
                    continue;
                }
                targets.putIfAbsent(
                        contact.getAddress().asBareJid().toString(),
                        new TargetContact(xmppAccount.getUuid(), contact.getSystemAccount()));
            }
        }
        final Map<String, LinkedContact> linked = getLinkedContacts(context, account);
        // operations are flushed at contact boundaries because withValueBackReference() indices
        // only resolve within a single applyBatch() call
        final List<ContentProviderOperation> operations = new ArrayList<>();
        for (final var entry : targets.entrySet()) {
            final String jid = entry.getKey();
            final TargetContact target = entry.getValue();
            final LinkedContact existing = linked.remove(jid);
            final SystemContact systemContact =
                    resolveSystemContact(context, target.lookupUri, account);
            if (systemContact == null) {
                // the linked system contact is gone; drop our row if one exists
                if (existing != null) {
                    operations.add(deleteRawContactOperation(account, existing.id));
                }
                continue;
            }
            if (existing == null) {
                operations.addAll(
                        buildInsertOperations(
                                context,
                                operations.size(),
                                account,
                                jid,
                                target.accountUuid,
                                systemContact));
                if (operations.size() >= MAX_BATCH_SIZE) {
                    applyBatch(context, operations);
                }
            } else if (!Objects.equals(existing.displayName, systemContact.displayName)) {
                operations.add(
                        buildUpdateDisplayNameOperation(
                                systemContact.displayName,
                                existing.id,
                                existing.displayNameSource));
                if (operations.size() >= MAX_BATCH_SIZE) {
                    applyBatch(context, operations);
                }
            }
        }
        for (final LinkedContact stale : linked.values()) {
            operations.add(deleteRawContactOperation(account, stale.id));
            if (operations.size() >= MAX_BATCH_SIZE) {
                applyBatch(context, operations);
            }
        }
        applyBatch(context, operations);
    }

    private Map<String, LinkedContact> getLinkedContacts(
            final Context context, final Account account) {
        final Uri uri =
                ContactsContract.RawContacts.CONTENT_URI
                        .buildUpon()
                        .appendQueryParameter(
                                ContactsContract.RawContacts.ACCOUNT_NAME, account.name)
                        .appendQueryParameter(
                                ContactsContract.RawContacts.ACCOUNT_TYPE, account.type)
                        .build();
        final String[] projection = {
            BaseColumns._ID,
            FIELD_JID,
            ContactsContract.RawContacts.CONTACT_ID,
            ContactsContract.RawContacts.DISPLAY_NAME_PRIMARY,
            ContactsContract.RawContacts.DISPLAY_NAME_SOURCE,
            ContactsContract.RawContacts.DELETED
        };
        final Map<String, LinkedContact> result = new HashMap<>();
        final List<Long> toDelete = new ArrayList<>();
        try (final Cursor cursor =
                context.getContentResolver().query(uri, projection, null, null, null)) {
            if (cursor == null) {
                return result;
            }
            while (cursor.moveToNext()) {
                final long id = cursor.getLong(0);
                final String jid = cursor.getString(1);
                final boolean deleted = cursor.getInt(5) != 0;
                if (deleted || jid == null || result.containsKey(jid)) {
                    toDelete.add(id);
                    continue;
                }
                result.put(jid, new LinkedContact(id, cursor.getString(3), cursor.getInt(4)));
            }
        } catch (final Exception e) {
            Log.w(Config.LOGTAG, "could not query synced contacts", e);
        }
        for (final long id : toDelete) {
            deleteRawContact(context, account, id);
        }
        return result;
    }

    private SystemContact resolveSystemContact(
            final Context context, final Uri lookupUri, final Account ownAccount) {
        final long contactId;
        final String displayName;
        try (final Cursor cursor =
                context.getContentResolver()
                        .query(
                                lookupUri,
                                new String[] {
                                    ContactsContract.Contacts._ID,
                                    ContactsContract.Contacts.DISPLAY_NAME
                                },
                                null,
                                null,
                                null)) {
            if (cursor == null || !cursor.moveToNext()) {
                return null;
            }
            contactId = cursor.getLong(0);
            displayName = cursor.getString(1);
        } catch (final Exception e) {
            Log.d(Config.LOGTAG, "could not resolve lookup uri " + lookupUri, e);
            return null;
        }
        final long siblingRawContactId;
        try (final Cursor cursor =
                context.getContentResolver()
                        .query(
                                ContactsContract.RawContacts.CONTENT_URI,
                                new String[] {BaseColumns._ID},
                                ContactsContract.RawContacts.CONTACT_ID
                                        + "=? AND "
                                        + ContactsContract.RawContacts.DELETED
                                        + "=0 AND ("
                                        + ContactsContract.RawContacts.ACCOUNT_TYPE
                                        + " IS NULL OR "
                                        + ContactsContract.RawContacts.ACCOUNT_TYPE
                                        + "<>?)",
                                new String[] {String.valueOf(contactId), ownAccount.type},
                                null)) {
            if (cursor == null || !cursor.moveToNext()) {
                return null;
            }
            siblingRawContactId = cursor.getLong(0);
        } catch (final Exception e) {
            Log.d(Config.LOGTAG, "could not find sibling raw contact", e);
            return null;
        }
        return new SystemContact(contactId, siblingRawContactId, displayName);
    }

    private List<ContentProviderOperation> buildInsertOperations(
            final Context context,
            final int rawContactIndex,
            final Account account,
            final String jid,
            final String accountUuid,
            final SystemContact systemContact) {
        final String appName = context.getString(R.string.app_name);
        final Uri rawContactsUri =
                ContactsContract.RawContacts.CONTENT_URI
                        .buildUpon()
                        .appendQueryParameter(ContactsContract.CALLER_IS_SYNCADAPTER, "true")
                        .build();
        final Uri dataUri =
                ContactsContract.Data.CONTENT_URI
                        .buildUpon()
                        .appendQueryParameter(ContactsContract.CALLER_IS_SYNCADAPTER, "true")
                        .build();
        final var operations = new ArrayList<ContentProviderOperation>();
        operations.add(
                ContentProviderOperation.newInsert(rawContactsUri)
                        .withValue(ContactsContract.RawContacts.ACCOUNT_NAME, account.name)
                        .withValue(ContactsContract.RawContacts.ACCOUNT_TYPE, account.type)
                        .withValue(FIELD_JID, jid)
                        .build());
        operations.add(
                ContentProviderOperation.newInsert(dataUri)
                        .withValueBackReference(
                                ContactsContract.CommonDataKinds.StructuredName.RAW_CONTACT_ID,
                                rawContactIndex)
                        .withValue(
                                ContactsContract.Data.MIMETYPE,
                                ContactsContract.CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE)
                        .withValue(
                                ContactsContract.CommonDataKinds.StructuredName.DISPLAY_NAME,
                                systemContact.displayName)
                        .build());
        operations.add(
                buildActionOperation(
                        dataUri,
                        rawContactIndex,
                        MIME_PROFILE,
                        jid,
                        accountUuid,
                        appName,
                        context.getString(R.string.contact_action_message, jid)));
        operations.add(
                buildActionOperation(
                        dataUri,
                        rawContactIndex,
                        MIME_CALL,
                        jid,
                        accountUuid,
                        appName,
                        context.getString(R.string.contact_action_call, jid)));
        operations.add(
                buildActionOperation(
                        dataUri,
                        rawContactIndex,
                        MIME_VIDEO_CALL,
                        jid,
                        accountUuid,
                        appName,
                        context.getString(R.string.contact_action_video_call, jid)));
        operations.add(
                ContentProviderOperation.newUpdate(
                                ContactsContract.AggregationExceptions.CONTENT_URI)
                        .withValue(
                                ContactsContract.AggregationExceptions.RAW_CONTACT_ID1,
                                systemContact.siblingRawContactId)
                        .withValueBackReference(
                                ContactsContract.AggregationExceptions.RAW_CONTACT_ID2,
                                rawContactIndex)
                        .withValue(
                                ContactsContract.AggregationExceptions.TYPE,
                                ContactsContract.AggregationExceptions.TYPE_KEEP_TOGETHER)
                        .build());
        return operations;
    }

    private static ContentProviderOperation buildActionOperation(
            final Uri dataUri,
            final int rawContactIndex,
            final String mimeType,
            final String jid,
            final String accountUuid,
            final String appName,
            final String prompt) {
        return ContentProviderOperation.newInsert(dataUri)
                .withValueBackReference(ContactsContract.Data.RAW_CONTACT_ID, rawContactIndex)
                .withValue(ContactsContract.Data.MIMETYPE, mimeType)
                .withValue(ContactsContract.Data.DATA1, jid)
                .withValue(ContactsContract.Data.DATA2, appName)
                .withValue(ContactsContract.Data.DATA3, prompt)
                .withValue(ContactsContract.Data.DATA4, accountUuid)
                .withYieldAllowed(true)
                .build();
    }

    private static ContentProviderOperation buildUpdateDisplayNameOperation(
            final String displayName, final long rawContactId, final int displayNameSource) {
        final Uri dataUri =
                ContactsContract.Data.CONTENT_URI
                        .buildUpon()
                        .appendQueryParameter(ContactsContract.CALLER_IS_SYNCADAPTER, "true")
                        .build();
        if (displayNameSource != ContactsContract.DisplayNameSources.STRUCTURED_NAME) {
            return ContentProviderOperation.newInsert(dataUri)
                    .withValue(
                            ContactsContract.CommonDataKinds.StructuredName.RAW_CONTACT_ID,
                            rawContactId)
                    .withValue(
                            ContactsContract.Data.MIMETYPE,
                            ContactsContract.CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE)
                    .withValue(
                            ContactsContract.CommonDataKinds.StructuredName.DISPLAY_NAME,
                            displayName)
                    .build();
        }
        return ContentProviderOperation.newUpdate(dataUri)
                .withSelection(
                        ContactsContract.CommonDataKinds.StructuredName.RAW_CONTACT_ID
                                + "=? AND "
                                + ContactsContract.Data.MIMETYPE
                                + "=?",
                        new String[] {
                            String.valueOf(rawContactId),
                            ContactsContract.CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE
                        })
                .withValue(
                        ContactsContract.CommonDataKinds.StructuredName.DISPLAY_NAME, displayName)
                .build();
    }

    private static ContentProviderOperation deleteRawContactOperation(
            final Account account, final long rawContactId) {
        return ContentProviderOperation.newDelete(rawContactsUri(account))
                .withSelection(BaseColumns._ID + "=?", new String[] {String.valueOf(rawContactId)})
                .withYieldAllowed(true)
                .build();
    }

    private static void deleteRawContact(
            final Context context, final Account account, final long rawContactId) {
        context.getContentResolver()
                .delete(
                        rawContactsUri(account),
                        BaseColumns._ID + "=?",
                        new String[] {String.valueOf(rawContactId)});
    }

    private static Uri rawContactsUri(final Account account) {
        return ContactsContract.RawContacts.CONTENT_URI
                .buildUpon()
                .appendQueryParameter(ContactsContract.RawContacts.ACCOUNT_NAME, account.name)
                .appendQueryParameter(ContactsContract.RawContacts.ACCOUNT_TYPE, account.type)
                .appendQueryParameter(ContactsContract.CALLER_IS_SYNCADAPTER, "true")
                .build();
    }

    private static void removeAllSyncedContacts(final Context context, final Account account) {
        final Uri uri =
                ContactsContract.RawContacts.CONTENT_URI
                        .buildUpon()
                        .appendQueryParameter(
                                ContactsContract.RawContacts.ACCOUNT_NAME, account.name)
                        .appendQueryParameter(
                                ContactsContract.RawContacts.ACCOUNT_TYPE, account.type)
                        .build();
        try (final Cursor cursor =
                context.getContentResolver()
                        .query(uri, new String[] {BaseColumns._ID}, null, null, null)) {
            if (cursor == null) {
                return;
            }
            while (cursor.moveToNext()) {
                deleteRawContact(context, account, cursor.getLong(0));
            }
        } catch (final Exception e) {
            Log.d(Config.LOGTAG, "could not remove synced contacts", e);
        }
    }

    private static final int MAX_BATCH_SIZE = 300;

    private static void applyBatch(
            final Context context, final List<ContentProviderOperation> operations) {
        if (operations.isEmpty()) {
            return;
        }
        try {
            context.getContentResolver()
                    .applyBatch(ContactsContract.AUTHORITY, new ArrayList<>(operations));
        } catch (final Exception e) {
            Log.w(Config.LOGTAG, "could not apply contact sync batch", e);
        }
        operations.clear();
    }

    /**
     * Creates the stub sync account if necessary and asks the sync framework to run a sync. Safe to
     * call from a background thread.
     */
    public static void requestSync(final Context context) {
        if (!QuickConversationsService.isContactListIntegration(context)
                || ContextCompat.checkSelfPermission(context, Manifest.permission.READ_CONTACTS)
                        != PackageManager.PERMISSION_GRANTED) {
            return;
        }
        final Account account = getOrCreateSystemAccount(context);
        if (account == null) {
            return;
        }
        ContentResolver.requestSync(account, ContactsContract.AUTHORITY, new Bundle());
    }

    private static Account getOrCreateSystemAccount(final Context context) {
        final var accountManager = AccountManager.get(context);
        final var accounts = accountManager.getAccountsByType(ACCOUNT_TYPE);
        Account account = accounts.length > 0 ? accounts[0] : null;
        if (account == null) {
            final var newAccount = new Account(context.getString(R.string.app_name), ACCOUNT_TYPE);
            try {
                if (accountManager.addAccountExplicitly(newAccount, null, null)) {
                    ContentResolver.setIsSyncable(newAccount, ContactsContract.AUTHORITY, 1);
                    account = newAccount;
                } else {
                    Log.w(Config.LOGTAG, "could not create contacts sync account");
                    return null;
                }
            } catch (final SecurityException e) {
                Log.w(Config.LOGTAG, "could not create contacts sync account", e);
                return null;
            }
        }
        if (!ContentResolver.getSyncAutomatically(account, ContactsContract.AUTHORITY)) {
            ContentResolver.setSyncAutomatically(account, ContactsContract.AUTHORITY, true);
        }
        return account;
    }

    private static final class TargetContact {
        private final String accountUuid;
        private final Uri lookupUri;

        private TargetContact(final String accountUuid, final Uri lookupUri) {
            this.accountUuid = accountUuid;
            this.lookupUri = lookupUri;
        }
    }

    private static final class LinkedContact {
        private final long id;
        private final String displayName;
        private final int displayNameSource;

        private LinkedContact(
                final long id, final String displayName, final int displayNameSource) {
            this.id = id;
            this.displayName = displayName;
            this.displayNameSource = displayNameSource;
        }
    }

    private static final class SystemContact {
        private final long contactId;
        private final long siblingRawContactId;
        private final String displayName;

        private SystemContact(
                final long contactId, final long siblingRawContactId, final String displayName) {
            this.contactId = contactId;
            this.siblingRawContactId = siblingRawContactId;
            this.displayName = displayName;
        }
    }
}
