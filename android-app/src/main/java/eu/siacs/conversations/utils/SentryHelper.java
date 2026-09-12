package eu.siacs.conversations.utils;

import android.content.Context;
import android.content.SharedPreferences;
import androidx.preference.PreferenceManager;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.BuildConfig;
import io.sentry.Breadcrumb;
import io.sentry.Sentry;
import io.sentry.SentryEvent;
import io.sentry.android.core.SentryAndroid;
import io.sentry.protocol.Contexts;
import io.sentry.protocol.Mechanism;
import io.sentry.protocol.Message;
import io.sentry.protocol.Request;
import io.sentry.protocol.SentryException;
import io.sentry.protocol.SentryStackFrame;
import io.sentry.protocol.SentryStackTrace;
import io.sentry.protocol.SentryThread;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Pattern;

/**
 * Crash reporting to a self hosted Sentry protocol compatible backend (Bugsink).
 *
 * <p>The DSN is declared as io.sentry.dsn meta-data in the manifest and io.sentry.auto-init is set
 * to false, so that the SentryInitProvider does not initialise the SDK before the
 * send_crash_reports user preference has been consulted. The SDK is initialised here from
 * Application.onCreate and kept in sync with the preference at runtime via a preference change
 * listener.
 */
public final class SentryHelper {

    // xmpp addresses and email like strings (local@domain), including an optional /resource
    private static final Pattern JID_PATTERN =
            Pattern.compile("[\\w.!#$%&'*+=?^`{|}~-]+@[\\w.-]+(?:/\\S*)?");

    private static final Pattern IPV4_PATTERN = Pattern.compile("\\b(?:\\d{1,3}\\.){3}\\d{1,3}\\b");

    // full eight segment form, or any compressed form that contains a double colon and is
    // preceded by whitespace or an opening bracket, so that times like 12:34:56 and scoped
    // names like Class::method are left alone
    private static final Pattern IPV6_PATTERN =
            Pattern.compile(
                    "(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}"
                            + "|(?<=^|[\\s'\"(\\[])(?:[0-9a-fA-F]{0,4}:)+:(?:[0-9a-fA-F]{0,4}:?)*");

    // absolute paths under app private or shared storage
    private static final Pattern FILE_PATH_PATTERN =
            Pattern.compile("/(?:data|storage|sdcard|mnt)/[^\\s'\")\\]},;]*");

    // SharedPreferences holds listeners in a WeakHashMap, so a strong reference is required
    private static SharedPreferences.OnSharedPreferenceChangeListener preferenceListener;

    private SentryHelper() {}

    public static synchronized void init(final Context context) {
        final Context appContext = context.getApplicationContext();
        final AppSettings appSettings = new AppSettings(appContext);
        SentryAndroid.init(
                appContext,
                options -> {
                    final String dsn = options.getDsn();
                    // a disabled SDK installs no integrations and captures nothing; guarding on
                    // an empty or missing dsn additionally avoids the SDK throwing on init
                    options.setEnabled(
                            appSettings.isSendCrashReports() && dsn != null && !dsn.isEmpty());
                    options.setSendDefaultPii(false);
                    options.setAttachScreenshot(false);
                    options.setAttachViewHierarchy(false);
                    // Bugsink does not consume session envelopes; skip them to save traffic
                    options.setEnableAutoSessionTracking(false);
                    options.setEnvironment(BuildConfig.BUILD_TYPE);
                    options.setBeforeSend(
                            (event, hint) -> {
                                // defense in depth in case the preference was toggled after
                                // the SDK was initialised
                                if (!appSettings.isSendCrashReports()) {
                                    return null;
                                }
                                return scrub(event);
                            });
                });
        registerPreferenceListener(appContext);
    }

    private static synchronized void registerPreferenceListener(final Context context) {
        if (preferenceListener != null) {
            return;
        }
        preferenceListener =
                (sharedPreferences, key) -> {
                    if (!AppSettings.SEND_CRASH_REPORTS.equals(key)) {
                        return;
                    }
                    if (new AppSettings(context).isSendCrashReports()) {
                        init(context);
                    } else {
                        Sentry.close();
                    }
                };
        PreferenceManager.getDefaultSharedPreferences(context)
                .registerOnSharedPreferenceChangeListener(preferenceListener);
    }

    private static SentryEvent scrub(final SentryEvent event) {
        event.setUser(null);
        event.setServerName(null);
        event.setTransaction(scrub(event.getTransaction()));

        final Message message = event.getMessage();
        if (message != null) {
            message.setMessage(scrub(message.getMessage()));
            message.setFormatted(scrub(message.getFormatted()));
            final List<String> params = message.getParams();
            if (params != null) {
                for (int i = 0; i < params.size(); ++i) {
                    params.set(i, scrub(params.get(i)));
                }
            }
        }

        final List<SentryException> exceptions = event.getExceptions();
        if (exceptions != null) {
            for (final SentryException exception : exceptions) {
                // the exception message is where stanza data and JIDs end up
                exception.setValue(scrub(exception.getValue()));
                final Mechanism mechanism = exception.getMechanism();
                if (mechanism != null) {
                    scrubMap(mechanism.getData());
                    scrubMap(mechanism.getMeta());
                }
                final SentryStackTrace stackTrace = exception.getStacktrace();
                if (stackTrace != null && stackTrace.getFrames() != null) {
                    for (final SentryStackFrame frame : stackTrace.getFrames()) {
                        frame.setAbsPath(scrub(frame.getAbsPath()));
                    }
                }
            }
        }

        // thread names may contain account JIDs
        final List<SentryThread> threads = event.getThreads();
        if (threads != null) {
            for (final SentryThread thread : threads) {
                thread.setName(scrub(thread.getName()));
            }
        }

        final List<Breadcrumb> breadcrumbs = event.getBreadcrumbs();
        if (breadcrumbs != null) {
            for (final Breadcrumb breadcrumb : breadcrumbs) {
                breadcrumb.setMessage(scrub(breadcrumb.getMessage()));
                breadcrumb.setCategory(scrub(breadcrumb.getCategory()));
                scrubMap(breadcrumb.getData());
            }
        }

        final Request request = event.getRequest();
        if (request != null) {
            request.setUrl(scrub(request.getUrl()));
            request.setQueryString(scrub(request.getQueryString()));
            request.setFragment(scrub(request.getFragment()));
            // bodies, cookies, headers and env vars can contain credentials or payloads
            request.setData(null);
            request.setCookies(null);
            request.setHeaders(null);
            request.setEnvs(null);
            scrubStringMap(request.getOthers());
        }

        scrubStringMap(event.getTags());
        scrubMap(event.getExtras());

        final Contexts contexts = event.getContexts();
        if (contexts != null) {
            for (final Map.Entry<String, Object> entry : contexts.entrySet()) {
                if (entry.getValue() instanceof String) {
                    contexts.set(entry.getKey(), scrub((String) entry.getValue()));
                }
            }
        }
        return event;
    }

    private static void scrubStringMap(final Map<String, String> map) {
        if (map == null) {
            return;
        }
        for (final Map.Entry<String, String> entry : map.entrySet()) {
            entry.setValue(scrub(entry.getValue()));
        }
    }

    private static void scrubMap(final Map<String, Object> map) {
        if (map == null) {
            return;
        }
        for (final Map.Entry<String, Object> entry : map.entrySet()) {
            entry.setValue(scrubValue(entry.getValue()));
        }
    }

    private static Object scrubValue(final Object value) {
        if (value instanceof String) {
            return scrub((String) value);
        }
        if (value instanceof Map) {
            final Map<?, ?> source = (Map<?, ?>) value;
            final Map<String, Object> copy = new HashMap<>();
            for (final Map.Entry<?, ?> entry : source.entrySet()) {
                copy.put(String.valueOf(entry.getKey()), scrubValue(entry.getValue()));
            }
            return copy;
        }
        if (value instanceof Collection) {
            final List<Object> copy = new ArrayList<>();
            for (final Object item : (Collection<?>) value) {
                copy.add(scrubValue(item));
            }
            return copy;
        }
        return value;
    }

    private static String scrub(final String input) {
        if (input == null || input.isEmpty()) {
            return input;
        }
        String output = input;
        output = FILE_PATH_PATTERN.matcher(output).replaceAll("[file]");
        output = JID_PATTERN.matcher(output).replaceAll("[jid]");
        output = IPV4_PATTERN.matcher(output).replaceAll("[ip]");
        output = IPV6_PATTERN.matcher(output).replaceAll("[ip]");
        return output;
    }
}
