package eu.siacs.conversations.utils;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import org.junit.Assert;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.ConscryptMode;

@RunWith(RobolectricTestRunner.class)
@ConscryptMode(ConscryptMode.Mode.OFF)
public class XEP0392HelperTest {

    private static double expectedHue(final String nick) throws Exception {
        final byte[] digest =
                MessageDigest.getInstance("SHA-1").digest(nick.getBytes(StandardCharsets.UTF_8));
        return (Byte.toUnsignedInt(digest[0]) + Byte.toUnsignedInt(digest[1]) * 256)
                / 65536.0
                * 360;
    }

    @Test
    public void deterministicForSameInput() {
        Assert.assertEquals(XEP0392Helper.rgbFromNick("alice"), XEP0392Helper.rgbFromNick("alice"));
        Assert.assertEquals(
                XEP0392Helper.rgbFromNick("user@example.com"),
                XEP0392Helper.rgbFromNick("user@example.com"));
    }

    @Test
    public void exactKnownValues() {
        Assert.assertEquals(0xFF947200, XEP0392Helper.rgbFromNick("alice"));
        Assert.assertEquals(0xFFB56100, XEP0392Helper.rgbFromNick("bob"));
        Assert.assertEquals(0xFF008580, XEP0392Helper.rgbFromNick("juliet"));
        Assert.assertEquals(0xFFAB34FF, XEP0392Helper.rgbFromNick("romeo"));
    }

    @Test
    public void nickMapsToSha1Hue() throws Exception {
        Assert.assertEquals(
                XEP0392Helper.rgbFromAngle(expectedHue("alice")),
                XEP0392Helper.rgbFromNick("alice"));
        Assert.assertEquals(
                XEP0392Helper.rgbFromAngle(expectedHue("user@example.com")),
                XEP0392Helper.rgbFromNick("user@example.com"));
    }

    @Test
    public void distinctNicksGetDistinctColors() {
        Assert.assertNotEquals(
                XEP0392Helper.rgbFromNick("alice"), XEP0392Helper.rgbFromNick("bob"));
        Assert.assertNotEquals(
                XEP0392Helper.rgbFromNick("juliet"), XEP0392Helper.rgbFromNick("romeo"));
        Assert.assertNotEquals(
                XEP0392Helper.rgbFromNick("alice"), XEP0392Helper.rgbFromNick("juliet"));
    }
}
