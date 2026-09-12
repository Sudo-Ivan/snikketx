package eu.siacs.conversations.widget;

import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.ServiceConnection;
import android.os.IBinder;
import eu.siacs.conversations.services.XmppConnectionService;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Binds to {@link XmppConnectionService} from a context that is not an XmppActivity (widget
 * provider and remote views factory). {@link #awaitService()} blocks the calling thread, so it must
 * only be used on background threads such as the binder thread running RemoteViewsFactory callbacks
 * or a goAsync broadcast thread.
 */
final class WidgetServiceConnector implements ServiceConnection {

    private static final long BIND_TIMEOUT_MS = 10_000;

    private final CountDownLatch connected = new CountDownLatch(1);
    private volatile XmppConnectionService service;

    private WidgetServiceConnector() {}

    static WidgetServiceConnector bind(final Context context) {
        final var connector = new WidgetServiceConnector();
        final var intent = new Intent(context, XmppConnectionService.class);
        context.bindService(intent, connector, Context.BIND_AUTO_CREATE);
        return connector;
    }

    XmppConnectionService awaitService() throws InterruptedException {
        connected.await(BIND_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        return service;
    }

    void unbind(final Context context) {
        try {
            context.unbindService(this);
        } catch (final IllegalArgumentException ignored) {
        }
    }

    @Override
    public void onServiceConnected(final ComponentName name, final IBinder binder) {
        service = ((XmppConnectionService.XmppConnectionBinder) binder).getService();
        connected.countDown();
    }

    @Override
    public void onServiceDisconnected(final ComponentName name) {
        service = null;
    }
}
