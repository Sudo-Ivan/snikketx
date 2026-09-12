package eu.siacs.conversations.utils;

import android.Manifest;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.util.Log;
import androidx.core.app.NotificationChannelCompat;
import androidx.core.app.NotificationCompat;
import androidx.core.app.NotificationManagerCompat;
import androidx.core.content.ContextCompat;
import eu.siacs.conversations.BuildConfig;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.ui.XmppActivity;
import java.io.IOException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import org.json.JSONObject;

/**
 * Checks the user's own server for a newer SnikketX APK. The SnikketX portal exposes metadata at
 * /download/android.version when it hosts the app package.
 */
public final class UpdateChecker {

    private static final String PREFS = "app_update_check";
    private static final String KEY_LAST_CHECK = "last_check";
    private static final String KEY_LAST_NOTIFIED = "last_notified_version";
    private static final long CHECK_INTERVAL_MS = TimeUnit.HOURS.toMillis(24);
    private static final int NOTIFICATION_ID = 1024 * 1024 * 20;
    private static final String CHANNEL_ID = "app_updates";
    private static final String VERSION_ENDPOINT = "/download/android.version";
    private static final String APK_ENDPOINT = "/download/android.apk";

    private static final ExecutorService EXECUTOR = Executors.newSingleThreadExecutor();

    private UpdateChecker() {}

    public static void checkIfDue(final XmppActivity activity) {
        final var service = activity.xmppConnectionService;
        if (service == null) {
            return;
        }
        final Account account = AccountUtils.getFirstEnabled(service);
        if (account == null) {
            return;
        }
        final SharedPreferences prefs = activity.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        final long now = System.currentTimeMillis();
        if (now - prefs.getLong(KEY_LAST_CHECK, 0) < CHECK_INTERVAL_MS) {
            return;
        }
        prefs.edit().putLong(KEY_LAST_CHECK, now).apply();
        final String domain = account.getJid().getDomain().toString();
        final Context appContext = activity.getApplicationContext();
        EXECUTOR.execute(() -> fetchAndNotify(appContext, prefs, domain));
    }

    private static void fetchAndNotify(
            final Context context, final SharedPreferences prefs, final String domain) {
        final var client = new OkHttpClient.Builder().callTimeout(15, TimeUnit.SECONDS).build();
        final var request =
                new Request.Builder()
                        .url("https://" + domain + VERSION_ENDPOINT)
                        .header("Accept", "application/json")
                        .build();
        final String version;
        try (final var response = client.newCall(request).execute()) {
            if (!response.isSuccessful() || response.body() == null) {
                return;
            }
            version = new JSONObject(response.body().string()).optString("version", "");
        } catch (final IOException | org.json.JSONException e) {
            Log.d(Config.LOGTAG, "update check failed for " + domain, e);
            return;
        }
        if (version.isEmpty() || !isNewer(version, BuildConfig.VERSION_NAME)) {
            return;
        }
        if (version.equals(prefs.getString(KEY_LAST_NOTIFIED, null))) {
            return;
        }
        notifyUpdate(context, domain, version);
        prefs.edit().putString(KEY_LAST_NOTIFIED, version).apply();
    }

    static boolean isNewer(final String candidate, final String current) {
        final int[] a = parseVersion(candidate);
        final int[] b = parseVersion(current);
        if (a == null || b == null) {
            return false;
        }
        for (int i = 0; i < Math.max(a.length, b.length); i++) {
            final int x = i < a.length ? a[i] : 0;
            final int y = i < b.length ? b[i] : 0;
            if (x != y) {
                return x > y;
            }
        }
        return false;
    }

    private static int[] parseVersion(final String input) {
        if (input == null) {
            return null;
        }
        String s = input.trim();
        final int plus = s.indexOf('+');
        if (plus >= 0) {
            s = s.substring(0, plus);
        }
        final int firstDigit = s.indexOf('-');
        if (firstDigit > 0 && !s.substring(0, firstDigit).matches(".*\\d.*")) {
            s = s.substring(firstDigit + 1);
        }
        if (s.startsWith("v")) {
            s = s.substring(1);
        }
        if (!s.matches("\\d+(\\.\\d+)*")) {
            return null;
        }
        final String[] parts = s.split("\\.");
        final int[] out = new int[parts.length];
        for (int i = 0; i < parts.length; i++) {
            try {
                out[i] = Integer.parseInt(parts[i]);
            } catch (final NumberFormatException e) {
                return null;
            }
        }
        return out;
    }

    private static void notifyUpdate(
            final Context context, final String domain, final String version) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED) {
            return;
        }
        final var notificationManager = NotificationManagerCompat.from(context);
        final var channel =
                new NotificationChannelCompat.Builder(
                                CHANNEL_ID, NotificationManagerCompat.IMPORTANCE_DEFAULT)
                        .setName(context.getString(R.string.app_updates_channel_name))
                        .setShowBadge(false)
                        .build();
        notificationManager.createNotificationChannel(channel);

        final var apkUrl = "https://" + domain + APK_ENDPOINT;
        final var openIntent = new Intent(Intent.ACTION_VIEW, Uri.parse(apkUrl));
        final var pendingIntent =
                PendingIntent.getActivity(
                        context,
                        0,
                        openIntent,
                        PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        final var notification =
                new NotificationCompat.Builder(context, CHANNEL_ID)
                        .setSmallIcon(R.drawable.ic_app_icon_notification)
                        .setContentTitle(context.getString(R.string.update_available_title))
                        .setContentText(context.getString(R.string.update_available_text, version))
                        .setContentIntent(pendingIntent)
                        .setAutoCancel(true)
                        .build();
        try {
            notificationManager.notify(NOTIFICATION_ID, notification);
        } catch (final SecurityException e) {
            Log.d(Config.LOGTAG, "no notification permission for update notice");
        }
    }
}
