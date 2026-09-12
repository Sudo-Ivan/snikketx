# Tracking upstream

`android-app/` is a vendored fork of
[snikket-android](https://github.com/snikket-im/snikket-android), which
itself tracks [Conversations](https://codeberg.org/iNPUTmice/Conversations).
The tree here has no upstream git history, so merges happen in a scratch
clone and are then synced into this repo.

## Merging a new Conversations release

1. Clone snikket-android somewhere outside this repo:

   ```
   git clone https://github.com/snikket-im/snikket-android /tmp/snikket-merge
   cd /tmp/snikket-merge
   git remote add conversations https://codeberg.org/iNPUTmice/Conversations
   git fetch conversations --tags
   ```

2. Overlay the current SnikketX tree so our fork changes are in the clone:

   ```
   rsync -a --delete \
     --exclude .git --exclude build --exclude .gradle \
     --exclude local.properties \
     /run/media/user1/projects/snikketx/android-app/ ./
   git add -A && git commit -m "snikketx state"
   ```

3. Merge the tag and resolve conflicts:

   ```
   git merge <tag>   # e.g. 2.20.2
   ```

   Conflict rules of thumb:
   - Keep SnikketX branding (applicationId `org.snikketx.android`,
     `SnikketX` name, new launcher icons).
   - Keep Snikket-flavor features: magic-create onboarding, invite flows,
     `src/conversations/` overlays.
   - Prefer fresh upstream translations over stale ones.
   - Keep the quicksy flavor removed; delete newly added quicksy sources.

4. Build and test before syncing:

   ```
   ./gradlew assembleConversationsFreeDebug testConversationsFreeDebugUnitTest
   ```

5. Sync back and commit:

   ```
   rsync -a --delete \
     --exclude .git --exclude build --exclude .gradle \
     --exclude local.properties \
     ./ /run/media/user1/projects/snikketx/android-app/
   ```

## Fork-specific deltas to preserve

- Rebranding: applicationId, app name, icon set, `snikketx` strings.
- `UpdateChecker` (conversationsFree only) polls the server portal for a
  newer APK. The playstore flavor carries a no-op stub.
- Crash reports still go to `bugs@snikket.org` upstream, since nearly all
  bugs live in shared code. Revisit if upstream objects.
- `PRIVACY_POLICY` points at this repo's `android-app/PRIVACY.md`.
