package eu.siacs.conversations.widget;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.widget.RemoteViews;
import eu.siacs.conversations.R;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.ui.ConversationsActivity;
import java.util.concurrent.TimeUnit;

public class RecentChatsWidgetProvider extends AppWidgetProvider {

    @Override
    public void onUpdate(
            final Context context,
            final AppWidgetManager appWidgetManager,
            final int[] appWidgetIds) {
        // the unread total lives in the service, so fetch it on a background thread via goAsync
        final PendingResult pendingResult = goAsync();
        final Context appContext = context.getApplicationContext();
        new Thread(
                        () -> {
                            int unread = 0;
                            WidgetServiceConnector connector = null;
                            try {
                                connector = WidgetServiceConnector.bind(appContext);
                                final XmppConnectionService service = connector.awaitService();
                                if (service != null) {
                                    service.restoredFromDatabaseLatch.await(10, TimeUnit.SECONDS);
                                    unread = service.unreadCount();
                                }
                            } catch (final InterruptedException e) {
                                Thread.currentThread().interrupt();
                            } finally {
                                if (connector != null) {
                                    connector.unbind(appContext);
                                }
                                updateWidgets(appContext, appWidgetIds, unread);
                                pendingResult.finish();
                            }
                        },
                        "recent-chats-widget-update")
                .start();
    }

    private static void updateWidgets(
            final Context context, final int[] appWidgetIds, final int unreadCount) {
        final AppWidgetManager appWidgetManager = AppWidgetManager.getInstance(context);
        for (final int appWidgetId : appWidgetIds) {
            appWidgetManager.updateAppWidget(
                    appWidgetId, buildRemoteViews(context, appWidgetId, unreadCount));
        }
    }

    /**
     * Called from {@link XmppConnectionService#updateConversationUi()} and {@link
     * XmppConnectionService#updateRosterUi()} so the widget refreshes whenever the conversation
     * list or roster changes. Returns immediately when no widget is placed.
     */
    public static void notifyConversationsChanged(final XmppConnectionService service) {
        final AppWidgetManager appWidgetManager = AppWidgetManager.getInstance(service);
        final int[] appWidgetIds =
                appWidgetManager.getAppWidgetIds(
                        new ComponentName(service, RecentChatsWidgetProvider.class));
        if (appWidgetIds.length == 0) {
            return;
        }
        appWidgetManager.notifyAppWidgetViewDataChanged(appWidgetIds, R.id.widget_list);
        // the unread total in the header is not part of the collection, so update it separately
        final RemoteViews partial =
                new RemoteViews(service.getPackageName(), R.layout.widget_recent_chats);
        partial.setTextViewText(
                R.id.widget_unread_total, formatUnreadTotal(service, service.unreadCount()));
        appWidgetManager.partiallyUpdateAppWidget(appWidgetIds, partial);
    }

    private static RemoteViews buildRemoteViews(
            final Context context, final int appWidgetId, final int unreadCount) {
        final RemoteViews views =
                new RemoteViews(context.getPackageName(), R.layout.widget_recent_chats);

        final Intent serviceIntent = new Intent(context, RecentChatsWidgetService.class);
        serviceIntent.putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId);
        // a unique data uri keeps one widget instance from sharing its factory with others
        serviceIntent.setData(Uri.parse(serviceIntent.toUri(Intent.URI_INTENT_SCHEME)));
        views.setRemoteAdapter(R.id.widget_list, serviceIntent);
        views.setEmptyView(R.id.widget_list, R.id.widget_empty);

        // template for the per-row fill-in intent carrying the conversation uuid
        final Intent viewConversation = new Intent(context, ConversationsActivity.class);
        viewConversation.setAction(ConversationsActivity.ACTION_VIEW_CONVERSATION);
        final PendingIntent viewConversationPendingIntent =
                PendingIntent.getActivity(
                        context,
                        0,
                        viewConversation,
                        PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        views.setPendingIntentTemplate(R.id.widget_list, viewConversationPendingIntent);

        views.setTextViewText(R.id.widget_unread_total, formatUnreadTotal(context, unreadCount));
        final Intent launch = new Intent(context, ConversationsActivity.class);
        launch.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        views.setOnClickPendingIntent(
                R.id.widget_header,
                PendingIntent.getActivity(
                        context,
                        1,
                        launch,
                        PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE));
        return views;
    }

    private static CharSequence formatUnreadTotal(final Context context, final int unreadCount) {
        if (unreadCount <= 0) {
            return "";
        }
        return context.getResources()
                .getQuantityString(R.plurals.widget_unread_total, unreadCount, unreadCount);
    }
}
