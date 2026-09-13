package eu.siacs.conversations.utils;

import android.content.Context;
import com.google.common.base.Strings;
import eu.siacs.conversations.R;
import eu.siacs.conversations.entities.Contact;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

public final class UserStates {

    private UserStates() {
        throw new AssertionError("Do not instantiate me");
    }

    public static List<String> details(final Context context, final Contact contact) {
        final var lines = new ArrayList<String>();
        final var mood = moodLabel(context, contact.getMood());
        if (mood != null) {
            var line = mood;
            if (!Strings.isNullOrEmpty(contact.getMoodText())) {
                line = String.format("%s: %s", line, contact.getMoodText());
            }
            lines.add(context.getString(R.string.contact_mood, line));
        }
        final var activity =
                activityLabel(context, contact.getActivity(), contact.getActivitySpecific());
        if (activity != null) {
            var line = activity;
            if (!Strings.isNullOrEmpty(contact.getActivityText())) {
                line = String.format("%s: %s", line, contact.getActivityText());
            }
            lines.add(context.getString(R.string.contact_activity, line));
        }
        final var tune = contact.getTune();
        if (!Strings.isNullOrEmpty(tune)) {
            lines.add(context.getString(R.string.contact_tune, tune));
        }
        return lines;
    }

    public static String inlineSummary(final Context context, final Contact contact) {
        final var parts = new ArrayList<String>();
        final var mood = moodLabel(context, contact.getMood());
        if (mood != null) {
            parts.add(
                    context.getString(
                            R.string.feeling_mood, mood.toLowerCase(Locale.getDefault())));
        }
        final var activity =
                activityLabel(context, contact.getActivity(), contact.getActivitySpecific());
        if (activity != null) {
            parts.add(activity.toLowerCase(Locale.getDefault()));
        }
        final var tune = contact.getTune();
        if (!Strings.isNullOrEmpty(tune)) {
            parts.add(context.getString(R.string.contact_tune, tune));
        }
        if (parts.isEmpty()) {
            return null;
        }
        return join(parts);
    }

    private static String join(final List<String> parts) {
        final var builder = new StringBuilder();
        for (final var part : parts) {
            if (builder.length() > 0) {
                builder.append(", ");
            }
            builder.append(part);
        }
        return builder.toString();
    }

    private static String moodLabel(final Context context, final String value) {
        return label(context, value, R.array.mood_values, R.array.mood_names);
    }

    private static String activityLabel(
            final Context context, final String general, final String specific) {
        final var label = label(context, general, R.array.activity_values, R.array.activity_names);
        if (label == null) {
            return null;
        }
        if (Strings.isNullOrEmpty(specific)) {
            return label;
        }
        return String.format("%s (%s)", label, specific.replace('_', ' '));
    }

    private static String label(
            final Context context,
            final String value,
            final int valuesResource,
            final int namesResource) {
        if (Strings.isNullOrEmpty(value)) {
            return null;
        }
        final String[] values = context.getResources().getStringArray(valuesResource);
        final String[] names = context.getResources().getStringArray(namesResource);
        for (int i = 0; i < values.length && i < names.length; ++i) {
            if (value.equals(values[i])) {
                return names[i];
            }
        }
        return value.replace('_', ' ');
    }
}
