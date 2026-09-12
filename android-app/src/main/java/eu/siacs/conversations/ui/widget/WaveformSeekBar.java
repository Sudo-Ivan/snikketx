package eu.siacs.conversations.ui.widget;

import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.RectF;
import android.util.AttributeSet;
import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.appcompat.widget.AppCompatSeekBar;
import com.google.android.material.color.MaterialColors;

/**
 * A SeekBar that renders an amplitude waveform (for voice messages) instead of a plain track.
 * Scrubbing is inherited from SeekBar. When no amplitudes have been set a simple line track is
 * drawn so the widget can stand in for a regular SeekBar (e.g. generic audio attachments).
 *
 * <p>The progress drawable is expected to be transparent so only the thumb is drawn by super.
 */
public class WaveformSeekBar extends AppCompatSeekBar {

    private static final float BAR_GAP_RATIO = 0.28f;
    private static final float MIN_BAR_HEIGHT_RATIO = 0.08f;
    private static final float TRACK_HEIGHT_RATIO = 0.10f;

    private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final RectF rect = new RectF();

    private float[] amplitudes;
    private int playedColor;
    private int unplayedColor;
    private boolean colorsInitialized = false;

    public WaveformSeekBar(@NonNull final Context context) {
        super(context);
    }

    public WaveformSeekBar(@NonNull final Context context, @Nullable final AttributeSet attrs) {
        super(context, attrs);
    }

    public WaveformSeekBar(
            @NonNull final Context context,
            @Nullable final AttributeSet attrs,
            final int defStyleAttr) {
        super(context, attrs, defStyleAttr);
    }

    public void setAmplitudes(@Nullable final float[] amplitudes) {
        this.amplitudes = amplitudes;
        invalidate();
    }

    private void ensureColors() {
        if (colorsInitialized) {
            return;
        }
        playedColor = MaterialColors.getColor(this, androidx.appcompat.R.attr.colorPrimary);
        unplayedColor =
                MaterialColors.getColor(
                        this, com.google.android.material.R.attr.colorOutlineVariant);
        colorsInitialized = true;
    }

    @Override
    protected synchronized void onDraw(@NonNull final Canvas canvas) {
        ensureColors();
        final int width = getWidth() - getPaddingLeft() - getPaddingRight();
        final int height = getHeight() - getPaddingTop() - getPaddingBottom();
        if (width > 0 && height > 0) {
            final float max = getMax() <= 0 ? 1f : getMax();
            final float fraction = Math.min(Math.max(getProgress() / max, 0f), 1f);
            final float playedBoundary = getPaddingLeft() + fraction * width;
            final float centerY = getPaddingTop() + height / 2f;
            if (amplitudes != null && amplitudes.length > 0) {
                drawWaveform(canvas, playedBoundary, centerY, width, height);
            } else {
                drawTrack(canvas, playedBoundary, centerY, width, height);
            }
        }
        // draws the (transparent) progress drawable and the thumb on top of the waveform
        super.onDraw(canvas);
    }

    private void drawWaveform(
            final Canvas canvas,
            final float playedBoundary,
            final float centerY,
            final int width,
            final int height) {
        final int count = amplitudes.length;
        final float slot = width / (float) count;
        final float barWidth = Math.max(1f, slot * (1f - BAR_GAP_RATIO));
        final float minBarHeight = height * MIN_BAR_HEIGHT_RATIO;
        for (int i = 0; i < count; ++i) {
            final float left = getPaddingLeft() + i * slot + (slot - barWidth) / 2f;
            final float barCenter = left + barWidth / 2f;
            final float amplitude = Math.min(Math.max(amplitudes[i], 0f), 1f);
            final float barHeight = Math.max(amplitude * height, minBarHeight);
            rect.set(left, centerY - barHeight / 2f, left + barWidth, centerY + barHeight / 2f);
            paint.setColor(barCenter <= playedBoundary ? playedColor : unplayedColor);
            final float radius = barWidth / 2f;
            canvas.drawRoundRect(rect, radius, radius, paint);
        }
    }

    private void drawTrack(
            final Canvas canvas,
            final float playedBoundary,
            final float centerY,
            final int width,
            final int height) {
        final float trackHeight = Math.max(height * TRACK_HEIGHT_RATIO, 2f);
        final float top = centerY - trackHeight / 2f;
        final float bottom = centerY + trackHeight / 2f;
        final float radius = trackHeight / 2f;
        paint.setColor(unplayedColor);
        rect.set(getPaddingLeft(), top, getPaddingLeft() + width, bottom);
        canvas.drawRoundRect(rect, radius, radius, paint);
        if (playedBoundary > getPaddingLeft()) {
            paint.setColor(playedColor);
            rect.set(getPaddingLeft(), top, playedBoundary, bottom);
            canvas.drawRoundRect(rect, radius, radius, paint);
        }
    }
}
