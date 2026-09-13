package eu.siacs.conversations.http;

import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.os.Handler;
import android.os.Looper;
import android.util.LruCache;
import androidx.annotation.Nullable;
import com.google.common.base.Strings;
import com.google.gson.JsonParser;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.services.XmppConnectionService;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;
import okhttp3.Credentials;
import okhttp3.HttpUrl;
import okhttp3.Request;
import okhttp3.Response;

public class LinkPreviewer {

    private static final int PREVIEW_CACHE_SIZE = 200;
    private static final int IMAGE_CACHE_BYTES = 8 * 1024 * 1024;
    private static final int MAX_METADATA_BYTES = 64 * 1024;
    private static final int MAX_IMAGE_BYTES = 8 * 1024 * 1024;
    private static final int MAX_IMAGE_DIMENSION = 2048;
    private static final long FAILURE_RETRY_DELAY_MS = 60_000L;
    private static final long AUTH_FAILURE_RETRY_DELAY_MS = 15 * 60_000L;
    private static final long MAX_RETRY_AFTER_MS = 10 * 60_000L;

    // All caches are keyed by account UUID plus the target URL so data can
    // never leak between accounts.
    private static final LruCache<String, Preview> PREVIEW_CACHE =
            new LruCache<>(PREVIEW_CACHE_SIZE);
    private static final LruCache<String, Bitmap> IMAGE_CACHE =
            new LruCache<>(IMAGE_CACHE_BYTES) {
                @Override
                protected int sizeOf(final String key, final Bitmap bitmap) {
                    return bitmap.getByteCount();
                }
            };
    // Value is the earliest epoch millis a retry is allowed.
    private static final LruCache<String, Long> FAILED = new LruCache<>(PREVIEW_CACHE_SIZE);
    private static final Map<String, List<Consumer<Preview>>> IN_FLIGHT = new HashMap<>();
    private static final Map<String, List<Consumer<Bitmap>>> IN_FLIGHT_IMAGES = new HashMap<>();
    private static final Handler MAIN_THREAD = new Handler(Looper.getMainLooper());

    private static String keyOf(final Account account, final String url) {
        return account.getUuid() + "\n" + url;
    }

    public static void fetch(
            final XmppConnectionService service,
            final Account account,
            final String url,
            final Consumer<Preview> callback) {
        final String key = keyOf(account, url);
        final Preview cached;
        synchronized (PREVIEW_CACHE) {
            cached = PREVIEW_CACHE.get(key);
        }
        if (cached != null) {
            callback.accept(cached);
            return;
        }
        if (suppressed(key)) {
            callback.accept(null);
            return;
        }
        synchronized (IN_FLIGHT) {
            final var pending = IN_FLIGHT.get(key);
            if (pending != null) {
                pending.add(callback);
                return;
            }
            final var callbacks = new ArrayList<Consumer<Preview>>();
            callbacks.add(callback);
            IN_FLIGHT.put(key, callbacks);
        }
        HttpConnectionManager.EXECUTOR.execute(
                () -> {
                    List<Consumer<Preview>> callbacks = null;
                    Preview preview = null;
                    try {
                        final var result = fetchPreview(service, account, url);
                        preview = result.preview;
                        if (preview == null) {
                            markFailed(key, result.retryAfterMs);
                        } else {
                            synchronized (PREVIEW_CACHE) {
                                PREVIEW_CACHE.put(key, preview);
                            }
                        }
                    } finally {
                        synchronized (IN_FLIGHT) {
                            callbacks = IN_FLIGHT.remove(key);
                        }
                    }
                    final Preview outcome = preview;
                    final var pending = callbacks;
                    MAIN_THREAD.post(
                            () -> {
                                if (pending == null) {
                                    return;
                                }
                                for (final var consumer : pending) {
                                    consumer.accept(outcome);
                                }
                            });
                });
    }

    public static void loadImage(
            final XmppConnectionService service,
            final Account account,
            final String remoteUrl,
            final Consumer<Bitmap> callback) {
        final String key = keyOf(account, remoteUrl);
        synchronized (IMAGE_CACHE) {
            final Bitmap cached = IMAGE_CACHE.get(key);
            if (cached != null) {
                callback.accept(cached);
                return;
            }
        }
        if (suppressed(key)) {
            callback.accept(null);
            return;
        }
        synchronized (IN_FLIGHT_IMAGES) {
            final var pending = IN_FLIGHT_IMAGES.get(key);
            if (pending != null) {
                pending.add(callback);
                return;
            }
            final var callbacks = new ArrayList<Consumer<Bitmap>>();
            callbacks.add(callback);
            IN_FLIGHT_IMAGES.put(key, callbacks);
        }
        HttpConnectionManager.EXECUTOR.execute(
                () -> {
                    List<Consumer<Bitmap>> callbacks = null;
                    Bitmap bitmap = null;
                    try {
                        final var result = fetchImage(service, account, remoteUrl);
                        bitmap = result.bitmap;
                        if (bitmap == null) {
                            markFailed(key, result.retryAfterMs);
                        } else {
                            final Bitmap finalBitmap = bitmap;
                            synchronized (IMAGE_CACHE) {
                                IMAGE_CACHE.put(key, finalBitmap);
                            }
                        }
                    } finally {
                        synchronized (IN_FLIGHT_IMAGES) {
                            callbacks = IN_FLIGHT_IMAGES.remove(key);
                        }
                    }
                    final Bitmap outcome = bitmap;
                    final var pending = callbacks;
                    MAIN_THREAD.post(
                            () -> {
                                if (pending == null) {
                                    return;
                                }
                                for (final var consumer : pending) {
                                    consumer.accept(outcome);
                                }
                            });
                });
    }

    private static boolean suppressed(final String key) {
        synchronized (FAILED) {
            final Long retryAt = FAILED.get(key);
            return retryAt != null && System.currentTimeMillis() < retryAt;
        }
    }

    private static void markFailed(final String key, final long retryAfterMs) {
        final long delay =
                retryAfterMs > 0
                        ? Math.min(retryAfterMs, MAX_RETRY_AFTER_MS)
                        : FAILURE_RETRY_DELAY_MS;
        synchronized (FAILED) {
            FAILED.put(key, System.currentTimeMillis() + delay);
        }
    }

    // Wraps a fetch result with the Retry-After delay the server asked
    // for, so rate limiting and auth outages back off instead of hammering.
    private static final class FetchResult {
        final Preview preview;
        final Bitmap bitmap;
        final long retryAfterMs;

        FetchResult(final Preview preview, final long retryAfterMs) {
            this.preview = preview;
            this.bitmap = null;
            this.retryAfterMs = retryAfterMs;
        }

        FetchResult(final Bitmap bitmap, final long retryAfterMs) {
            this.preview = null;
            this.bitmap = bitmap;
            this.retryAfterMs = retryAfterMs;
        }
    }

    private static FetchResult fetchPreview(
            final XmppConnectionService service, final Account account, final String url) {
        final HttpUrl endpoint = proxyEndpoint(account, "api/link-preview", url);
        if (endpoint == null) {
            return new FetchResult((Preview) null, 0);
        }
        try {
            final var client =
                    service.getHttpConnectionManager().buildHttpClient(endpoint, account, false);
            final var request =
                    new Request.Builder()
                            .get()
                            .url(endpoint)
                            .header("Authorization", authorization(account))
                            .build();
            try (final var response = client.newCall(request).execute()) {
                final var body = response.body();
                if (!response.isSuccessful() || body == null) {
                    return new FetchResult((Preview) null, failureDelay(response));
                }
                if (body.contentLength() > MAX_METADATA_BYTES) {
                    return new FetchResult((Preview) null, 0);
                }
                final String payload = readBounded(body.byteStream(), MAX_METADATA_BYTES);
                if (payload == null || payload.isEmpty()) {
                    return new FetchResult((Preview) null, 0);
                }
                final var json = JsonParser.parseString(payload).getAsJsonObject();
                return new FetchResult(
                        new Preview(
                                getAsString(json, "url", url),
                                getAsString(json, "title", null),
                                getAsString(json, "description", null),
                                getAsString(json, "site_name", null),
                                getAsString(json, "image", null),
                                getAsString(json, "favicon", null)),
                        0);
            }
        } catch (final Exception e) {
            return new FetchResult((Preview) null, 0);
        }
    }

    private static FetchResult fetchImage(
            final XmppConnectionService service, final Account account, final String remoteUrl) {
        final HttpUrl endpoint = proxyEndpoint(account, "api/link-preview/image", remoteUrl);
        if (endpoint == null) {
            return new FetchResult((Bitmap) null, 0);
        }
        try {
            final var client =
                    service.getHttpConnectionManager().buildHttpClient(endpoint, account, false);
            final var request =
                    new Request.Builder()
                            .get()
                            .url(endpoint)
                            .header("Authorization", authorization(account))
                            .build();
            try (final var response = client.newCall(request).execute()) {
                final var body = response.body();
                if (!response.isSuccessful() || body == null) {
                    return new FetchResult((Bitmap) null, failureDelay(response));
                }
                if (body.contentLength() > MAX_IMAGE_BYTES) {
                    return new FetchResult((Bitmap) null, 0);
                }
                try (final var stream = body.byteStream()) {
                    final byte[] bytes = readBytes(stream, MAX_IMAGE_BYTES);
                    if (bytes == null || bytes.length == 0) {
                        return new FetchResult((Bitmap) null, 0);
                    }
                    final Bitmap bitmap = decodeBounded(bytes);
                    return new FetchResult(bitmap, 0);
                }
            }
        } catch (final IOException | RuntimeException e) {
            return new FetchResult((Bitmap) null, 0);
        }
    }

    // Authentication failures back off hard so a locked or expired
    // credential is not retried on every list scroll. Retry-After is
    // honored for 429 and 5xx answers.
    private static long failureDelay(final Response response) {
        final int code = response.code();
        if (code == 401 || code == 403) {
            return AUTH_FAILURE_RETRY_DELAY_MS;
        }
        final String retryAfter = response.header("Retry-After");
        if (code == 429 || code >= 500) {
            if (retryAfter != null) {
                try {
                    final long seconds = Long.parseLong(retryAfter.trim());
                    if (seconds > 0) {
                        return seconds * 1000L;
                    }
                } catch (final NumberFormatException ignored) {
                    // HTTP-date Retry-After is not needed for this API
                }
            }
        }
        return 0;
    }

    @Nullable
    private static String readBounded(final java.io.InputStream stream, final int maxBytes)
            throws IOException {
        final byte[] bytes = readBytes(stream, maxBytes);
        return bytes == null ? null : new String(bytes, StandardCharsets.UTF_8);
    }

    @Nullable
    private static byte[] readBytes(final java.io.InputStream stream, final int maxBytes)
            throws IOException {
        final var out = new java.io.ByteArrayOutputStream(Math.min(maxBytes, 8192));
        final byte[] buffer = new byte[8192];
        int total = 0;
        int read;
        while ((read = stream.read(buffer)) != -1) {
            total += read;
            if (total > maxBytes) {
                return null;
            }
            out.write(buffer, 0, read);
        }
        return out.toByteArray();
    }

    // Downsamples anything beyond MAX_IMAGE_DIMENSION so a hostile or
    // absurdly large bitmap cannot blow up the decode or the row layout.
    @Nullable
    private static Bitmap decodeBounded(final byte[] bytes) {
        final var bounds = new BitmapFactory.Options();
        bounds.inJustDecodeBounds = true;
        BitmapFactory.decodeByteArray(bytes, 0, bytes.length, bounds);
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) {
            return null;
        }
        int sample = 1;
        while (bounds.outWidth / (sample * 2) >= MAX_IMAGE_DIMENSION
                || bounds.outHeight / (sample * 2) >= MAX_IMAGE_DIMENSION) {
            sample *= 2;
        }
        final var options = new BitmapFactory.Options();
        options.inSampleSize = sample;
        return BitmapFactory.decodeByteArray(bytes, 0, bytes.length, options);
    }

    @Nullable
    private static HttpUrl proxyEndpoint(
            final Account account, final String path, final String targetUrl) {
        final var jid = account.getJid();
        final var password = account.getPassword();
        if (jid == null || Strings.isNullOrEmpty(password)) {
            return null;
        }
        if (HttpUrl.parse(targetUrl) == null) {
            return null;
        }
        try {
            return new HttpUrl.Builder()
                    .scheme("https")
                    .host(jid.getDomain().toString())
                    .addPathSegments(path)
                    .addQueryParameter("url", targetUrl)
                    .build();
        } catch (final IllegalArgumentException e) {
            return null;
        }
    }

    private static String authorization(final Account account) {
        return Credentials.basic(
                account.getJid().asBareJid().toString(),
                account.getPassword(),
                StandardCharsets.UTF_8);
    }

    private static String getAsString(
            final com.google.gson.JsonObject json, final String name, final String fallback) {
        final var element = json.get(name);
        if (element == null || !element.isJsonPrimitive()) {
            return fallback;
        }
        final var value = element.getAsString();
        return Strings.isNullOrEmpty(value) ? fallback : value;
    }

    public static class Preview {
        public final String url;
        public final String title;
        public final String description;
        public final String siteName;
        public final String image;
        public final String favicon;

        private Preview(
                final String url,
                final String title,
                final String description,
                final String siteName,
                final String image,
                final String favicon) {
            this.url = url;
            this.title = title;
            this.description = description;
            this.siteName = siteName;
            this.image = image;
            this.favicon = favicon;
        }

        public boolean isEmpty() {
            return Strings.isNullOrEmpty(title)
                    && Strings.isNullOrEmpty(description)
                    && Strings.isNullOrEmpty(image)
                    && Strings.isNullOrEmpty(favicon);
        }
    }
}
