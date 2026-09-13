package eu.siacs.conversations.ui;

import android.os.Bundle;
import android.util.Log;
import android.view.WindowManager;
import androidx.activity.OnBackPressedCallback;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.biometric.BiometricManager;
import androidx.biometric.BiometricPrompt;
import androidx.core.content.ContextCompat;
import androidx.databinding.DataBindingUtil;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.ActivityLockBinding;
import eu.siacs.conversations.utils.AppLockManager;

public class LockActivity extends XmppActivity {

    private static final int AUTHENTICATORS =
            BiometricManager.Authenticators.BIOMETRIC_WEAK
                    | BiometricManager.Authenticators.DEVICE_CREDENTIAL;

    private ActivityLockBinding binding;

    @Override
    protected void onCreate(@Nullable final Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
        this.binding = DataBindingUtil.setContentView(this, R.layout.activity_lock);
        this.binding.message.setText(getString(R.string.app_locked, getString(R.string.app_name)));
        this.binding.unlock.setOnClickListener(v -> showPrompt());
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
        showPrompt();
    }

    @Override
    protected void onResume() {
        super.onResume();
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
    }

    private void showPrompt() {
        final var biometricManager = BiometricManager.from(this);
        final int canAuthenticate = biometricManager.canAuthenticate(AUTHENTICATORS);
        if (canAuthenticate != BiometricManager.BIOMETRIC_SUCCESS) {
            Log.d(Config.LOGTAG, "canAuthenticate()=" + canAuthenticate);
            this.binding.message.setText(R.string.no_unlock_method_available);
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
                                if (errorCode == BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL) {
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
    protected void onBackendConnected() {}
}
