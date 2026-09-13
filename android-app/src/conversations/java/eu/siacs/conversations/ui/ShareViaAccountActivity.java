package eu.siacs.conversations.ui;

import android.annotation.SuppressLint;
import android.os.Bundle;
import android.view.View;
import androidx.databinding.DataBindingUtil;
import androidx.recyclerview.widget.LinearLayoutManager;
import eu.siacs.conversations.R;
import eu.siacs.conversations.databinding.ActivityShareViaAccountBinding;
import eu.siacs.conversations.entities.Account;
import eu.siacs.conversations.entities.Conversation;
import eu.siacs.conversations.services.XmppConnectionService;
import eu.siacs.conversations.ui.adapter.AccountAdapter;
import eu.siacs.conversations.ui.adapter.ConversationAdapter;
import eu.siacs.conversations.xmpp.Jid;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Share destination picker for {@code xmpp:?message} style shares.
 *
 * <p>Two sections are shown: the most recently active conversations and the list of accounts.
 *
 * <p>Tapping a recent chat delivers the shared content into that conversation: the shared body
 * (prefixed with the shared contact as an {@code xmpp:} link when a different contact was
 * addressed) is appended to the message input as a draft.
 *
 * <p>Tapping an account keeps the previous behavior: open or create a conversation with the shared
 * contact on that account and prefill the draft with the shared body.
 *
 * <p>The single account fast path is kept only when there is nothing else to pick from, ie when
 * there is exactly one account and no recent conversation to share into.
 */
public class ShareViaAccountActivity extends XmppActivity
        implements XmppConnectionService.OnConversationUpdate,
                XmppConnectionService.OnAccountUpdate {

    public static final String EXTRA_CONTACT = "contact";
    public static final String EXTRA_BODY = "body";

    private static final int MAX_RECENT_CONVERSATIONS = 5;

    protected final List<Account> accountList = new ArrayList<>();
    private final List<Conversation> recentConversations = new ArrayList<>();

    private ActivityShareViaAccountBinding binding;
    private AccountAdapter mAccountAdapter;
    private ConversationAdapter mRecentAdapter;

    @Override
    public void onConversationUpdate() {
        refreshUi();
    }

    @Override
    public void onAccountUpdate() {
        refreshUi();
    }

    @Override
    @SuppressLint("NotifyDataSetChanged")
    protected void refreshUiReal() {
        synchronized (this.accountList) {
            accountList.clear();
            accountList.addAll(xmppConnectionService.getAccounts());
        }
        populateRecentConversations();
        rebuildAccountRows();
        mRecentAdapter.notifyDataSetChanged();
        final boolean hasRecents = !recentConversations.isEmpty();
        binding.recentChatsHeader.setVisibility(hasRecents ? View.VISIBLE : View.GONE);
        binding.recentChatsList.setVisibility(hasRecents ? View.VISIBLE : View.GONE);
        binding.accountsHeader.setVisibility(accountList.isEmpty() ? View.GONE : View.VISIBLE);
    }

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        binding = DataBindingUtil.setContentView(this, R.layout.activity_share_via_account);
        Activities.setStatusAndNavigationBarColors(this, binding.getRoot());
        setSupportActionBar(binding.toolbar);
        configureActionBar(getSupportActionBar());

        mRecentAdapter = new ConversationAdapter(this, recentConversations);
        mRecentAdapter.setConversationClickListener(
                (view, conversation) -> onRecentConversationSelected(conversation));
        binding.recentChatsList.setLayoutManager(new LinearLayoutManager(this));
        binding.recentChatsList.setAdapter(mRecentAdapter);

        this.mAccountAdapter = new AccountAdapter(this, accountList, false);
    }

    @Override
    protected void onBackendConnected() {
        final List<Account> accounts = xmppConnectionService.getAccounts();
        populateRecentConversations();
        if (accounts.size() == 1 && recentConversations.isEmpty()) {
            // fast path: nothing to choose, forward straight to the shared contact
            onAccountSelected(accounts.get(0));
        } else {
            refreshUiReal();
        }
    }

    private void populateRecentConversations() {
        final List<Conversation> sorted = new ArrayList<>(xmppConnectionService.getConversations());
        Collections.sort(
                sorted,
                (a, b) ->
                        Long.compare(
                                b.getLatestMessage().getTimeSent(),
                                a.getLatestMessage().getTimeSent()));
        recentConversations.clear();
        recentConversations.addAll(
                sorted.subList(0, Math.min(MAX_RECENT_CONVERSATIONS, sorted.size())));
    }

    private void rebuildAccountRows() {
        binding.accountList.removeAllViews();
        synchronized (this.accountList) {
            for (int i = 0; i < accountList.size(); ++i) {
                final Account account = accountList.get(i);
                final View row = mAccountAdapter.getView(i, null, binding.accountList);
                row.setOnClickListener(v -> onAccountSelected(account));
                binding.accountList.addView(row);
            }
        }
    }

    private void onAccountSelected(final Account account) {
        final String body = getIntent().getStringExtra(EXTRA_BODY);
        final String contactExtra = getIntent().getStringExtra(EXTRA_CONTACT);

        if (contactExtra == null) {
            finish();
            return;
        }
        try {
            final Jid contact = Jid.of(contactExtra);
            final Conversation conversation =
                    xmppConnectionService.findOrCreateConversation(account, contact, false, false);
            switchToConversation(conversation, body);
        } catch (IllegalArgumentException e) {
            // ignore error
        }

        finish();
    }

    private void onRecentConversationSelected(final Conversation conversation) {
        String text = getIntent().getStringExtra(EXTRA_BODY);
        final String contactExtra = getIntent().getStringExtra(EXTRA_CONTACT);
        if (contactExtra != null) {
            try {
                final Jid contact = Jid.of(contactExtra);
                if (!conversation.getAddress().asBareJid().equals(contact.asBareJid())) {
                    // sharing into a different conversation: pass the shared contact along as a
                    // tappable xmpp link
                    final String contactLink = "xmpp:" + contact.asBareJid();
                    text = text == null ? contactLink : contactLink + "\n" + text;
                }
            } catch (final IllegalArgumentException e) {
                // treat a malformed contact extra as absent
            }
        }
        if (text == null) {
            switchToConversation(conversation);
        } else {
            switchToConversation(conversation, text);
        }
    }
}
