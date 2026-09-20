package eu.siacs.conversations.ui.fragment.settings;

import android.app.KeyguardManager;
import android.content.Context;
import android.content.DialogInterface;
import android.os.Bundle;
import android.text.InputType;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.Toast;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.appcompat.app.AlertDialog;
import androidx.preference.ListPreference;
import androidx.preference.Preference;
import androidx.preference.SwitchPreferenceCompat;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import com.google.common.base.Strings;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.R;
import eu.siacs.conversations.crypto.OmemoSetting;
import eu.siacs.conversations.services.MemorizingTrustManager;
import eu.siacs.conversations.services.QuickConversationsService;
import eu.siacs.conversations.utils.AppLockManager;
import eu.siacs.conversations.utils.TimeFrameUtils;
import java.security.KeyStoreException;
import java.util.ArrayList;
import java.util.Collections;

public class SecuritySettingsFragment extends XmppPreferenceFragment {

    private static final String REMOVE_TRUSTED_CERTIFICATES = "remove_trusted_certificates";
    private static final String SERVER_CONNECTION = "server_connection";

    @Override
    public void onCreatePreferences(@Nullable Bundle savedInstanceState, @Nullable String rootKey) {
        setPreferencesFromResource(R.xml.preferences_security, rootKey);
        final ListPreference omemo = findPreference(AppSettings.OMEMO);
        final ListPreference automaticMessageDeletion =
                findPreference(AppSettings.AUTOMATIC_MESSAGE_DELETION);
        final SwitchPreferenceCompat appLock = findPreference(AppSettings.APP_LOCK);
        final ListPreference appLockTimeout = findPreference(AppSettings.APP_LOCK_TIMEOUT);
        final Preference appLockCredential = findPreference(AppSettings.APP_LOCK_CREDENTIAL);
        final Preference duressCredential = findPreference(AppSettings.DURESS_CREDENTIAL);
        final ListPreference duressAction = findPreference(AppSettings.DURESS_ACTION);
        final ListPreference duressAccount = findPreference(AppSettings.DURESS_ACCOUNT);
        final Preference serverConnection = findPreference(SERVER_CONNECTION);
        if (omemo == null
                || automaticMessageDeletion == null
                || appLock == null
                || appLockTimeout == null
                || appLockCredential == null
                || duressCredential == null
                || duressAction == null
                || duressAccount == null
                || serverConnection == null) {
            throw new IllegalStateException("The preference resource file is missing preferences");
        }
        omemo.setSummaryProvider(new OmemoSummaryProvider());
        setValues(
                automaticMessageDeletion,
                R.array.automatic_message_deletion_values,
                value -> timeframeValueToName(requireContext(), value));
        setValues(
                appLockTimeout,
                R.array.app_lock_timeout_values,
                value -> appLockTimeoutToName(requireContext(), value));
        appLock.setOnPreferenceChangeListener(
                (preference, newValue) -> {
                    if (!Boolean.TRUE.equals(newValue)) {
                        return true;
                    }
                    final var keyguardManager =
                            requireContext().getSystemService(KeyguardManager.class);
                    final boolean deviceSecure =
                            keyguardManager != null && keyguardManager.isDeviceSecure();
                    final boolean hasPin =
                            !new AppSettings(requireContext()).getAppLockCredential().isEmpty();
                    if (!deviceSecure && !hasPin) {
                        Toast.makeText(
                                        requireContext(),
                                        R.string.app_lock_requires_screen_lock,
                                        Toast.LENGTH_LONG)
                                .show();
                        return false;
                    }
                    AppLockManager.unlock();
                    return true;
                });
        refreshCredentialSummaries();
        duressAccount.setEnabled(
                "decoy".equals(new AppSettings(requireContext()).getDuressAction()));
        if (QuickConversationsService.isQuicksy()) {
            serverConnection.setVisible(false);
        }
    }

    @Override
    public void onBackendConnected() {
        final ListPreference duressAccount = findPreference(AppSettings.DURESS_ACCOUNT);
        if (duressAccount == null) {
            return;
        }
        final var accounts = requireService().getAccounts();
        final var entries = new ArrayList<CharSequence>();
        final var values = new ArrayList<CharSequence>();
        for (final var account : accounts) {
            entries.add(account.getJid().asBareJid().toString());
            values.add(account.getUuid());
        }
        duressAccount.setEntries(entries.toArray(new CharSequence[0]));
        duressAccount.setEntryValues(values.toArray(new CharSequence[0]));
    }

    private void refreshCredentialSummaries() {
        final var appSettings = new AppSettings(requireContext());
        final Preference appLockCredential = findPreference(AppSettings.APP_LOCK_CREDENTIAL);
        final Preference duressCredential = findPreference(AppSettings.DURESS_CREDENTIAL);
        if (appLockCredential != null) {
            appLockCredential.setSummary(
                    appSettings.getAppLockCredential().isEmpty()
                            ? R.string.pin_not_set
                            : R.string.pin_is_set);
        }
        if (duressCredential != null) {
            duressCredential.setSummary(
                    appSettings.getDuressCredential().isEmpty()
                            ? R.string.pin_not_set
                            : R.string.pin_is_set);
        }
    }

    @Override
    protected void onSharedPreferenceChanged(@NonNull String key) {
        super.onSharedPreferenceChanged(key);
        switch (key) {
            case AppSettings.OMEMO -> {
                OmemoSetting.load(requireContext());
            }
            case AppSettings.TRUST_SYSTEM_CA_STORE -> {
                requireService().updateMemorizingTrustManager();
                reconnectAccounts();
            }
            case AppSettings.REQUIRE_CHANNEL_BINDING, AppSettings.REQUIRE_TLS_V1_3 -> {
                reconnectAccounts();
            }
            case AppSettings.AUTOMATIC_MESSAGE_DELETION -> {
                requireService().expireOldMessages(true);
            }
            case AppSettings.DURESS_ACTION -> {
                final ListPreference duressAccount = findPreference(AppSettings.DURESS_ACCOUNT);
                if (duressAccount != null) {
                    duressAccount.setEnabled(
                            "decoy".equals(new AppSettings(requireContext()).getDuressAction()));
                }
            }
        }
    }

    @Override
    public void onStart() {
        super.onStart();
        requireActivity().setTitle(R.string.pref_title_security);
    }

    @Override
    public boolean onPreferenceTreeClick(final Preference preference) {
        if (REMOVE_TRUSTED_CERTIFICATES.equals(preference.getKey())) {
            showRemoveCertificatesDialog();
            return true;
        }
        if (AppSettings.APP_LOCK_CREDENTIAL.equals(preference.getKey())) {
            showCredentialDialog(false);
            return true;
        }
        if (AppSettings.DURESS_CREDENTIAL.equals(preference.getKey())) {
            showCredentialDialog(true);
            return true;
        }
        return super.onPreferenceTreeClick(preference);
    }

    private void showCredentialDialog(final boolean duress) {
        final var appSettings = new AppSettings(requireContext());
        final String existing =
                duress ? appSettings.getDuressCredential() : appSettings.getAppLockCredential();
        final var input = new EditText(requireContext());
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        input.setHint(duress ? R.string.pref_duress_pin : R.string.pref_app_lock_pin);
        final int margin = (int) (24 * getResources().getDisplayMetrics().density);
        final var container = new FrameLayout(requireContext());
        final var layoutParams =
                new FrameLayout.LayoutParams(
                        FrameLayout.LayoutParams.MATCH_PARENT,
                        FrameLayout.LayoutParams.WRAP_CONTENT);
        layoutParams.leftMargin = margin;
        layoutParams.rightMargin = margin;
        input.setLayoutParams(layoutParams);
        container.addView(input);
        final var builder = new MaterialAlertDialogBuilder(requireActivity());
        builder.setTitle(duress ? R.string.pref_duress_pin : R.string.pref_app_lock_pin);
        builder.setView(container);
        builder.setNegativeButton(R.string.cancel, null);
        builder.setPositiveButton(
                R.string.save,
                (dialog, which) -> saveCredential(duress, input.getText().toString()));
        if (!existing.isEmpty()) {
            builder.setNeutralButton(
                    R.string.remove_pin,
                    (dialog, which) -> {
                        if (duress) {
                            appSettings.setDuressCredential(null);
                        } else {
                            appSettings.setAppLockCredential(null);
                        }
                        refreshCredentialSummaries();
                    });
        }
        builder.create().show();
    }

    private void saveCredential(final boolean duress, final String secret) {
        if (secret == null || secret.length() < 4) {
            Toast.makeText(requireContext(), R.string.pin_too_short, Toast.LENGTH_LONG).show();
            return;
        }
        final var appSettings = new AppSettings(requireContext());
        // the two credentials must not collide; otherwise the duress PIN would be
        // reachable by the primary PIN and vice versa
        final String other =
                duress ? appSettings.getAppLockCredential() : appSettings.getDuressCredential();
        if (AppLockManager.verifyCredential(other, secret)) {
            Toast.makeText(requireContext(), R.string.pins_must_differ, Toast.LENGTH_LONG).show();
            return;
        }
        final var hashed = AppLockManager.hashCredential(secret);
        if (hashed == null) {
            return;
        }
        if (duress) {
            appSettings.setDuressCredential(hashed);
        } else {
            appSettings.setAppLockCredential(hashed);
        }
        refreshCredentialSummaries();
    }

    private void showRemoveCertificatesDialog() {
        final MemorizingTrustManager mtm = requireService().getMemorizingTrustManager();
        final ArrayList<String> aliases = Collections.list(mtm.getCertificates());
        if (aliases.isEmpty()) {
            Toast.makeText(requireActivity(), R.string.toast_no_trusted_certs, Toast.LENGTH_LONG)
                    .show();
            return;
        }
        final ArrayList<Integer> selectedItems = new ArrayList<>();
        final MaterialAlertDialogBuilder dialogBuilder =
                new MaterialAlertDialogBuilder(requireActivity());
        dialogBuilder.setTitle(getString(R.string.dialog_manage_certs_title));
        dialogBuilder.setMultiChoiceItems(
                aliases.toArray(new CharSequence[0]),
                null,
                (dialog, indexSelected, isChecked) -> {
                    if (isChecked) {
                        selectedItems.add(indexSelected);
                    } else if (selectedItems.contains(indexSelected)) {
                        selectedItems.remove(Integer.valueOf(indexSelected));
                    }
                    if (dialog instanceof AlertDialog alertDialog) {
                        alertDialog
                                .getButton(DialogInterface.BUTTON_POSITIVE)
                                .setEnabled(!selectedItems.isEmpty());
                    }
                });

        dialogBuilder.setPositiveButton(
                getString(R.string.dialog_manage_certs_positivebutton),
                (dialog, which) -> confirmCertificateDeletion(aliases, selectedItems));
        dialogBuilder.setNegativeButton(R.string.cancel, null);
        final AlertDialog removeCertsDialog = dialogBuilder.create();
        removeCertsDialog.show();
        removeCertsDialog.getButton(AlertDialog.BUTTON_POSITIVE).setEnabled(false);
    }

    private void confirmCertificateDeletion(
            final ArrayList<String> aliases, final ArrayList<Integer> selectedItems) {
        final int count = selectedItems.size();
        if (count == 0) {
            return;
        }
        final MemorizingTrustManager mtm = requireService().getMemorizingTrustManager();
        for (int i = 0; i < count; i++) {
            try {
                final int item = Integer.parseInt(selectedItems.get(i).toString());
                final String alias = aliases.get(item);
                mtm.deleteCertificate(alias);
            } catch (final KeyStoreException e) {
                Toast.makeText(
                                requireActivity(),
                                "Error: " + e.getLocalizedMessage(),
                                Toast.LENGTH_LONG)
                        .show();
            }
        }
        reconnectAccounts();
        Toast.makeText(
                        requireActivity(),
                        getResources()
                                .getQuantityString(
                                        R.plurals.toast_delete_certificates, count, count),
                        Toast.LENGTH_LONG)
                .show();
    }

    private static String appLockTimeoutToName(final Context context, final int value) {
        if (value <= 0) {
            return context.getString(R.string.immediately);
        }
        return TimeFrameUtils.resolve(context, 1000L * value);
    }

    private static class OmemoSummaryProvider
            implements Preference.SummaryProvider<ListPreference> {

        @Nullable
        @Override
        public CharSequence provideSummary(@NonNull ListPreference preference) {
            final var context = preference.getContext();
            final var sharedPreferences = preference.getSharedPreferences();
            final String value;
            if (sharedPreferences == null) {
                value = null;
            } else {
                value =
                        sharedPreferences.getString(
                                preference.getKey(),
                                context.getString(R.string.omemo_setting_default));
            }
            return switch (Strings.nullToEmpty(value)) {
                case "always" -> context.getString(R.string.pref_omemo_setting_summary_always);
                case "default_off" ->
                        context.getString(R.string.pref_omemo_setting_summary_default_off);
                default -> context.getString(R.string.pref_omemo_setting_summary_default_on);
            };
        }
    }
}
