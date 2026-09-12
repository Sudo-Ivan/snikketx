package eu.siacs.conversations.utils;

import android.media.AudioFormat;
import android.media.MediaCodec;
import android.media.MediaExtractor;
import android.media.MediaFormat;
import android.util.Log;
import android.util.LruCache;
import eu.siacs.conversations.Config;
import java.io.File;
import java.io.IOException;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.ArrayList;
import java.util.List;

/**
 * Extracts a normalized amplitude envelope from an audio file and caches the result in memory.
 * Decoding happens via MediaExtractor + MediaCodec and must be called off the main thread. Voice
 * notes produced by the app are AAC inside m4a (or Opus inside oga), both of which decode to 16 bit
 * PCM through MediaCodec on virtually all devices.
 */
public final class WaveformCache {

    public static final int BAR_COUNT = 56;

    // samples per measurement block. at 24kHz (voice note sample rate) this is ~42ms per block
    private static final int BLOCK_SIZE = 1024;

    // decode at most ~3 minutes worth of blocks to keep memory and runtime bounded
    private static final int MAX_BLOCKS = 20_000;

    private static final int MAX_CACHED_WAVEFORMS = 128;

    private static final String KEY_PCM_FORMAT = "pcm-format";

    private static final LruCache<String, float[]> CACHE = new LruCache<>(MAX_CACHED_WAVEFORMS);

    private WaveformCache() {}

    /** Returns the cached waveform for the given path or null if it has not been decoded yet. */
    public static synchronized float[] peek(final String path) {
        return CACHE.get(path);
    }

    /**
     * Returns the waveform for the given path, decoding the file if necessary. Returns null when
     * the file cannot be decoded. Must not be called on the main thread.
     */
    public static synchronized float[] get(final String path) {
        final float[] cached = CACHE.get(path);
        if (cached != null) {
            return cached;
        }
        final float[] waveform = extract(new File(path), BAR_COUNT);
        if (waveform != null) {
            CACHE.put(path, waveform);
        }
        return waveform;
    }

    private static float[] extract(final File file, final int barCount) {
        if (file == null || !file.isFile()) {
            return null;
        }
        MediaExtractor extractor = null;
        MediaCodec codec = null;
        try {
            extractor = new MediaExtractor();
            extractor.setDataSource(file.getAbsolutePath());
            int track = -1;
            MediaFormat format = null;
            for (int i = 0; i < extractor.getTrackCount(); ++i) {
                final MediaFormat candidate = extractor.getTrackFormat(i);
                final String mime = candidate.getString(MediaFormat.KEY_MIME);
                if (mime != null && mime.startsWith("audio/")) {
                    track = i;
                    format = candidate;
                    break;
                }
            }
            if (track < 0 || format == null) {
                return null;
            }
            extractor.selectTrack(track);
            codec = MediaCodec.createDecoderByType(format.getString(MediaFormat.KEY_MIME));
            codec.configure(format, null, null, 0);
            codec.start();
            final List<Float> blocks = decodeToBlocks(extractor, codec);
            if (blocks.isEmpty()) {
                return null;
            }
            return bucket(blocks, barCount);
        } catch (final Exception e) {
            Log.d(Config.LOGTAG, "could not extract waveform from " + file.getAbsolutePath(), e);
            return null;
        } finally {
            if (codec != null) {
                try {
                    codec.stop();
                } catch (final Exception ignored) {
                }
                codec.release();
            }
            if (extractor != null) {
                extractor.release();
            }
        }
    }

    private static List<Float> decodeToBlocks(
            final MediaExtractor extractor, final MediaCodec codec) throws IOException {
        final MediaCodec.BufferInfo info = new MediaCodec.BufferInfo();
        final List<Float> blocks = new ArrayList<>();
        boolean inputDone = false;
        boolean outputDone = false;
        float blockPeak = 0f;
        int blockSamples = 0;
        while (!outputDone && blocks.size() < MAX_BLOCKS) {
            if (!inputDone) {
                final int inputIndex = codec.dequeueInputBuffer(10_000);
                if (inputIndex >= 0) {
                    final ByteBuffer buffer = codec.getInputBuffer(inputIndex);
                    final int size = buffer == null ? -1 : extractor.readSampleData(buffer, 0);
                    if (size < 0) {
                        codec.queueInputBuffer(
                                inputIndex, 0, 0, 0, MediaCodec.BUFFER_FLAG_END_OF_STREAM);
                        inputDone = true;
                    } else {
                        codec.queueInputBuffer(inputIndex, 0, size, extractor.getSampleTime(), 0);
                        extractor.advance();
                    }
                }
            }
            final int outputIndex = codec.dequeueOutputBuffer(info, 10_000);
            if (outputIndex >= 0) {
                final ByteBuffer output = codec.getOutputBuffer(outputIndex);
                if (output != null && info.size > 0) {
                    output.order(ByteOrder.LITTLE_ENDIAN);
                    output.position(info.offset);
                    output.limit(info.offset + info.size);
                    // decoders output interleaved 16 bit PCM; taking the peak over all
                    // channels keeps this independent of the channel count
                    while (output.remaining() >= 2 && blocks.size() < MAX_BLOCKS) {
                        final int sample = Math.abs((int) output.getShort());
                        if (sample > blockPeak) {
                            blockPeak = sample;
                        }
                        if (++blockSamples >= BLOCK_SIZE) {
                            blocks.add(blockPeak / Short.MAX_VALUE);
                            blockPeak = 0f;
                            blockSamples = 0;
                        }
                    }
                }
                codec.releaseOutputBuffer(outputIndex, false);
                if ((info.flags & MediaCodec.BUFFER_FLAG_END_OF_STREAM) != 0) {
                    outputDone = true;
                }
            } else if (outputIndex == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                final MediaFormat outputFormat = codec.getOutputFormat();
                // MediaFormat.KEY_PCM_FORMAT (API 24, value "pcm-format") is not on the compile
                // classpath here; the key is simply absent on older decoders which means 16 bit
                final int pcmEncoding =
                        outputFormat.containsKey(KEY_PCM_FORMAT)
                                ? outputFormat.getInteger(KEY_PCM_FORMAT)
                                : AudioFormat.ENCODING_PCM_16BIT;
                if (pcmEncoding != AudioFormat.ENCODING_PCM_16BIT) {
                    // decoders for voice note codecs output 16 bit PCM; bail out otherwise so
                    // the caller can fall back to the plain track rendering
                    throw new IOException("unsupported pcm encoding " + pcmEncoding);
                }
            }
        }
        if (blockSamples > 0 && blockPeak > 0f && blocks.size() < MAX_BLOCKS) {
            blocks.add(blockPeak / Short.MAX_VALUE);
        }
        return blocks;
    }

    private static float[] bucket(final List<Float> blocks, final int barCount) {
        final int total = blocks.size();
        final float[] bars = new float[barCount];
        float max = 0f;
        for (int i = 0; i < barCount; ++i) {
            final int from = (int) ((long) i * total / barCount);
            final int to = Math.max((int) ((long) (i + 1) * total / barCount), from + 1);
            float peak = 0f;
            for (int j = from; j < to && j < total; ++j) {
                final float value = blocks.get(j);
                if (value > peak) {
                    peak = value;
                }
            }
            bars[i] = peak;
            if (peak > max) {
                max = peak;
            }
        }
        if (max <= 0f) {
            return null;
        }
        for (int i = 0; i < barCount; ++i) {
            // normalize and apply a square root curve so quieter parts remain visible
            bars[i] = (float) Math.sqrt(bars[i] / max);
        }
        return bars;
    }
}
