# SnikketX android-app work in progress

Repo: /run/media/user1/projects/snikketx/android-app (Conversations 2.20.2 / snikket-android fork)
Build: JAVA_HOME=/usr/lib/jvm/java-21-openjdk ./gradlew assembleConversationsFreeDebug
APKs out: build/outputs/apk/conversationsFree/debug/
Format: ./gradlew spotlessApply (googleJavaFormat aosp; build runs spotlessCheck)

## Requested tasks

1. FIX freeze after initial app start on "fetching history from server" (MAM catchup)
2. Change push server default to push.quad4.io
3. Add advanced setting to edit crash reports server
4. Option: save attachments to encrypted app storage vs phone; auto-deletion must cover it
5. Custom app PIN/password + duress support: wipe, decoy account (hide main), fake account with fake messages
Constraint: only edit android-app.

## What was implemented

### Task 1 - freeze fix
- XmppConnectionService.restoreFromDatabase(): runnable now wraps restore in try/catch/finally,
  restoredFromDatabaseLatch.countDown() moved into finally so a restore exception can no longer
  leave the latch at 1 (which wedged bind/SASL2/quickstart await()s forever). Exception logged.
- MessageArchiveManager.catchup(): in-flight queries are now killed via kill() (snapshot under
  lock, then finalizeQuery + UI update per query) instead of queries.clear() which dropped them
  silently and could leave the "fetching history" UI stuck.
- MessageArchiveManager.query(): execute() (IQ send, synchronizes on connection) moved outside
  the synchronized(queries) block; adds still happen under the lock before sending.

### Task 2 - push server
- Already done in 426771d: res/values/defaults.xml default_push_server = push.quad4.io.

### Task 3 - crash report server
- New pref crash_report_dsn (EditTextPreference in preferences_privacy.xml, depends on
  send_crash_reports, SimpleSummaryProvider shows current value).
- AppSettings.getCrashReportDsn(); SentryHelper.init applies it via options.setDsn() when
  non-empty (empty = manifest DSN). Preference listener re-inits (Sentry.close + init) on change.
- PrivacySettingsFragment validates input: empty or http(s) URI with host and path.

### Task 4 - attachment storage
- New ListPreference attachment_storage ("private" = app internal FBE storage, "shared" =
  public dirs) in preferences_attachments.xml replacing the old use_shared_storage switch.
- AppSettings.isUseSharedStorage() migrates legacy use_shared_storage boolean once into
  attachment_storage, then reads the string pref. All call sites go through it.
- Deletion verified: internal files are stored with absolute paths + sharedStorage=0, so
  expireOldMessages / deleteMessagesInConversation -> filterUnusedFiles -> deleteFiles covers
  them. Relative paths only exist in legacy rows which resolve to external dirs (kept, matching
  shared-storage semantics). XEP-0466 expireEphemeralMessages covers both via
  getLegacyFileForFilename + deleteFileIfOrphaned.

### Task 5 - PIN + duress
- AppLockManager: PBKDF2 credential store (salt:hash base64), session-scoped duress state
  (activateDuress / isDuressActive / isDuressFake / getDuressAccountUuid / isHidden(Account)),
  unlock() resets duress.
- LockActivity + activity_lock.xml: PIN/password field shown when app_lock_credential set,
  biometrics remain via secondary button. Primary PIN unlocks; duress PIN dispatches action.
- Security settings: "App PIN or password" and "Duress PIN" set/remove dialogs (min 4 chars,
  pins must differ), duress_action list (none/wipe/decoy/fake), duress_account account picker
  (enabled only for decoy). app_lock switch now also allows enabling when a PIN is set even
  without a device secure lockscreen.
- Decoy: getAccounts() returns only decoy account during duress; populateWithOrderedConversations
  uses visibleConversations() (decoy convos only). getConversations() itself stays unfiltered
  because MessageParser routes via find() - filtering it would break incoming messages.
- Fake: getOrCreateDuressAccount() builds a detached Account (jordan@<domain>) with a real but
  never-connected XmppConnection so getManager() calls from the UI work; three canned
  conversations (Sam/Mom/Work) with scripted read messages.
- Wipe: XmppConnectionService.wipeAllUserData() cancels notifications, closes + deletes the
  history DB, deletes filesDir/cacheDir recursively, clears SharedPreferences, kills process.
- NotificationService: push/pushFromBacklog/pushFailedDelivery skip hidden accounts.
- ConversationsOverviewFragment folder dialog skips hidden conversations.

## Caveats / follow-ups

- In decoy mode the hidden account stays connected; stanza routing still works, only UI +
  notifications are filtered. Unread badge may still count hidden conversations.
- Fake conversations can be opened but sending from them is untested (no device run yet).
- Legacy relative file paths (pre-migration external files) are still never deleted by
  auto-deletion - intentional, they live on shared storage.

## TODO state (mirror of task list)

1. [done] explore
2. [done] fix freeze (latch try/finally + catchup kill + execute-outside-lock)
3. [done] push default (already push.quad4.io)
4. [done] crash report server setting
5. [done] encrypted app storage option for attachments + deletion
6. [done] PIN + duress (wipe/decoy/fake)
7. [done] rebuild APK (assembleConversationsFreeDebug OK, universal + per-ABI debug APKs)
