package eu.siacs.conversations.entities;

import eu.siacs.conversations.xmpp.Jid;
import org.junit.Assert;
import org.junit.Test;

public class EphemeralMessageTest {

    private static Message newMessage() {
        final Account account = new Account(Jid.of("user@example.com"), "secret");
        final Conversation conversation =
                new Conversation(
                        "peer", account, Jid.of("peer@example.com"), Conversation.MODE_SINGLE);
        return new Message(conversation, "hello", Message.ENCRYPTION_NONE);
    }

    @Test
    public void zeroExpireMeansNeverExpires() {
        final Message message = newMessage();
        Assert.assertEquals(0, message.getExpire());
        Assert.assertFalse(message.isEphemeral());
        message.markRead();
        Assert.assertEquals(0, message.getExpire());
    }

    @Test
    public void armedTimerHasNoDeadlineBeforeRead() {
        final Message message = newMessage();
        message.setExpireAfterRead(3600);
        Assert.assertEquals(-3600, message.getExpire());
        Assert.assertTrue(message.isEphemeral());
    }

    @Test
    public void armedTimerStartsCountdownOnRead() {
        final Message message = newMessage();
        message.setExpireAfterRead(3600);
        final long before = System.currentTimeMillis();
        message.markRead();
        final long after = System.currentTimeMillis();
        Assert.assertTrue(message.getExpire() >= before + 3600_000L);
        Assert.assertTrue(message.getExpire() <= after + 3600_000L);
    }

    @Test
    public void absoluteDeadlineSurvivesRead() {
        final Message message = newMessage();
        final long deadline = System.currentTimeMillis() + 60_000L;
        message.setExpire(deadline);
        message.markRead();
        Assert.assertEquals(deadline, message.getExpire());
    }
}
