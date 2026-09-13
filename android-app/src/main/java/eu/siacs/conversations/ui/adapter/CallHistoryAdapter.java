package eu.siacs.conversations.ui.adapter;

import android.content.res.ColorStateList;
import android.graphics.drawable.Drawable;
import android.util.Log;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import androidx.annotation.NonNull;
import androidx.appcompat.content.res.AppCompatResources;
import androidx.core.graphics.drawable.DrawableCompat;
import androidx.databinding.DataBindingUtil;
import androidx.recyclerview.widget.RecyclerView;
import com.google.android.material.color.MaterialColors;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.ItemCallHistoryBinding;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.entities.Conversational;
import eu.siacs.conversations.entities.Message;
import eu.siacs.conversations.entities.RtpSessionStatus;
import eu.siacs.conversations.ui.XmppActivity;
import eu.siacs.conversations.ui.util.AvatarWorkerTask;
import eu.siacs.conversations.utils.IrregularUnicodeDetector;
import eu.siacs.conversations.utils.TimeFrameUtils;
import eu.siacs.conversations.utils.UIHelper;
import eu.siacs.conversations.xmpp.Jid;
import java.util.List;

public class CallHistoryAdapter
        extends RecyclerView.Adapter<CallHistoryAdapter.CallHistoryViewHolder> {

    private final XmppActivity activity;
    private final List<CallLogEntry> entries;
    private OnCallLogEntryClickListener clickListener;
    private OnCallBackClickListener callBackListener;

    public CallHistoryAdapter(final XmppActivity activity, final List<CallLogEntry> entries) {
        this.activity = activity;
        this.entries = entries;
    }

    @NonNull
    @Override
    public CallHistoryViewHolder onCreateViewHolder(
            @NonNull final ViewGroup parent, final int viewType) {
        return new CallHistoryViewHolder(
                DataBindingUtil.inflate(
                        LayoutInflater.from(parent.getContext()),
                        R.layout.item_call_history,
                        parent,
                        false));
    }

    @Override
    public void onBindViewHolder(
            @NonNull final CallHistoryViewHolder viewHolder, final int position) {
        final CallLogEntry entry = entries.get(position);
        final Message message = entry.message;
        final Conversation conversation = entry.conversation;

        final CharSequence name = getDisplayName(conversation);
        if (name instanceof Jid) {
            viewHolder.binding.callName.setText(
                    IrregularUnicodeDetector.style(activity, (Jid) name));
        } else {
            viewHolder.binding.callName.setText(name);
        }

        final boolean received = message.getStatus() <= Message.STATUS_RECEIVED;
        final RtpSessionStatus rtpSessionStatus = RtpSessionStatus.of(message.getBody());
        final boolean missed = !rtpSessionStatus.successful;

        final int iconTint;
        final int textColor;
        if (missed) {
            iconTint =
                    MaterialColors.getColor(
                            viewHolder.binding.callDetails, androidx.appcompat.R.attr.colorError);
            textColor = iconTint;
        } else {
            iconTint =
                    MaterialColors.getColor(
                            viewHolder.binding.callDetails,
                            androidx.appcompat.R.attr.colorControlNormal);
            textColor =
                    MaterialColors.getColor(
                            viewHolder.binding.callDetails,
                            com.google.android.material.R.attr.colorOnSurfaceVariant);
        }
        final Drawable directionIcon =
                AppCompatResources.getDrawable(
                        activity,
                        RtpSessionStatus.getDrawable(received, rtpSessionStatus.successful));
        if (directionIcon != null) {
            final int iconSize =
                    Math.round(18f * activity.getResources().getDisplayMetrics().scaledDensity);
            directionIcon.setBounds(0, 0, iconSize, iconSize);
            DrawableCompat.setTintList(directionIcon, ColorStateList.valueOf(iconTint));
        }
        viewHolder.binding.callDetails.setCompoundDrawablesRelative(
                directionIcon, null, null, null);
        viewHolder.binding.callDetails.setTextColor(textColor);

        final int label;
        if (conversation.getMode() == Conversational.MODE_MULTI) {
            label = R.string.group_call;
        } else if (missed && received) {
            label = R.string.missed_call;
        } else {
            label = received ? R.string.incoming_call : R.string.outgoing_call;
        }
        final StringBuilder details = new StringBuilder(activity.getString(label));
        details.append(", ")
                .append(UIHelper.readableTimeDifferenceFull(activity, message.getTimeSent()));
        if (rtpSessionStatus.duration > 0) {
            details.append(", ")
                    .append(TimeFrameUtils.resolve(activity, rtpSessionStatus.duration));
        }
        viewHolder.binding.callDetails.setText(details);

        AvatarWorkerTask.loadAvatar(
                conversation,
                viewHolder.binding.callAvatar,
                R.dimen.avatar_on_conversation_overview);

        viewHolder.binding.callButton.setVisibility(
                conversation.getMode() == Conversational.MODE_MULTI ? View.GONE : View.VISIBLE);
        viewHolder.binding.callButton.setOnClickListener(
                v -> {
                    if (callBackListener != null) {
                        callBackListener.onCallBackClick(entry);
                    }
                });
        viewHolder.itemView.setOnClickListener(
                v -> {
                    if (clickListener != null) {
                        clickListener.onCallLogEntryClick(entry);
                    }
                });
    }

    private static CharSequence getDisplayName(final Conversation conversation) {
        try {
            return conversation.getName();
        } catch (final Exception e) {
            Log.w(Config.LOGTAG, "unable to get conversation name for call log", e);
            final var address = conversation.getAddress();
            return address == null ? "" : address.toString();
        }
    }

    @Override
    public int getItemCount() {
        return entries.size();
    }

    public void setOnCallLogEntryClickListener(final OnCallLogEntryClickListener listener) {
        this.clickListener = listener;
    }

    public void setOnCallBackClickListener(final OnCallBackClickListener listener) {
        this.callBackListener = listener;
    }

    public interface OnCallLogEntryClickListener {
        void onCallLogEntryClick(CallLogEntry entry);
    }

    public interface OnCallBackClickListener {
        void onCallBackClick(CallLogEntry entry);
    }

    public static class CallLogEntry {
        public final Message message;
        public final Conversation conversation;

        public CallLogEntry(final Message message, final Conversation conversation) {
            this.message = message;
            this.conversation = conversation;
        }
    }

    public static class CallHistoryViewHolder extends RecyclerView.ViewHolder {
        public final ItemCallHistoryBinding binding;

        private CallHistoryViewHolder(final ItemCallHistoryBinding binding) {
            super(binding.getRoot());
            this.binding = binding;
        }
    }
}
