package eu.siacs.conversations.utils;

import eu.siacs.conversations.ui.XmppActivity;

public final class UpdateChecker {

    private UpdateChecker() {}

    // Play Store builds must not self-update; updates ship via the store.
    public static void checkIfDue(final XmppActivity activity) {}
}
