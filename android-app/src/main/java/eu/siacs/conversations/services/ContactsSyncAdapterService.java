package eu.siacs.conversations.services;

import android.app.Service;
import android.content.Intent;
import android.os.IBinder;
import androidx.annotation.Nullable;

public class ContactsSyncAdapterService extends Service {

    private static ContactsSyncAdapter sSyncAdapter;

    @Override
    public void onCreate() {
        synchronized (ContactsSyncAdapterService.class) {
            if (sSyncAdapter == null) {
                sSyncAdapter = new ContactsSyncAdapter(getApplicationContext(), true);
            }
        }
    }

    @Nullable
    @Override
    public IBinder onBind(final Intent intent) {
        return sSyncAdapter.getSyncAdapterBinder();
    }
}
