package eu.siacs.conversations.ui.fragment.settings;

import android.os.Bundle;
import android.widget.Toast;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.preference.EditTextPreference;
import com.google.common.base.Strings;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.R;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.Arrays;

public class PrivacySettingsFragment extends XmppPreferenceFragment {

    @Override
    public void onCreatePreferences(@Nullable Bundle savedInstanceState, @Nullable String rootKey) {
        setPreferencesFromResource(R.xml.preferences_privacy, rootKey);
        final EditTextPreference crashReportDsn = findPreference(AppSettings.CRASH_REPORT_DSN);
        if (crashReportDsn != null) {
            crashReportDsn.setOnPreferenceChangeListener(
                    (preference, newValue) -> {
                        if (newValue instanceof String dsn && isValidDsn(dsn.trim())) {
                            return true;
                        }
                        Toast.makeText(
                                        requireActivity(),
                                        R.string.invalid_crash_report_dsn,
                                        Toast.LENGTH_LONG)
                                .show();
                        return false;
                    });
        }
    }

    // empty restores the DSN compiled into the manifest. otherwise a sentry
    // compatible http(s) DSN is expected
    private static boolean isValidDsn(final String input) {
        if (Strings.isNullOrEmpty(input)) {
            return true;
        }
        final URI uri;
        try {
            uri = new URI(input);
        } catch (final URISyntaxException e) {
            return false;
        }
        return Arrays.asList("http", "https").contains(uri.getScheme())
                && uri.getHost() != null
                && uri.getRawPath() != null
                && uri.getRawPath().length() > 1;
    }

    @Override
    protected void onSharedPreferenceChanged(@NonNull String key) {
        super.onSharedPreferenceChanged(key);
        switch (key) {
            case AppSettings.READ_RECEIPTS,
                    AppSettings.BROADCAST_LAST_ACTIVITY,
                    AppSettings.ALLOW_MESSAGE_CORRECTION ->
                    requireService().refreshAllPresences();
        }
    }

    @Override
    public void onStart() {
        super.onStart();
        requireActivity().setTitle(R.string.pref_privacy);
    }
}
