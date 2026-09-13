package eu.siacs.conversations.xmpp.manager;

import android.util.Log;
import com.google.common.base.Strings;
import com.google.common.util.concurrent.ListenableFuture;
import eu.siacs.conversations.Config;
import eu.siacs.conversations.entities.Contact;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.xml.Namespace;
import eu.siacs.conversations.xmpp.Jid;
import eu.siacs.conversations.xmpp.XmppConnection;
import im.conversations.android.xmpp.NodeConfiguration;
import im.conversations.android.xmpp.model.activity.Activity;
import im.conversations.android.xmpp.model.mood.Mood;
import im.conversations.android.xmpp.model.pubsub.Items;
import im.conversations.android.xmpp.model.tune.Tune;

public class UserStateManager extends AbstractManager {

    private final XmppConnectionService service;

    public UserStateManager(final XmppConnectionService service, final XmppConnection connection) {
        super(service.getApplicationContext(), connection);
        this.service = service;
    }

    public void handleMoodItems(final Jid from, final Items items) {
        final var mood = items.getFirstItem(Mood.class);
        setMood(from, mood == null ? null : mood.getMood(), mood == null ? null : mood.getText());
    }

    public void handleActivityItems(final Jid from, final Items items) {
        final var activity = items.getFirstItem(Activity.class);
        if (activity == null) {
            setActivity(from, null, null, null);
        } else {
            setActivity(from, activity.getGeneral(), activity.getSpecific(), activity.getText());
        }
    }

    public void handleTuneItems(final Jid from, final Items items) {
        final var tune = items.getFirstItem(Tune.class);
        setTune(from, tune == null ? null : tune.getDescription());
    }

    public void handleDelete(final Jid from, final String node) {
        if (Namespace.MOOD.equals(node)) {
            setMood(from, null, null);
        } else if (Namespace.ACTIVITY.equals(node)) {
            setActivity(from, null, null, null);
        } else if (Namespace.TUNE.equals(node)) {
            setTune(from, null);
        }
    }

    private void setMood(final Jid user, final String mood, final String text) {
        final var contact = contact(user);
        if (contact == null) {
            return;
        }
        contact.setMood(mood);
        contact.setMoodText(text);
        notifyUi();
    }

    private void setActivity(
            final Jid user, final String general, final String specific, final String text) {
        final var contact = contact(user);
        if (contact == null) {
            return;
        }
        contact.setActivity(general);
        contact.setActivitySpecific(specific);
        contact.setActivityText(text);
        notifyUi();
    }

    private void setTune(final Jid user, final String tune) {
        final var contact = contact(user);
        if (contact == null) {
            return;
        }
        contact.setTune(tune);
        notifyUi();
    }

    private Contact contact(final Jid user) {
        if (user == null || user instanceof Jid.Invalid) {
            Log.d(
                    Config.LOGTAG,
                    getAccount().getJid().asBareJid() + ": ignoring user state from invalid jid");
            return null;
        }
        return getAccount().getRoster().getContact(user.asBareJid());
    }

    private void notifyUi() {
        service.updateRosterUi();
        service.updateConversationUi();
    }

    public ListenableFuture<Void> publishMood(final String mood, final String text) {
        if (Strings.isNullOrEmpty(mood)) {
            return getManager(PepManager.class).delete(Namespace.MOOD);
        }
        return getManager(PepManager.class)
                .publishSingleton(new Mood(mood, text), NodeConfiguration.PRESENCE);
    }

    public ListenableFuture<Void> publishActivity(
            final String general, final String specific, final String text) {
        if (Strings.isNullOrEmpty(general)) {
            return getManager(PepManager.class).delete(Namespace.ACTIVITY);
        }
        return getManager(PepManager.class)
                .publishSingleton(
                        new Activity(general, specific, text), NodeConfiguration.PRESENCE);
    }

    public ListenableFuture<Void> publishTune(final String artist, final String title) {
        if (Strings.isNullOrEmpty(artist) && Strings.isNullOrEmpty(title)) {
            return getManager(PepManager.class).delete(Namespace.TUNE);
        }
        return getManager(PepManager.class)
                .publishSingleton(new Tune(artist, title), NodeConfiguration.PRESENCE);
    }
}
