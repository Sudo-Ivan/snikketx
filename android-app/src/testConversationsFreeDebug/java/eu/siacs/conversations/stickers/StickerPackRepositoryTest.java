package eu.siacs.conversations.stickers;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import android.content.Context;
import com.google.gson.JsonParseException;
import java.io.File;
import java.io.FileWriter;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Random;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class StickerPackRepositoryTest {

    private final Context context = RuntimeEnvironment.getApplication();

    private File packDir(final String name) throws IOException {
        final var dir = new File(StickerPackRepository.localPacksDir(context), name);
        assertTrue(dir.mkdirs() || dir.isDirectory());
        return dir;
    }

    private static File touch(final File dir, final String name) throws IOException {
        final var file = new File(dir, name);
        try (final var writer = new FileWriter(file, StandardCharsets.UTF_8)) {
            writer.write("not-really-an-image");
        }
        return file;
    }

    private static void writeManifest(final File dir, final String json) throws IOException {
        try (final var writer =
                new FileWriter(new File(dir, "pack.json"), StandardCharsets.UTF_8)) {
            writer.write(json);
        }
    }

    @Test
    public void manifestItemsAreParsed() throws IOException {
        final var dir = packDir("pack-a");
        touch(dir, "a.png");
        touch(dir, "b.png");
        writeManifest(
                dir,
                "{\"name\":\"Cool Pack\",\"summary\":\"the summary\","
                        + "\"items\":[{\"file\":\"a.png\",\"desc\":\"\\uD83D\\uDE00\"},"
                        + "{\"file\":\"b.png\"}]}");
        final var pack = StickerPackRepository.parseLocalPack(dir);
        assertEquals("pack-a", pack.getId());
        assertEquals("Cool Pack", pack.getName());
        assertEquals("the summary", pack.getSummary());
        assertEquals(2, pack.getItems().size());
        assertEquals("a.png", pack.getItems().get(0).getFile().getName());
        assertEquals("\uD83D\uDE00", pack.getItems().get(0).getDescription());
        assertEquals("image/png", pack.getItems().get(0).getMimeType());
        assertEquals("b.png", pack.getItems().get(1).getFile().getName());
        assertNull(pack.getItems().get(1).getDescription());
    }

    @Test
    public void manifestItemsWithoutExistingFileAreSkipped() throws IOException {
        final var dir = packDir("pack-b");
        touch(dir, "exists.png");
        touch(dir, "note.txt");
        writeManifest(
                dir,
                "{\"items\":[{\"file\":\"missing.png\"},{\"file\":\"note.txt\"},"
                        + "{\"file\":\"exists.png\"},{\"desc\":\"no file field\"},null]}");
        final var pack = StickerPackRepository.parseLocalPack(dir);
        assertEquals(1, pack.getItems().size());
        assertEquals("exists.png", pack.getItems().get(0).getFile().getName());
    }

    @Test
    public void manifestWithoutItemsFallsBackToDirectoryListing() throws IOException {
        final var dir = packDir("pack-c");
        touch(dir, "z.png");
        touch(dir, "a.png");
        touch(dir, "notes.txt");
        writeManifest(dir, "{\"name\":\"Named\",\"summary\":null}");
        final var pack = StickerPackRepository.parseLocalPack(dir);
        assertEquals("Named", pack.getName());
        assertNull(pack.getSummary());
        assertEquals(2, pack.getItems().size());
        // files are sorted by name
        assertEquals("a.png", pack.getItems().get(0).getFile().getName());
        assertEquals("z.png", pack.getItems().get(1).getFile().getName());
    }

    @Test
    public void missingManifestFallsBackToDirectoryListing() throws IOException {
        final var dir = packDir("pack-d");
        touch(dir, "b.png");
        touch(dir, "a.png");
        touch(dir, "readme.txt");
        final var pack = StickerPackRepository.parseLocalPack(dir);
        // the directory name becomes the pack name
        assertEquals("pack-d", pack.getName());
        assertEquals(2, pack.getItems().size());
        assertEquals("a.png", pack.getItems().get(0).getFile().getName());
        assertEquals("b.png", pack.getItems().get(1).getFile().getName());
        assertNull(pack.getItems().get(0).getDescription());
    }

    @Test
    public void malformedManifestThrows() throws IOException {
        final var dir = packDir("pack-e");
        touch(dir, "a.png");
        writeManifest(dir, "{ this is not json");
        assertThrows(JsonParseException.class, () -> StickerPackRepository.parseLocalPack(dir));
    }

    @Test
    public void malformedManifestDoesNotBreakOtherPacks() throws Exception {
        final var bad = packDir("bad");
        writeManifest(bad, "{ not json");
        final var good = packDir("good");
        touch(good, "g.png");
        final var packs = StickerPackRepository.load(context, null).get(10, TimeUnit.SECONDS);
        assertTrue(packs.stream().anyMatch(p -> p.getId().equals("good")));
        assertTrue(packs.stream().noneMatch(p -> p.getId().equals("bad")));
    }

    @Test
    public void loadReturnsPacksViaPublicApi() throws Exception {
        final var dir = packDir("pack-f");
        touch(dir, "a.png");
        writeManifest(dir, "{\"name\":\"Public\",\"items\":[{\"file\":\"a.png\"}]}");
        final var packs = StickerPackRepository.load(context, null).get(10, TimeUnit.SECONDS);
        final var pack =
                packs.stream().filter(p -> p.getId().equals("pack-f")).findFirst().orElseThrow();
        assertEquals("Public", pack.getName());
        assertEquals(1, pack.getItems().size());
    }

    @Test
    public void emptyDirectoryProducesNoPack() throws Exception {
        packDir("empty");
        final var packs = StickerPackRepository.load(context, null).get(10, TimeUnit.SECONDS);
        assertTrue(packs.stream().noneMatch(p -> p.getId().equals("empty")));
    }

    @Test
    public void extensionIsTakenFromUrl() {
        assertEquals(
                ".png",
                StickerPackRepository.extensionFor("https://example.org/a/sticker.png", null));
        assertEquals(
                ".webp",
                StickerPackRepository.extensionFor(
                        "https://example.org/a/sticker.webp", "image/png"));
        // a relative url without a slash still has an extension
        assertEquals(".png", StickerPackRepository.extensionFor("sticker.png", null));
        // dots in path segments are not extensions
        assertEquals(
                ".img", StickerPackRepository.extensionFor("https://exa.mple/a/sticker", null));
        // hidden files are not extensions
        assertEquals(".img", StickerPackRepository.extensionFor("https://example.org/.png", null));
        // overly long extensions are rejected
        assertEquals(
                ".img",
                StickerPackRepository.extensionFor("https://example.org/a.toolongextension", null));
    }

    @Test
    public void extensionFallsBackToMimeType() {
        assertEquals(
                ".png",
                StickerPackRepository.extensionFor("https://example.org/sticker", "image/png"));
        assertEquals(
                ".img",
                StickerPackRepository.extensionFor(
                        "https://example.org/sticker", "application/x-unknown"));
        assertEquals(
                ".img", StickerPackRepository.extensionFor("https://example.org/sticker", null));
    }

    @Test
    public void sanitizeReplacesUnsafeCharacters() {
        assertEquals("abc_123", StickerPackRepository.sanitize("abc:123"));
        assertEquals("a_b_c", StickerPackRepository.sanitize("a/b c"));
        assertEquals("pack-1_ok", StickerPackRepository.sanitize("pack-1_ok"));
    }

    @Test
    public void extensionForFuzzDoesNotThrow() {
        final var random = new Random(42);
        final char[] alphabet = "ab./:\uD83D\uDE00 ?#".toCharArray();
        final String[] mimes = {null, "image/png", "image/webp", "text/plain", ""};
        for (int i = 0; i < 2000; ++i) {
            final int length = random.nextInt(40);
            final var builder = new StringBuilder(length);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            StickerPackRepository.extensionFor(
                    builder.toString(), mimes[random.nextInt(mimes.length)]);
            StickerPackRepository.sanitize(builder.toString());
        }
    }

    @Test
    public void malformedManifestFuzzDoesNotCrashLoad() throws Exception {
        final var random = new Random(42);
        final char[] alphabet = "{}[]\":,itemsfledsc012 .".toCharArray();
        for (int i = 0; i < 50; ++i) {
            final var dir = packDir("fuzz-" + i);
            touch(dir, "a.png");
            final var builder = new StringBuilder();
            final int length = random.nextInt(60);
            for (int j = 0; j < length; ++j) {
                builder.append(alphabet[random.nextInt(alphabet.length)]);
            }
            writeManifest(dir, builder.toString());
        }
        // loading must not throw no matter what the manifests contained
        StickerPackRepository.load(context, null).get(10, TimeUnit.SECONDS);
    }
}
