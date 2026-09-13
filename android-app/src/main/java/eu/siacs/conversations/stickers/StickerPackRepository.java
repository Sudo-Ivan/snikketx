package eu.siacs.conversations.stickers;

import android.content.Context;
import android.util.Log;
import androidx.annotation.Nullable;
import com.google.common.base.Strings;
import com.google.common.collect.ImmutableList;
import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.ListeningExecutorService;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.utils.MimeUtils;
import eu.siacs.conversations.xml.Element;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.XmppConnection;
import eu.siacs.conversations.xmpp.manager.PubSubManager;
import im.conversations.android.xmpp.model.stickers.Pack;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executors;
import okhttp3.OkHttpClient;
import okhttp3.Request;

/**
 * Loads sticker packs from local storage and, if connected, from the user's personal pubsub node
 * 'urn:xmpp:stickers:0' as described in XEP-0449.
 *
 * <p>Local pack layout for development:
 *
 * <pre>
 * &lt;filesDir&gt;/stickers/&lt;packId&gt;/pack.json
 * &lt;filesDir&gt;/stickers/&lt;packId&gt;/&lt;image files&gt;
 * </pre>
 *
 * pack.json is optional and looks like:
 *
 * <pre>
 * {"name":"My Pack","summary":"optional","items":[{"file":"foo.png","desc":"😄"}]}
 * </pre>
 *
 * Without pack.json every image file in the directory becomes a sticker.
 */
public final class StickerPackRepository {

    private static final String LOCAL_PACKS_DIR = "stickers";
    private static final String REMOTE_CACHE_DIR = "sticker_cache";
    private static final String PACK_MANIFEST = "pack.json";
    private static final long MAX_STICKER_BYTES = 1024L * 1024L * 8L;

    private static final ListeningExecutorService EXECUTOR =
            MoreExecutors.listeningDecorator(Executors.newSingleThreadExecutor());
    private static final OkHttpClient HTTP_CLIENT = new OkHttpClient();
    private static final Gson GSON = new Gson();

    private StickerPackRepository() {}

    public static ListenableFuture<List<StickerPack>> load(
            final Context context, @Nullable final Account account) {
        final ListenableFuture<List<StickerPack>> local =
                EXECUTOR.submit(() -> loadLocalPacks(context));
        final ListenableFuture<List<StickerPack>> remote =
                Futures.catching(
                        loadRemotePacks(context, account),
                        Exception.class,
                        e -> {
                            Log.d(Config.LOGTAG, "could not fetch remote sticker packs", e);
                            return List.of();
                        },
                        MoreExecutors.directExecutor());
        return Futures.whenAllSucceed(local, remote)
                .call(
                        () ->
                                ImmutableList.<StickerPack>builder()
                                        .addAll(Futures.getDone(local))
                                        .addAll(Futures.getDone(remote))
                                        .build(),
                        MoreExecutors.directExecutor());
    }

    public static File localPacksDir(final Context context) {
        return new File(context.getFilesDir(), LOCAL_PACKS_DIR);
    }

    private static List<StickerPack> loadLocalPacks(final Context context) {
        final File root = localPacksDir(context);
        final File[] dirs = root.listFiles(File::isDirectory);
        if (dirs == null) {
            return List.of();
        }
        Arrays.sort(dirs, Comparator.comparing(File::getName));
        final ImmutableList.Builder<StickerPack> packs = ImmutableList.builder();
        for (final File dir : dirs) {
            try {
                final StickerPack pack = parseLocalPack(dir);
                if (pack != null && !pack.getItems().isEmpty()) {
                    packs.add(pack);
                }
            } catch (final IOException | RuntimeException e) {
                // a malformed pack.json throws a JsonSyntaxException (RuntimeException); it must
                // not take down the other local packs
                Log.w(Config.LOGTAG, "skipping sticker pack " + dir.getName(), e);
            }
        }
        return packs.build();
    }

    static StickerPack parseLocalPack(final File dir) throws IOException {
        final PackManifest manifest = readManifest(new File(dir, PACK_MANIFEST));
        final ImmutableList.Builder<StickerPack.Item> items = ImmutableList.builder();
        if (manifest != null && manifest.items != null) {
            for (final ItemManifest item : manifest.items) {
                if (item == null || Strings.isNullOrEmpty(item.file)) {
                    continue;
                }
                // keep manifest filenames inside the pack directory; '..' segments or
                // absolute paths would let a pack reference arbitrary readable files
                if (item.file.contains("..") || new File(item.file).isAbsolute()) {
                    Log.w(Config.LOGTAG, "skipping suspicious sticker filename " + item.file);
                    continue;
                }
                final File file = new File(dir, item.file);
                if (isImageFile(file)) {
                    items.add(
                            new StickerPack.Item(
                                    file, item.desc, MimeUtils.getMimeType(file), null));
                }
            }
        } else {
            final File[] files = dir.listFiles(StickerPackRepository::isImageFile);
            if (files != null) {
                Arrays.sort(files, Comparator.comparing(File::getName));
                for (final File file : files) {
                    items.add(new StickerPack.Item(file, null, MimeUtils.getMimeType(file), null));
                }
            }
        }
        final String name =
                manifest != null && !Strings.isNullOrEmpty(manifest.name)
                        ? manifest.name
                        : dir.getName();
        return new StickerPack(
                dir.getName(), name, manifest == null ? null : manifest.summary, items.build());
    }

    private static PackManifest readManifest(final File file) throws IOException {
        if (!file.isFile()) {
            return null;
        }
        try (final var reader =
                new InputStreamReader(new FileInputStream(file), StandardCharsets.UTF_8)) {
            return GSON.fromJson(reader, PackManifest.class);
        }
    }

    private static boolean isImageFile(final File file) {
        if (file == null || !file.isFile()) {
            return false;
        }
        final String mime = MimeUtils.getMimeType(file);
        return mime != null && mime.startsWith("image/");
    }

    private static ListenableFuture<List<StickerPack>> loadRemotePacks(
            final Context context, @Nullable final Account account) {
        final XmppConnection connection = account == null ? null : account.getXmppConnection();
        if (connection == null) {
            return Futures.immediateFuture(List.of());
        }
        final ListenableFuture<Map<String, Pack>> future =
                connection
                        .getManager(PubSubManager.class)
                        .fetchItems(account.getJid().asBareJid(), Namespace.STICKERS, Pack.class);
        return Futures.transform(future, items -> toRemotePacks(context, items), EXECUTOR);
    }

    private static List<StickerPack> toRemotePacks(
            final Context context, final Map<String, Pack> items) {
        if (items == null) {
            return List.of();
        }
        final ImmutableList.Builder<StickerPack> packs = ImmutableList.builder();
        for (final Map.Entry<String, Pack> entry : items.entrySet()) {
            final String packId = entry.getKey();
            final Pack pack = entry.getValue();
            final ImmutableList.Builder<StickerPack.Item> stickers = ImmutableList.builder();
            int index = 0;
            for (final Element item : pack.getItems()) {
                ++index;
                final Element file = Pack.fileMetadata(item);
                final String desc = file == null ? null : file.findChildContent("desc");
                final String mime = file == null ? null : file.findChildContent("media-type");
                for (final String url : Pack.sourceUrls(item)) {
                    final File localFile = download(context, packId, index, url, mime);
                    if (localFile != null) {
                        stickers.add(new StickerPack.Item(localFile, desc, mime, url));
                        break;
                    }
                }
            }
            final List<StickerPack.Item> stickerList = stickers.build();
            if (!stickerList.isEmpty()) {
                packs.add(
                        new StickerPack(
                                packId, pack.getPackName(), pack.getPackSummary(), stickerList));
            }
        }
        return packs.build();
    }

    private static File download(
            final Context context,
            final String packId,
            final int index,
            final String url,
            @Nullable final String mimeType) {
        if (!url.startsWith("https://")) {
            // sticker images are downloaded unattended; plain http would allow a network
            // attacker to serve arbitrary image payloads
            Log.d(Config.LOGTAG, "refusing non https sticker source " + url);
            return null;
        }
        final String extension = extensionFor(url, mimeType);
        final String fileName = "sticker-" + index + extension;
        final File dir =
                new File(new File(context.getCacheDir(), REMOTE_CACHE_DIR), sanitize(packId));
        final File destination = new File(dir, fileName);
        if (destination.isFile() && destination.length() > 0) {
            return destination;
        }
        try {
            final var request = new Request.Builder().url(url).build();
            try (final var response = HTTP_CLIENT.newCall(request).execute()) {
                if (!response.isSuccessful() || response.body() == null) {
                    return null;
                }
                final long length = response.body().contentLength();
                if (length > MAX_STICKER_BYTES) {
                    return null;
                }
                if (!dir.mkdirs() && !dir.isDirectory()) {
                    return null;
                }
                final var tmp = File.createTempFile("sticker", ".tmp", dir);
                try {
                    try (final var out = new FileOutputStream(tmp)) {
                        // contentLength may be unknown (-1). cap the stream so an
                        // oversized body can not fill the cache partition
                        ByteStreams.copy(
                                ByteStreams.limit(
                                        response.body().byteStream(), MAX_STICKER_BYTES + 1),
                                out);
                    }
                    if (tmp.length() > MAX_STICKER_BYTES) {
                        return null;
                    }
                    if (tmp.renameTo(destination)) {
                        return destination;
                    }
                    return destination.isFile() ? destination : null;
                } finally {
                    tmp.delete();
                }
            }
        } catch (final IOException | IllegalArgumentException e) {
            Log.d(Config.LOGTAG, "could not download sticker " + url, e);
            return null;
        }
    }

    static String extensionFor(final String url, @Nullable final String mimeType) {
        final int slash = url.lastIndexOf('/');
        final int dot = url.lastIndexOf('.');
        if (dot > slash + 1 && dot < url.length() - 1 && url.length() - dot <= 5) {
            return url.substring(dot);
        }
        final String extension =
                mimeType == null ? null : MimeUtils.guessExtensionFromMimeType(mimeType);
        return extension == null ? ".img" : "." + extension;
    }

    static String sanitize(final String id) {
        return id.replaceAll("[^a-zA-Z0-9_-]", "_");
    }

    private static class PackManifest {
        @SerializedName("name")
        String name;

        @SerializedName("summary")
        String summary;

        @SerializedName("items")
        List<ItemManifest> items;
    }

    private static class ItemManifest {
        @SerializedName("file")
        String file;

        @SerializedName("desc")
        String desc;
    }
}
