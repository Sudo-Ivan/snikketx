package eu.siacs.conversations.ui;

import android.Manifest;
import android.annotation.SuppressLint;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.widget.Toast;
import androidx.annotation.NonNull;
import androidx.databinding.DataBindingUtil;
import androidx.recyclerview.widget.LinearLayoutManager;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.ActivityCallHistoryBinding;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.entities.Contact;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.services.CallIntegrationConnectionService;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.ui.adapter.CallHistoryAdapter;
import eu.siacs.conversations.ui.util.PresenceSelector;
import eu.siacs.conversations.utils.PermissionUtils;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.jingle.RtpCapability;
import eu.siacs.conversations.xmpp.manager.JingleManager;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

public class CallHistoryActivity extends XmppActivity {

    private static final int REQUEST_START_AUDIO_CALL = 0xca11;

    private ActivityCallHistoryBinding binding;
    private CallHistoryAdapter callHistoryAdapter;
    private final List<CallHistoryAdapter.CallLogEntry> entries = new ArrayList<>();
    private Conversation pendingCallConversation;

    @Override
    public void onCreate(final Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        this.binding = DataBindingUtil.setContentView(this, R.layout.activity_call_history);
        Activities.setStatusAndNavigationBarColors(this, binding.getRoot());
        setSupportActionBar(this.binding.toolbar);
        configureActionBar(getSupportActionBar());
        this.binding.toolbar.setTitle(R.string.calls);
        this.callHistoryAdapter = new CallHistoryAdapter(this, this.entries);
        this.callHistoryAdapter.setOnCallLogEntryClickListener(
                entry -> switchToConversation(entry.conversation));
        this.callHistoryAdapter.setOnCallBackClickListener(
                entry -> checkPermissionAndTriggerAudioCall(entry.conversation));
        this.binding.callHistory.setLayoutManager(new LinearLayoutManager(this));
        this.binding.callHistory.setAdapter(this.callHistoryAdapter);
    }

    @Override
    protected void onBackendConnected() {
        loadCallLog();
    }

    @Override
    protected void refreshUiReal() {
        if (xmppConnectionServiceBound) {
            loadCallLog();
        }
    }

    @SuppressLint("NotifyDataSetChanged")
    private void loadCallLog() {
        final XmppConnectionService service = xmppConnectionService;
        if (service == null) {
            return;
        }
        XmppConnectionService.DATABASE_READER.execute(
                () -> {
                    final List<CallHistoryAdapter.CallLogEntry> loaded = new ArrayList<>();
                    for (final Message message : service.databaseBackend.getRtpSessionMessages()) {
                        final Conversation conversation =
                                resolveConversation(service, message.getConversationUuid());
                        if (conversation != null) {
                            loaded.add(new CallHistoryAdapter.CallLogEntry(message, conversation));
                        }
                    }
                    runOnUiThread(
                            () -> {
                                this.entries.clear();
                                this.entries.addAll(loaded);
                                this.callHistoryAdapter.notifyDataSetChanged();
                                this.binding.callHistoryEmpty.setVisibility(
                                        this.entries.isEmpty() ? View.VISIBLE : View.GONE);
                            });
                });
    }

    private static Conversation resolveConversation(
            final XmppConnectionService service, final String uuid) {
        Conversation conversation = service.findConversationByUuid(uuid);
        if (conversation == null) {
            // conversation may be archived and therefore not in the loaded list
            conversation = service.databaseBackend.findConversation(uuid);
            if (conversation == null) {
                return null;
            }
            final Account account = service.findAccountByUuid(conversation.getAccountUuid());
            if (account == null) {
                return null;
            }
            conversation.setAccount(account);
        }
        return conversation;
    }

    private void checkPermissionAndTriggerAudioCall(final Conversation conversation) {
        final List<String> permissions;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            permissions =
                    Arrays.asList(
                            Manifest.permission.RECORD_AUDIO,
                            Manifest.permission.BLUETOOTH_CONNECT);
        } else {
            permissions = Collections.singletonList(Manifest.permission.RECORD_AUDIO);
        }
        if (PermissionUtils.hasPermission(this, permissions, REQUEST_START_AUDIO_CALL)) {
            triggerRtpSession(conversation);
        } else {
            this.pendingCallConversation = conversation;
        }
    }

    @Override
    public void onRequestPermissionsResult(
            final int requestCode,
            @NonNull final String[] permissions,
            @NonNull final int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        final PermissionUtils.PermissionResult permissionResult =
                PermissionUtils.removeBluetoothConnect(permissions, grantResults);
        if (permissionResult.grantResults.length == 0) {
            return;
        }
        if (requestCode == REQUEST_START_AUDIO_CALL) {
            final Conversation conversation = this.pendingCallConversation;
            this.pendingCallConversation = null;
            if (PermissionUtils.allGranted(permissionResult.grantResults) && conversation != null) {
                triggerRtpSession(conversation);
            } else {
                Toast.makeText(this, R.string.no_microphone_permission, Toast.LENGTH_SHORT).show();
            }
        }
    }

    private void triggerRtpSession(final Conversation conversation) {
        if (JingleManager.isBusy(xmppConnectionService.getAccounts())) {
            Toast.makeText(this, R.string.only_one_call_at_a_time, Toast.LENGTH_LONG).show();
            return;
        }
        final Account account = conversation.getAccount();
        if (account.setOption(Account.OPTION_SOFT_DISABLED, false)) {
            xmppConnectionService.updateAccount(account);
        }
        if (account.getXmppConnection() == null) {
            // getContact() would throw an NPE via getRoster() when the account has no
            // connection, so bail out before resolving it
            Toast.makeText(this, R.string.rtp_state_contact_offline, Toast.LENGTH_LONG).show();
            return;
        }
        final Contact contact = conversation.getContact();
        if (Config.USE_JINGLE_MESSAGE_INIT && RtpCapability.jmiSupport(contact)) {
            placeCall(account, contact.getAddress().asBareJid());
        } else {
            PresenceSelector.selectFullJidForDirectRtpConnection(
                    this,
                    contact,
                    RtpCapability.Capability.AUDIO,
                    fullJid -> placeCall(account, fullJid));
        }
    }

    private void placeCall(final Account account, final Jid with) {
        CallIntegrationConnectionService.placeCall(
                xmppConnectionService,
                account,
                with,
                RtpSessionActivity.actionToMedia(RtpSessionActivity.ACTION_MAKE_VOICE_CALL));
    }
}
