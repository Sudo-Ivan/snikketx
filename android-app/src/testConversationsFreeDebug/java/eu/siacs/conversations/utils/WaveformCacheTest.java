package eu.siacs.conversations.utils;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class WaveformCacheTest {

    @Test
    public void missingFileYieldsNull() {
        assertNull(WaveformCache.get("/definitely/not/a/real/file.m4a"));
        assertNull(WaveformCache.peek("/definitely/not/a/real/file.m4a"));
    }

    @Test
    public void nullPathYieldsNull() {
        assertNull(WaveformCache.get(null));
        assertNull(WaveformCache.peek(null));
    }

    @Test
    public void emptyAndSilentInputYieldsNull() {
        assertNull(WaveformCache.bucket(List.of(), 4));
        assertNull(WaveformCache.bucket(List.of(0f, 0f, 0f), 4));
        assertNull(WaveformCache.bucket(List.of(1f), 0));
    }

    @Test
    public void fewerBlocksThanBarsAreSpread() {
        final var bars = WaveformCache.bucket(List.of(0.5f), 4);
        assertArrayEquals(new float[] {1f, 1f, 1f, 1f}, bars, 0.0001f);
    }

    @Test
    public void peakIsNormalizedToOne() {
        final var bars = WaveformCache.bucket(List.of(0.25f, 1f), 2);
        // square root curve: 0.25 / 1.0 -> sqrt(0.25) == 0.5
        assertArrayEquals(new float[] {0.5f, 1f}, bars, 0.0001f);
    }

    @Test
    public void sqrtCurveKeepsQuietPartsVisible() {
        // linear scaling would give 0.01; sqrt gives 0.1
        final var bars = WaveformCache.bucket(List.of(0.01f, 1f), 2);
        assertEquals(0.1f, bars[0], 0.0001f);
        assertEquals(1f, bars[1], 0.0001f);
    }

    @Test
    public void blocksAreBinnedByPeak() {
        // bar 0 covers blocks 0,1; bar 1 covers blocks 2,3; each bar takes the peak
        final var bars = WaveformCache.bucket(List.of(0.1f, 0.9f, 0.2f, 0.8f), 2);
        assertEquals(1f, bars[0], 0.0001f);
        assertEquals((float) Math.sqrt(0.8f / 0.9f), bars[1], 0.0001f);
    }

    @Test
    public void manyBlocksAreCompressedToBarCount() {
        final var blocks = new ArrayList<Float>();
        for (int i = 0; i < 500; ++i) {
            blocks.add((i % 10) / 10f);
        }
        final var bars = WaveformCache.bucket(blocks, WaveformCache.BAR_COUNT);
        assertEquals(WaveformCache.BAR_COUNT, bars.length);
        for (final float bar : bars) {
            assertTrue(bar >= 0f && bar <= 1f);
        }
    }

    @Test
    public void bucketFuzzDoesNotThrow() {
        final var random = new Random(42);
        for (int i = 0; i < 1000; ++i) {
            final int count = random.nextInt(200);
            final var blocks = new ArrayList<Float>(count);
            for (int j = 0; j < count; ++j) {
                blocks.add(random.nextFloat());
            }
            final var bars = WaveformCache.bucket(blocks, 1 + random.nextInt(64));
            if (bars != null) {
                for (final float bar : bars) {
                    assertTrue(bar >= 0f && bar <= 1.0001f);
                }
            }
        }
    }
}
