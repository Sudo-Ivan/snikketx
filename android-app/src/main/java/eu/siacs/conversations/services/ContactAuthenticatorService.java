package eu.siacs.conversations.services;

import android.accounts.AbstractAccountAuthenticator;
import android.accounts.Account;
import android.accounts.AccountAuthenticatorResponse;
import android.accounts.AccountManager;
import android.accounts.NetworkErrorException;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.os.Bundle;
import android.os.IBinder;
import androidx.annotation.Nullable;

public class ContactAuthenticatorService extends Service {

    private Authenticator authenticator;

    @Nullable
    @Override
    public IBinder onBind(final Intent intent) {
        if (AccountManager.ACTION_AUTHENTICATOR_INTENT.equals(intent.getAction())) {
            return getAuthenticator().getIBinder();
        }
        return null;
    }

    private synchronized Authenticator getAuthenticator() {
        if (authenticator == null) {
            authenticator = new Authenticator(this);
        }
        return authenticator;
    }

    private static class Authenticator extends AbstractAccountAuthenticator {

        private Authenticator(final Context context) {
            super(context);
        }

        @Override
        public Bundle addAccount(
                final AccountAuthenticatorResponse response,
                final String accountType,
                final String authTokenType,
                final String[] requiredFeatures,
                final Bundle options)
                throws NetworkErrorException {
            return null;
        }

        @Override
        public Bundle confirmCredentials(
                final AccountAuthenticatorResponse response,
                final Account account,
                final Bundle options)
                throws NetworkErrorException {
            return null;
        }

        @Override
        public Bundle editProperties(
                final AccountAuthenticatorResponse response, final String accountType) {
            return null;
        }

        @Override
        public Bundle getAuthToken(
                final AccountAuthenticatorResponse response,
                final Account account,
                final String authTokenType,
                final Bundle options)
                throws NetworkErrorException {
            return null;
        }

        @Override
        public String getAuthTokenLabel(final String authTokenType) {
            return null;
        }

        @Override
        public Bundle hasFeatures(
                final AccountAuthenticatorResponse response,
                final Account account,
                final String[] features)
                throws NetworkErrorException {
            return null;
        }

        @Override
        public Bundle updateCredentials(
                final AccountAuthenticatorResponse response,
                final Account account,
                final String authTokenType,
                final Bundle options)
                throws NetworkErrorException {
            return null;
        }
    }
}
