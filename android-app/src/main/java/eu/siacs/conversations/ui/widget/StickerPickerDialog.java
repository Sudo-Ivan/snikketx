package eu.siacs.conversations.ui.widget;

import android.app.Activity;
import android.graphics.BitmapFactory;
import android.view.LayoutInflater;
import android.view.ViewGroup;
import android.widget.ImageView;
import androidx.annotation.NonNull;
import androidx.appcompat.app.AlertDialog;
import androidx.databinding.DataBindingUtil;
import androidx.recyclerview.widget.GridLayoutManager;
import androidx.recyclerview.widget.RecyclerView;
import com.google.android.material.dialog.MaterialAlertDialogBuilder;
import com.google.common.collect.ImmutableList;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.DialogStickerPickerBinding;
import eu.siacs.conversations.stickers.StickerPack;
import java.io.File;
import java.util.List;
import java.util.function.BiConsumer;

/** Dialog that shows all available stickers in a grid and reports the selection. */
public class StickerPickerDialog {

    private static final int GRID_COLUMNS = 4;
    private static final int PREVIEW_SIZE = 192;

    private final List<StickerPack> packs;
    private final BiConsumer<StickerPack, StickerPack.Item> callback;

    public StickerPickerDialog(
            final List<StickerPack> packs,
            final BiConsumer<StickerPack, StickerPack.Item> callback) {
        this.packs = packs;
        this.callback = callback;
    }

    public AlertDialog create(final Activity activity) {
        final var builder = new MaterialAlertDialogBuilder(activity);
        builder.setTitle(R.string.sticker_picker_title);
        final var binding =
                DataBindingUtil.<DialogStickerPickerBinding>inflate(
                        activity.getLayoutInflater(), R.layout.dialog_sticker_picker, null, false);
        builder.setView(binding.getRoot());
        final var entries = flatten(packs);
        final var dialog = builder.create();
        if (entries.isEmpty()) {
            binding.emptyState.setVisibility(android.view.View.VISIBLE);
            binding.stickerGrid.setVisibility(android.view.View.GONE);
        } else {
            binding.stickerGrid.setLayoutManager(new GridLayoutManager(activity, GRID_COLUMNS));
            binding.stickerGrid.setAdapter(
                    new StickerGridAdapter(entries, dialog::dismiss, callback));
        }
        return dialog;
    }

    private static List<Entry> flatten(final List<StickerPack> packs) {
        final var builder = new ImmutableList.Builder<Entry>();
        for (final StickerPack pack : packs) {
            for (final StickerPack.Item item : pack.getItems()) {
                if (item.getFile() != null) {
                    builder.add(new Entry(pack, item));
                }
            }
        }
        return builder.build();
    }

    private record Entry(StickerPack pack, StickerPack.Item item) {}

    private static class StickerGridAdapter extends RecyclerView.Adapter<StickerViewHolder> {

        private final List<Entry> entries;
        private final Runnable dismiss;
        private final BiConsumer<StickerPack, StickerPack.Item> callback;

        private StickerGridAdapter(
                final List<Entry> entries,
                final Runnable dismiss,
                final BiConsumer<StickerPack, StickerPack.Item> callback) {
            this.entries = entries;
            this.dismiss = dismiss;
            this.callback = callback;
        }

        @NonNull
        @Override
        public StickerViewHolder onCreateViewHolder(
                @NonNull final ViewGroup parent, final int viewType) {
            final var view =
                    (ImageView)
                            LayoutInflater.from(parent.getContext())
                                    .inflate(R.layout.item_sticker, parent, false);
            return new StickerViewHolder(view);
        }

        @Override
        public void onBindViewHolder(@NonNull final StickerViewHolder holder, final int position) {
            final var entry = entries.get(position);
            final File file = entry.item().getFile();
            holder.image.setImageBitmap(file == null ? null : decodePreview(file));
            final var desc = entry.item().getDescription();
            holder.image.setContentDescription(desc == null ? entry.pack().getName() : desc);
            holder.image.setOnClickListener(
                    v -> {
                        callback.accept(entry.pack(), entry.item());
                        dismiss.run();
                    });
        }

        @Override
        public int getItemCount() {
            return entries.size();
        }
    }

    private static class StickerViewHolder extends RecyclerView.ViewHolder {
        private final ImageView image;

        private StickerViewHolder(final ImageView image) {
            super(image);
            this.image = image;
        }
    }

    private static android.graphics.Bitmap decodePreview(final File file) {
        final var bounds = new BitmapFactory.Options();
        bounds.inJustDecodeBounds = true;
        BitmapFactory.decodeFile(file.getAbsolutePath(), bounds);
        final var options = new BitmapFactory.Options();
        int sample = 1;
        while (Math.max(bounds.outWidth, bounds.outHeight) / (sample * 2) >= PREVIEW_SIZE) {
            sample *= 2;
        }
        options.inSampleSize = sample;
        return BitmapFactory.decodeFile(file.getAbsolutePath(), options);
    }
}
