package eu.siacs.conversations.utils;

import android.app.Activity;
import android.content.Intent;
import android.os.SystemClock;
import android.util.Base64;
import androidx.annotation.Nullable;
import com.google.common.base.Strings;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.ui.LockActivity;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.security.spec.InvalidKeySpecException;
import java.security.spec.KeySpec;
import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.PBEKeySpec;

public final class AppLockManager {

    private static final int PBKDF2_ITERATIONS = 12000;
    private static final int SALT_BYTES = 16;
    private static final int KEY_BITS = 256;

    private static volatile long lastUnlock = 0;
    private static volatile boolean backgrounded = false;
    private static int startedActivities = 0;

    // session scoped duress state. duressAccountUuid == null with fake == false hides
    // every account; fake == true surfaces the fabricated account instead
    private static volatile boolean duressActive = false;
    private static volatile String duressAccountUuid = null;
    private static volatile boolean duressFake = false;

    private AppLockManager() {}

    public static void onActivityStarted() {
        ++startedActivities;
    }

    public static void onActivityStopped() {
        if (startedActivities > 0) {
            --startedActivities;
        }
        if (startedActivities == 0) {
            backgrounded = true;
        }
    }

    public static void onActivityResumed(final Activity activity) {
        if (activity instanceof LockActivity) {
            return;
        }
        if (shouldLock(activity)) {
            activity.startActivity(new Intent(activity, LockActivity.class));
        }
    }

    public static boolean shouldLock(final Activity activity) {
        final var appSettings = new AppSettings(activity);
        if (!appSettings.isAppLockEnabled()) {
            return false;
        }
        if (lastUnlock == 0L) {
            return true;
        }
        final long timeout = appSettings.getAppLockTimeout();
        if (timeout <= 0L) {
            return backgrounded;
        }
        return SystemClock.elapsedRealtime() - lastUnlock >= timeout * 1000L;
    }

    public static void unlock() {
        duressActive = false;
        duressAccountUuid = null;
        duressFake = false;
        lastUnlock = SystemClock.elapsedRealtime();
        backgrounded = false;
    }

    public static void activateDuress(@Nullable final String accountUuid, final boolean fake) {
        // entering duress mode counts as an unlock; the flags are set after reset
        unlock();
        duressActive = true;
        duressAccountUuid = accountUuid;
        duressFake = fake;
    }

    public static boolean isDuressActive() {
        return duressActive;
    }

    public static boolean isDuressFake() {
        return duressFake;
    }

    @Nullable
    public static String getDuressAccountUuid() {
        return duressAccountUuid;
    }

    public static boolean isHidden(final Account account) {
        if (!duressActive || account == null) {
            return false;
        }
        if (duressFake) {
            return true;
        }
        return duressAccountUuid == null || !duressAccountUuid.equals(account.getUuid());
    }

    // stores a credential as salt:hash so the PIN itself is never persisted
    @Nullable
    public static String hashCredential(final String secret) {
        if (Strings.isNullOrEmpty(secret)) {
            return null;
        }
        final var salt = new byte[SALT_BYTES];
        Random.SECURE_RANDOM.nextBytes(salt);
        final var hash = pbkdf2(secret, salt);
        if (hash == null) {
            return null;
        }
        return Base64.encodeToString(salt, Base64.NO_WRAP)
                + ":"
                + Base64.encodeToString(hash, Base64.NO_WRAP);
    }

    public static boolean verifyCredential(
            @Nullable final String stored, @Nullable final String secret) {
        if (Strings.isNullOrEmpty(stored) || Strings.isNullOrEmpty(secret)) {
            return false;
        }
        final var parts = stored.split(":", 2);
        if (parts.length != 2) {
            return false;
        }
        final byte[] salt;
        final byte[] expected;
        try {
            salt = Base64.decode(parts[0], Base64.NO_WRAP);
            expected = Base64.decode(parts[1], Base64.NO_WRAP);
        } catch (final IllegalArgumentException e) {
            return false;
        }
        final var actual = pbkdf2(secret, salt);
        return actual != null && MessageDigest.isEqual(expected, actual);
    }

    @Nullable
    private static byte[] pbkdf2(final String secret, final byte[] salt) {
        try {
            final KeySpec spec =
                    new PBEKeySpec(secret.toCharArray(), salt, PBKDF2_ITERATIONS, KEY_BITS);
            return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256")
                    .generateSecret(spec)
                    .getEncoded();
        } catch (final NoSuchAlgorithmException | InvalidKeySpecException e) {
            return null;
        }
    }
}
