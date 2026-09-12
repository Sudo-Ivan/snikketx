package eu.siacs.conversations.widget;

import android.content.Context;
import android.content.Intent;
import android.graphics.Bitmap;
import android.view.View;
import android.widget.RemoteViews;
import android.widget.RemoteViewsService;
import eu.siacs.conversations.AppSettings;
import eu.siacs.conversations.R;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.services.AvatarService;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.ui.ConversationsActivity;
import eu.siacs.conversations.utils.IrregularUnicodeDetector;
import eu.siacs.conversations.utils.UIHelper;
import eu.siacs.conversations.xmpp.Jid;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class RecentChatsWidgetService extends RemoteViewsService {

    @Override
    public RemoteViewsFactory onGetViewFactory(final Intent intent) {
        return new RecentChatsFactory(getApplicationContext());
    }

    private static class RecentChatsFactory implements RemoteViewsFactory {

        // avatars are shipped across the binder as bitmaps, so cap the number of rows
        private static final int MAX_ITEMS = 20;

        private final Context context;
        private WidgetServiceConnector connector;
        private XmppConnectionService service;
        private List<Conversation> conversations = Collections.emptyList();
        private boolean showPreview = true;
        private int avatarSize;

        RecentChatsFactory(final Context context) {
            this.context = context;
        }

        @Override
        public void onCreate() {
            avatarSize = AvatarService.getSystemUiAvatarSize(context);
            connector = WidgetServiceConnector.bind(context);
        }

        @Override
        public void onDataSetChanged() {
            showPreview = new AppSettings(context).isWidgetShowMessagePreview();
            if (service == null) {
                try {
                    service = connector.awaitService();
                } catch (final InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            }
            if (service == null) {
                conversations = Collections.emptyList();
                return;
            }
            try {
                service.restoredFromDatabaseLatch.await(10, TimeUnit.SECONDS);
            } catch (final InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            final List<Conversation> sorted = new ArrayList<>(service.getConversations());
            Collections.sort(sorted);
            conversations = sorted.subList(0, Math.min(sorted.size(), MAX_ITEMS));
        }

        @Override
        public void onDestroy() {
            if (connector != null) {
                connector.unbind(context);
            }
        }

        @Override
        public int getCount() {
            return conversations.size();
        }

        @Override
        public RemoteViews getViewAt(final int position) {
            final RemoteViews views =
                    new RemoteViews(context.getPackageName(), R.layout.widget_recent_chats_item);
            if (position >= conversations.size()) {
                return views;
            }
            final Conversation conversation = conversations.get(position);

            final CharSequence name = conversation.getName();
            views.setTextViewText(
                    R.id.widget_item_name,
                    name instanceof Jid
                            ? IrregularUnicodeDetector.style(context, (Jid) name)
                            : name);

            final CharSequence preview = preview(conversation);
            if (preview == null || preview.length() == 0) {
                views.setViewVisibility(R.id.widget_item_preview, View.GONE);
            } else {
                views.setTextViewText(R.id.widget_item_preview, preview);
                views.setViewVisibility(R.id.widget_item_preview, View.VISIBLE);
            }

            final int unread = conversation.unreadCount();
            if (unread > 0) {
                views.setTextViewText(
                        R.id.widget_item_unread, unread > 99 ? "99+" : String.valueOf(unread));
                views.setViewVisibility(R.id.widget_item_unread, View.VISIBLE);
            } else {
                views.setViewVisibility(R.id.widget_item_unread, View.GONE);
            }

            if (service != null) {
                try {
                    final Bitmap avatar =
                            service.getAvatarService().get(conversation, avatarSize, false);
                    if (avatar != null) {
                        views.setImageViewBitmap(R.id.widget_item_avatar, avatar);
                    }
                } catch (final Exception e) {
                    // keep the placeholder if avatar generation fails
                }
            }

            final Intent fillIn = new Intent();
            fillIn.putExtra(ConversationsActivity.EXTRA_CONVERSATION, conversation.getUuid());
            views.setOnClickFillInIntent(R.id.widget_item_root, fillIn);
            return views;
        }

        private CharSequence preview(final Conversation conversation) {
            if (!showPreview) {
                return null;
            }
            final Conversation.Draft draft = conversation.isRead() ? conversation.getDraft() : null;
            if (draft != null) {
                return context.getString(R.string.draft) + ": " + draft.message();
            }
            return UIHelper.shorten(
                    UIHelper.getMessagePreview(context, conversation.getLatestMessage()).first);
        }

        @Override
        public RemoteViews getLoadingView() {
            return null;
        }

        @Override
        public int getViewTypeCount() {
            return 1;
        }

        @Override
        public long getItemId(final int position) {
            if (position >= conversations.size()) {
                return position;
            }
            return conversations.get(position).getUuid().hashCode();
        }

        @Override
        public boolean hasStableIds() {
            return true;
        }
    }
}
