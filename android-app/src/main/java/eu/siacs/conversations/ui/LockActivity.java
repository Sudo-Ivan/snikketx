package eu.siacs.conversations.ui;

import android.os.Bundle;
import android.util.Log;
import android.view.View;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import androidx.activity.OnBackPressedCallback;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.biometric.BiometricManager;
import androidx.biometric.BiometricPrompt;
import androidx.core.content.ContextCompat;
import androidx.databinding.DataBindingUtil;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.ActivityLockBinding;
import eu.siacs.conversations.utils.AppLockManager;

public class LockActivity extends XmppActivity {

    private static final int AUTHENTICATORS =
            BiometricManager.Authenticators.BIOMETRIC_WEAK
                    | BiometricManager.Authenticators.DEVICE_CREDENTIAL;

    private ActivityLockBinding binding;
    private volatile boolean wipePending = false;

    @Override
    protected void onCreate(@Nullable final Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
        this.binding = DataBindingUtil.setContentView(this, R.layout.activity_lock);
        this.binding.message.setText(getString(R.string.app_locked, getString(R.string.app_name)));
        final var actionBar = getSupportActionBar();
        if (actionBar != null) {
            actionBar.hide();
        }
        getOnBackPressedDispatcher()
                .addCallback(
                        this,
                        new OnBackPressedCallback(true) {
                            @Override
                            public void handleOnBackPressed() {
                                moveTaskToBack(true);
                            }
                        });
        if (new AppSettings(this).getAppLockCredential().isEmpty()) {
            this.binding.unlock.setOnClickListener(v -> showPrompt());
            showPrompt();
        } else {
            // a custom PIN is configured; biometrics stay reachable via the
            // secondary button so the PIN field remains the primary input
            this.binding.pinLayout.setVisibility(View.VISIBLE);
            this.binding.biometricUnlock.setVisibility(View.VISIBLE);
            this.binding.biometricUnlock.setOnClickListener(v -> showPrompt());
            this.binding.unlock.setOnClickListener(v -> submitCredential());
            this.binding.pin.setOnEditorActionListener(
                    (v, actionId, event) -> {
                        if (actionId == EditorInfo.IME_ACTION_DONE) {
                            submitCredential();
                            return true;
                        }
                        return false;
                    });
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
    }

    private void submitCredential() {
        final var editable = this.binding.pin.getText();
        final String entered = editable == null ? "" : editable.toString();
        final var appSettings = new AppSettings(this);
        if (AppLockManager.verifyCredential(appSettings.getAppLockCredential(), entered)) {
            AppLockManager.unlock();
            finish();
            return;
        }
        if (AppLockManager.verifyCredential(appSettings.getDuressCredential(), entered)) {
            runDuressAction(appSettings);
            return;
        }
        this.binding.pinLayout.setError(getString(R.string.wrong_pin_or_password));
        this.binding.pin.setText("");
    }

    private void runDuressAction(final AppSettings appSettings) {
        switch (appSettings.getDuressAction()) {
            case "wipe" -> {
                this.wipePending = true;
                this.binding.pin.setEnabled(false);
                this.binding.pinLayout.setEnabled(false);
                this.binding.unlock.setEnabled(false);
                if (xmppConnectionService != null) {
                    xmppConnectionService.wipeAllUserData();
                }
            }
            case "decoy" -> {
                final var uuid = appSettings.getDuressAccount();
                AppLockManager.activateDuress(uuid.isEmpty() ? null : uuid, false);
                finish();
            }
            case "fake" -> {
                AppLockManager.activateDuress(null, true);
                finish();
            }
            default -> {
                AppLockManager.unlock();
                finish();
            }
        }
    }

    private void showPrompt() {
        final var biometricManager = BiometricManager.from(this);
        final int canAuthenticate = biometricManager.canAuthenticate(AUTHENTICATORS);
        if (canAuthenticate != BiometricManager.BIOMETRIC_SUCCESS) {
            Log.d(Config.LOGTAG, "canAuthenticate()=" + canAuthenticate);
            if (this.binding.pinLayout.getVisibility() != View.VISIBLE) {
                this.binding.message.setText(R.string.no_unlock_method_available);
            }
            return;
        }
        final var prompt =
                new BiometricPrompt(
                        this,
                        ContextCompat.getMainExecutor(this),
                        new BiometricPrompt.AuthenticationCallback() {
                            @Override
                            public void onAuthenticationSucceeded(
                                    @NonNull final BiometricPrompt.AuthenticationResult result) {
                                AppLockManager.unlock();
                                finish();
                            }

                            @Override
                            public void onAuthenticationError(
                                    final int errorCode, @NonNull final CharSequence errString) {
                                Log.d(
                                        Config.LOGTAG,
                                        "authentication error " + errorCode + ": " + errString);
                                if (errorCode == BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL
                                        && binding.pinLayout.getVisibility() != View.VISIBLE) {
                                    binding.message.setText(R.string.no_screen_lock_configured);
                                }
                            }
                        });
        prompt.authenticate(
                new BiometricPrompt.PromptInfo.Builder()
                        .setTitle(getString(R.string.app_name))
                        .setSubtitle(getString(R.string.unlock_to_continue))
                        .setAllowedAuthenticators(AUTHENTICATORS)
                        .build());
    }

    @Override
    protected void refreshUiReal() {}

    @Override
    protected void onBackendConnected() {
        if (wipePending && xmppConnectionService != null) {
            xmppConnectionService.wipeAllUserData();
        }
    }
}
