package eu.siacs.conversations.stickers;

import androidx.annotation.Nullable;
import java.io.File;
import java.util.List;

/**
 * A sticker pack as described by XEP-0449. Local packs are stored below {@code
 * files/stickers/<packId>/} and remote packs are discovered on the pubsub node
 * 'urn:xmpp:stickers:0'.
 */
public class StickerPack {

    private final String id;
    private final String name;
    private final String summary;
    private final List<Item> items;

    public StickerPack(
            final String id, final String name, final String summary, final List<Item> items) {
        this.id = id;
        this.name = name;
        this.summary = summary;
        this.items = items;
    }

    public String getId() {
        return id;
    }

    public String getName() {
        return name;
    }

    public String getSummary() {
        return summary;
    }

    public List<Item> getItems() {
        return items;
    }

    public static class Item {
        private final File file;
        private final String description;
        private final String mimeType;
        private final String sourceUrl;

        public Item(
                @Nullable final File file,
                @Nullable final String description,
                @Nullable final String mimeType,
                @Nullable final String sourceUrl) {
            this.file = file;
            this.description = description;
            this.mimeType = mimeType;
            this.sourceUrl = sourceUrl;
        }

        /** Local file holding the sticker image. May be null if not downloaded yet. */
        @Nullable
        public File getFile() {
            return file;
        }

        /** Textual fallback (usually an emoji) taken from the file metadata desc element. */
        @Nullable
        public String getDescription() {
            return description;
        }

        @Nullable
        public String getMimeType() {
            return mimeType;
        }

        /** Remote source url of the sticker image, if the pack was fetched via pubsub. */
        @Nullable
        public String getSourceUrl() {
            return sourceUrl;
        }
    }
}
