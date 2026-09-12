package eu.siacs.conversations.ui.util;

import android.content.Context;
import androidx.annotation.DrawableRes;
import androidx.annotation.Nullable;
import androidx.annotation.StringRes;
import com.google.common.collect.ImmutableList;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.R;
import eu.siacs.conversations.entities.Conversation;
import java.util.List;

public final class ChatWallpapers {

    public static final String NONE = "none";

    public record Wallpaper(String key, @StringRes int label, @DrawableRes int drawable) {}

    private static final List<Wallpaper> WALLPAPERS =
            ImmutableList.of(
                    new Wallpaper(
                            "tonal", R.string.wallpaper_tonal, R.drawable.chat_wallpaper_tonal),
                    new Wallpaper(
                            "sunbeam",
                            R.string.wallpaper_sunbeam,
                            R.drawable.chat_wallpaper_sunbeam),
                    new Wallpaper(
                            "peach", R.string.wallpaper_peach, R.drawable.chat_wallpaper_peach),
                    new Wallpaper(
                            "ocean", R.string.wallpaper_ocean, R.drawable.chat_wallpaper_ocean),
                    new Wallpaper(
                            "bubbles",
                            R.string.wallpaper_bubbles,
                            R.drawable.chat_wallpaper_bubbles),
                    new Wallpaper("grid", R.string.wallpaper_grid, R.drawable.chat_wallpaper_grid));

    private ChatWallpapers() {}

    public static List<Wallpaper> all() {
        return WALLPAPERS;
    }

    public static CharSequence[] labels(final Context context) {
        final CharSequence[] labels = new CharSequence[WALLPAPERS.size() + 1];
        labels[0] = context.getString(R.string.wallpaper_none);
        for (int i = 0; i < WALLPAPERS.size(); ++i) {
            labels[i + 1] = context.getString(WALLPAPERS.get(i).label());
        }
        return labels;
    }

    public static String keyAt(final int index) {
        if (index <= 0 || index > WALLPAPERS.size()) {
            return NONE;
        }
        return WALLPAPERS.get(index - 1).key();
    }

    public static int indexOfKey(@Nullable final String key) {
        for (int i = 0; i < WALLPAPERS.size(); ++i) {
            if (WALLPAPERS.get(i).key().equals(key)) {
                return i + 1;
            }
        }
        return 0;
    }

    @DrawableRes
    public static int resolve(@Nullable final String key) {
        for (final var wallpaper : WALLPAPERS) {
            if (wallpaper.key().equals(key)) {
                return wallpaper.drawable();
            }
        }
        return 0;
    }

    @DrawableRes
    public static int resolve(final Context context, final Conversation conversation) {
        String key = conversation.getWallpaper();
        if (key == null) {
            key = new AppSettings(context).getChatWallpaper();
        }
        return resolve(key);
    }
}
