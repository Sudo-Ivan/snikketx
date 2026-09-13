package eu.siacs.conversations.ui;

import android.Manifest;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.net.Uri;
import android.os.Bundle;
import android.provider.ContactsContract;
import android.telecom.TelecomManager;
import android.telecom.VideoProfile;
import android.util.Log;
import androidx.appcompat.app.AppCompatActivity;
import com.google.common.collect.ImmutableSet;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.services.CallIntegration;
import eu.siacs.conversations.services.CallIntegrationConnectionService;
import eu.siacs.conversations.services.ContactsSyncAdapter;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.jingle.Media;
import java.util.Set;

/**
 * Handles taps on the "message/call via SnikketX" rows that the sync adapter adds to linked system
 * contacts. The system Contacts app fires ACTION_VIEW with the ContactsContract.Data row Uri and
 * our custom mime type.
 */
public class ContactsEntrypointActivity extends AppCompatActivity {

    @Override
    protected void onCreate(final Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        route(getIntent());
        finish();
    }

    private void route(final Intent intent) {
        final Uri data = intent == null ? null : intent.getData();
        final String type = intent == null ? null : intent.getType();
        if (data == null
                || type == null
                || !ContactsContract.AUTHORITY.equals(data.getAuthority())) {
            return;
        }
        final String jidString;
        final String accountUuid;
        try (final Cursor cursor =
                getContentResolver()
                        .query(
                                data,
                                new String[] {
                                    ContactsContract.Data.DATA1, ContactsContract.Data.DATA4
                                },
                                null,
                                null,
                                null)) {
            if (cursor == null || !cursor.moveToNext()) {
                return;
            }
            jidString = cursor.getString(0);
            accountUuid = cursor.getString(1);
        } catch (final Exception e) {
            Log.d(Config.LOGTAG, "unable to read synced contact row", e);
            return;
        }
        final Jid jid;
        try {
            jid = Jid.of(jidString).asBareJid();
        } catch (final IllegalArgumentException | NullPointerException e) {
            return;
        }
        if (ContactsSyncAdapter.MIME_CALL.equals(type)) {
            placeCall(jid, accountUuid, ImmutableSet.of(Media.AUDIO));
        } else if (ContactsSyncAdapter.MIME_VIDEO_CALL.equals(type)) {
            placeCall(jid, accountUuid, ImmutableSet.of(Media.AUDIO, Media.VIDEO));
        } else {
            openConversation(jid);
        }
    }

    private void openConversation(final Jid jid) {
        final var intent = new Intent(this, StartConversationActivity.class);
        intent.setAction(Intent.ACTION_VIEW);
        intent.setData(CallIntegration.address(jid));
        intent.addFlags(Intent.FLAG_ACTIVITY_REORDER_TO_FRONT);
        startActivity(intent);
    }

    private void placeCall(final Jid jid, final String accountUuid, final Set<Media> media) {
        if (!CallIntegration.selfManaged(this)
                || accountUuid == null
                || checkSelfPermission(Manifest.permission.MANAGE_OWN_CALLS)
                        != PackageManager.PERMISSION_GRANTED) {
            openConversation(jid);
            return;
        }
        try {
            final var extras = new Bundle();
            extras.putParcelable(
                    TelecomManager.EXTRA_PHONE_ACCOUNT_HANDLE,
                    CallIntegrationConnectionService.getHandle(this, accountUuid));
            extras.putInt(
                    TelecomManager.EXTRA_START_CALL_WITH_VIDEO_STATE,
                    Media.audioOnly(media)
                            ? VideoProfile.STATE_AUDIO_ONLY
                            : VideoProfile.STATE_BIDIRECTIONAL);
            getSystemService(TelecomManager.class).placeCall(CallIntegration.address(jid), extras);
        } catch (final Exception e) {
            Log.w(Config.LOGTAG, "could not place call from contacts entrypoint", e);
            openConversation(jid);
        }
    }
}
