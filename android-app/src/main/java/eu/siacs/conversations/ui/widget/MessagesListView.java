package eu.siacs.conversations.ui.widget;

import android.annotation.SuppressLint;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.drawable.Drawable;
import android.util.AttributeSet;
import android.view.HapticFeedbackConstants;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewConfiguration;
import android.view.ViewParent;
import android.widget.AdapterView;
import android.widget.ListView;
import androidx.annotation.Nullable;
import androidx.appcompat.content.res.AppCompatResources;
import androidx.core.view.ViewCompat;
import com.google.android.material.color.MaterialColors;
import eu.siacs.conversations.R;

public class MessagesListView extends ListView {

    private static final float SWIPE_REPLY_THRESHOLD = 0.35f;
    private static final long SNAP_BACK_DURATION_MS = 200L;

    public interface OnSwipeReplyListener {
        boolean canSwipeReply(final int position);

        void onSwipeReply(final int position);
    }

    private final int touchSlop;
    private final int replyIconMargin;
    private Drawable replyIcon;
    private OnSwipeReplyListener swipeReplyListener;
    private float downX;
    private float downY;
    private int downPosition = AdapterView.INVALID_POSITION;
    private View swipingChild;
    private boolean swiping;
    private boolean thresholdFeedbackSent;

    public MessagesListView(final Context context) {
        this(context, null);
    }

    public MessagesListView(final Context context, final AttributeSet attrs) {
        this(context, attrs, 0);
    }

    public MessagesListView(
            final Context context, final AttributeSet attrs, final int defStyleAttr) {
        super(context, attrs, defStyleAttr);
        this.touchSlop = ViewConfiguration.get(context).getScaledTouchSlop();
        this.replyIconMargin = (int) (24f * getResources().getDisplayMetrics().density);
        final Drawable icon = AppCompatResources.getDrawable(context, R.drawable.ic_reply_24dp);
        if (icon != null) {
            this.replyIcon = icon.mutate();
            this.replyIcon.setTint(
                    MaterialColors.getColor(this, androidx.appcompat.R.attr.colorPrimary));
        }
    }

    public void setOnSwipeReplyListener(@Nullable final OnSwipeReplyListener listener) {
        this.swipeReplyListener = listener;
    }

    private boolean isLayoutRtl() {
        return ViewCompat.getLayoutDirection(this) == ViewCompat.LAYOUT_DIRECTION_RTL;
    }

    private boolean isSwipeTowardsReplyEdge(final float dx) {
        return isLayoutRtl() ? dx < 0 : dx > 0;
    }

    @Override
    public boolean onInterceptTouchEvent(final MotionEvent ev) {
        switch (ev.getActionMasked()) {
            case MotionEvent.ACTION_DOWN -> {
                downX = ev.getX();
                downY = ev.getY();
                downPosition = pointToPosition((int) downX, (int) downY);
                swiping = false;
                thresholdFeedbackSent = false;
            }
            case MotionEvent.ACTION_MOVE -> {
                if (swiping) {
                    return true;
                }
                if (swipeReplyListener != null && downPosition != AdapterView.INVALID_POSITION) {
                    final float dx = ev.getX() - downX;
                    final float dy = ev.getY() - downY;
                    if (Math.abs(dx) > touchSlop
                            && Math.abs(dx) > Math.abs(dy)
                            && isSwipeTowardsReplyEdge(dx)
                            && swipeReplyListener.canSwipeReply(downPosition)) {
                        final View child = getChildAt(downPosition - getFirstVisiblePosition());
                        if (child != null) {
                            swiping = true;
                            swipingChild = child;
                            final ViewParent parent = getParent();
                            if (parent != null) {
                                parent.requestDisallowInterceptTouchEvent(true);
                            }
                            return true;
                        }
                    }
                }
            }
            case MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> resetSwipeState();
        }
        return super.onInterceptTouchEvent(ev);
    }

    @Override
    public boolean performClick() {
        // ListView items handle their own clicks; this override exists so that
        // accessibility tooling sees a proper click path for the custom touch handling.
        return super.performClick();
    }

    // click handling stays in super.onTouchEvent, which routes to performItemClick
    @SuppressLint("ClickableViewAccessibility")
    @Override
    public boolean onTouchEvent(final MotionEvent ev) {
        if (swiping && swipingChild != null) {
            switch (ev.getActionMasked()) {
                case MotionEvent.ACTION_MOVE -> {
                    final float width = swipingChild.getWidth();
                    final float dx = ev.getX() - downX;
                    final float translation =
                            isLayoutRtl()
                                    ? Math.max(-width, Math.min(dx, 0f))
                                    : Math.max(0f, Math.min(dx, width));
                    swipingChild.setTranslationX(translation);
                    if (!thresholdFeedbackSent
                            && Math.abs(translation) >= width * SWIPE_REPLY_THRESHOLD) {
                        thresholdFeedbackSent = true;
                        performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP);
                    }
                    invalidate();
                    return true;
                }
                case MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    // a cancelled gesture (parent intercept, incoming call) must not fire
                    // the reply action even when the threshold was crossed
                    releaseSwipingChild(ev.getActionMasked() == MotionEvent.ACTION_UP);
                    resetSwipeState();
                    return true;
                }
                default -> {
                    return true;
                }
            }
        }
        return super.onTouchEvent(ev);
    }

    private void releaseSwipingChild(final boolean released) {
        final View child = swipingChild;
        if (child == null) {
            return;
        }
        final boolean triggered =
                released
                        && Math.abs(child.getTranslationX())
                                >= child.getWidth() * SWIPE_REPLY_THRESHOLD;
        child.animate()
                .translationX(0f)
                .setDuration(SNAP_BACK_DURATION_MS)
                .setUpdateListener(animation -> invalidate())
                .withEndAction(
                        () -> {
                            if (!swiping) {
                                child.setTranslationX(0f);
                                if (swipingChild == child) {
                                    swipingChild = null;
                                }
                            }
                            invalidate();
                        })
                .start();
        if (triggered && swipeReplyListener != null) {
            announceForAccessibility(getResources().getString(R.string.swipe_to_reply));
            swipeReplyListener.onSwipeReply(downPosition);
        }
    }

    private void resetSwipeState() {
        downPosition = AdapterView.INVALID_POSITION;
        swiping = false;
        thresholdFeedbackSent = false;
    }

    @Override
    protected void dispatchDraw(final Canvas canvas) {
        super.dispatchDraw(canvas);
        final View child = swipingChild;
        final Drawable icon = replyIcon;
        if (child == null || icon == null) {
            return;
        }
        final float dx = Math.abs(child.getTranslationX());
        if (dx <= 0f) {
            return;
        }
        final int iconSize = icon.getIntrinsicWidth();
        final int top = child.getTop() + (child.getHeight() - iconSize) / 2;
        final float threshold = child.getWidth() * SWIPE_REPLY_THRESHOLD;
        final float progress = Math.min(1f, dx / threshold);
        final float center = Math.min(dx / 2f, replyIconMargin + iconSize / 2f);
        final int left =
                isLayoutRtl()
                        ? Math.round(getWidth() - center - iconSize / 2f)
                        : Math.round(center - iconSize / 2f);
        icon.setLayoutDirection(getLayoutDirection());
        icon.setAlpha(Math.round(255 * progress));
        icon.setBounds(left, top, left + iconSize, top + iconSize);
        icon.draw(canvas);
    }

    @Override
    protected void onDetachedFromWindow() {
        final View child = swipingChild;
        if (child != null) {
            child.animate().cancel();
            child.setTranslationX(0f);
            swipingChild = null;
        }
        super.onDetachedFromWindow();
    }
}
