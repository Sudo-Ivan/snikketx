package im.conversations.android.xmpp.model.activity;

import com.google.common.base.Strings;
import eu.siacs.conversations.xml.Element;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;

@XmlElement
public class Activity extends Extension {

    public Activity() {
        super(Activity.class);
    }

    public Activity(final String general, final String specific, final String text) {
        this();
        setActivity(general, specific);
        setText(text);
    }

    private Element getGeneralElement() {
        for (final Element child : getChildren()) {
            if (!"text".equals(child.getName())) {
                return child;
            }
        }
        return null;
    }

    public String getGeneral() {
        final var general = getGeneralElement();
        return general == null ? null : general.getName();
    }

    public String getSpecific() {
        final var general = getGeneralElement();
        if (general == null || general.getChildren().isEmpty()) {
            return null;
        }
        return general.getChildren().get(0).getName();
    }

    public void setActivity(final String general, final String specific) {
        if (Strings.isNullOrEmpty(general)) {
            return;
        }
        final var generalElement = this.addChild(general);
        if (!Strings.isNullOrEmpty(specific)) {
            generalElement.addChild(specific);
        }
    }

    public String getText() {
        return Strings.emptyToNull(this.findChildContent("text"));
    }

    public void setText(final String text) {
        if (!Strings.isNullOrEmpty(text)) {
            this.addChild("text").setContent(text);
        }
    }
}
