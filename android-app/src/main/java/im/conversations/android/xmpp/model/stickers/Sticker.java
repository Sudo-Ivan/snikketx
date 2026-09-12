package im.conversations.android.xmpp.model.stickers;

import com.google.common.base.Strings;
import im.conversations.android.annotation.XmlElement;
import im.conversations.android.xmpp.model.Extension;

@XmlElement
public class Sticker extends Extension {

    public Sticker() {
        super(Sticker.class);
    }

    public String getPack() {
        return Strings.emptyToNull(this.getAttribute("pack"));
    }

    public void setPack(final String pack) {
        if (!Strings.isNullOrEmpty(pack)) {
            this.setAttribute("pack", pack);
        }
    }

    public String getJid() {
        return Strings.emptyToNull(this.getAttribute("jid"));
    }

    public String getNode() {
        return Strings.emptyToNull(this.getAttribute("node"));
    }
}
