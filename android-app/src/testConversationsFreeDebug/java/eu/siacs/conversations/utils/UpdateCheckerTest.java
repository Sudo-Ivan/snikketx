package eu.siacs.conversations.utils;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class UpdateCheckerTest {

    @Test
    public void newerVersionIsDetected() {
        assertTrue(UpdateChecker.isNewer("2.20.3", "2.20.2"));
        assertTrue(UpdateChecker.isNewer("2.21.0", "2.20.2"));
        assertTrue(UpdateChecker.isNewer("v2.20.3", "2.20.2"));
        assertTrue(UpdateChecker.isNewer("android-v2.20.3", "2.20.2"));
        assertTrue(UpdateChecker.isNewer("2.20.3", "2.20.2+free"));
    }

    @Test
    public void sameOrOlderVersionIsNotNewer() {
        assertFalse(UpdateChecker.isNewer("2.20.2", "2.20.2"));
        assertFalse(UpdateChecker.isNewer("2.20.1", "2.20.2"));
        assertFalse(UpdateChecker.isNewer("android-2.20.2", "2.20.2+free"));
    }

    @Test
    public void garbageInputIsNotNewer() {
        assertFalse(UpdateChecker.isNewer("latest", "2.20.2"));
        assertFalse(UpdateChecker.isNewer("", "2.20.2"));
        assertFalse(UpdateChecker.isNewer(null, "2.20.2"));
        assertFalse(UpdateChecker.isNewer("2.20.2", "not-a-version"));
    }
}
