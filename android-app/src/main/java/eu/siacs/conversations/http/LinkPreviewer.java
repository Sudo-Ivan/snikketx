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
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;
import okhttp3.Credentials;
import okhttp3.HttpUrl;
import okhttp3.Request;

public class LinkPreviewer {

    private static final int PREVIEW_CACHE_SIZE = 200;
    private static final int IMAGE_CACHE_BYTES = 8 * 1024 * 1024;
    private static final long FAILURE_RETRY_DELAY_MS = 60_000L;

    private static final LruCache<String, Preview> PREVIEW_CACHE =
            new LruCache<>(PREVIEW_CACHE_SIZE);
    private static final LruCache<String, Bitmap> IMAGE_CACHE =
            new LruCache<>(IMAGE_CACHE_BYTES) {
                @Override
                protected int sizeOf(final String key, final Bitmap bitmap) {
                    return bitmap.getByteCount();
                }
            };
    private static final LruCache<String, Long> FAILED = new LruCache<>(PREVIEW_CACHE_SIZE);
    private static final Map<String, List<Consumer<Preview>>> IN_FLIGHT = new HashMap<>();
    private static final Handler MAIN_THREAD = new Handler(Looper.getMainLooper());

    public static void fetch(
            final XmppConnectionService service,
            final Account account,
            final String url,
            final Consumer<Preview> callback) {
        final Preview cached;
        synchronized (PREVIEW_CACHE) {
            cached = PREVIEW_CACHE.get(url);
        }
        if (cached != null) {
            callback.accept(cached);
            return;
        }
        synchronized (FAILED) {
            final Long failedAt = FAILED.get(url);
            if (failedAt != null
                    && System.currentTimeMillis() - failedAt < FAILURE_RETRY_DELAY_MS) {
                callback.accept(null);
                return;
            }
        }
        synchronized (IN_FLIGHT) {
            final var pending = IN_FLIGHT.get(url);
            if (pending != null) {
                pending.add(callback);
                return;
            }
            final var callbacks = new ArrayList<Consumer<Preview>>();
            callbacks.add(callback);
            IN_FLIGHT.put(url, callbacks);
        }
        HttpConnectionManager.EXECUTOR.execute(
                () -> {
                    final Preview preview = fetchPreview(service, account, url);
                    if (preview == null) {
                        synchronized (FAILED) {
                            FAILED.put(url, System.currentTimeMillis());
                        }
                    } else {
                        synchronized (PREVIEW_CACHE) {
                            PREVIEW_CACHE.put(url, preview);
                        }
                    }
                    final List<Consumer<Preview>> callbacks;
                    synchronized (IN_FLIGHT) {
                        callbacks = IN_FLIGHT.remove(url);
                    }
                    MAIN_THREAD.post(
                            () -> {
                                if (callbacks == null) {
                                    return;
                                }
                                for (final var consumer : callbacks) {
                                    consumer.accept(preview);
                                }
                            });
                });
    }

    public static void loadImage(
            final XmppConnectionService service,
            final Account account,
            final String remoteUrl,
            final Consumer<Bitmap> callback) {
        synchronized (IMAGE_CACHE) {
            final Bitmap cached = IMAGE_CACHE.get(remoteUrl);
            if (cached != null) {
                callback.accept(cached);
                return;
            }
        }
        HttpConnectionManager.EXECUTOR.execute(
                () -> {
                    final Bitmap bitmap = fetchImage(service, account, remoteUrl);
                    if (bitmap != null) {
                        synchronized (IMAGE_CACHE) {
                            IMAGE_CACHE.put(remoteUrl, bitmap);
                        }
                    }
                    MAIN_THREAD.post(() -> callback.accept(bitmap));
                });
    }

    @Nullable
    private static Preview fetchPreview(
            final XmppConnectionService service, final Account account, final String url) {
        final HttpUrl endpoint = proxyEndpoint(account, "api/link-preview", url);
        if (endpoint == null) {
            return null;
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
                    return null;
                }
                final var json = JsonParser.parseString(body.string()).getAsJsonObject();
                return new Preview(
                        getAsString(json, "url", url),
                        getAsString(json, "title", null),
                        getAsString(json, "description", null),
                        getAsString(json, "site_name", null),
                        getAsString(json, "image", null),
                        getAsString(json, "favicon", null));
            }
        } catch (final Exception e) {
            return null;
        }
    }

    @Nullable
    private static Bitmap fetchImage(
            final XmppConnectionService service, final Account account, final String remoteUrl) {
        final HttpUrl endpoint = proxyEndpoint(account, "api/link-preview/image", remoteUrl);
        if (endpoint == null) {
            return null;
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
                    return null;
                }
                try (final var stream = body.byteStream()) {
                    return BitmapFactory.decodeStream(stream);
                }
            }
        } catch (final IOException | RuntimeException e) {
            return null;
        }
    }

    @Nullable
    private static HttpUrl proxyEndpoint(
            final Account account, final String path, final String targetUrl) {
        final var jid = account.getJid();
        final var password = account.getPassword();
        if (jid == null || Strings.isNullOrEmpty(password)) {
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
        return Credentials.basic(account.getJid().asBareJid().toString(), account.getPassword());
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
                    && Strings.isNullOrEmpty(image);
        }
    }
}
