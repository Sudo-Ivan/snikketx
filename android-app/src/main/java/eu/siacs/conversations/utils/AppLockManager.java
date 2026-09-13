package eu.siacs.conversations.utils;

import android.app.Activity;
import android.content.Intent;
import android.os.SystemClock;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.ui.LockActivity;

public final class AppLockManager {

    private static volatile long lastUnlock = 0;
    private static volatile boolean backgrounded = false;
    private static int startedActivities = 0;

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
        lastUnlock = SystemClock.elapsedRealtime();
        backgrounded = false;
    }
}
