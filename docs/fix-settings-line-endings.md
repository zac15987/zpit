# Fix: `.claude/settings.json` Line Ending Drift on Windows

## Problem

Zpit deploys `.claude/settings.json` to target projects using Go's `json.MarshalIndent()`，which always produces LF (`\n`) line endings regardless of OS. On Windows, editors and `git diff` may flag this file as having "wrong" line endings, or git may stage spurious CRLF ↔ LF changes depending on `core.autocrlf` setting.

If multiple developers work on the same project across Windows and Linux/WSL, different `autocrlf` settings can cause the file to flip between LF and CRLF on each commit — producing noise diffs and merge conflicts with no actual content change.

## Why Not "Write CRLF on Windows"?

Since `.claude/settings.json` is a **git-tracked** file (not in `.gitignore`), writing platform-specific line endings would cause cross-platform conflicts:

- Developer A (Windows) commits CRLF
- Developer B (Linux) runs zpit → overwrites with LF → git sees a diff
- Ping-pong on every deploy

The correct fix is to **always write LF** (which Go already does) and tell git to enforce this via `.gitattributes`.

## Fix

Add a `.gitattributes` rule to each target project so git normalizes this file to LF in the repository, regardless of platform:

```
.claude/settings.json text eol=lf
```

---

## For Already Deployed Projects

If `.claude/settings.json` was already committed with CRLF (or mixed) line endings, adding `.gitattributes` alone won't retroactively fix the index. You need to refresh the git index.

### Steps

1. **Add the `.gitattributes` rule**

   If the project doesn't have a `.gitattributes` file, create one. If it already exists, append the rule.

   ```bash
   echo ".claude/settings.json text eol=lf" >> .gitattributes
   ```

2. **Refresh the git index to apply the new rule**

   ```bash
   git rm --cached .claude/settings.json
   git add .claude/settings.json
   ```

   This re-adds the file with LF normalization applied. `git diff --cached` should show the line ending conversion (if any).

3. **Commit**

   ```bash
   git add .gitattributes
   git commit -m "fix: add .gitattributes to normalize settings.json line endings"
   ```

4. **Verify**

   ```bash
   git diff HEAD~1 --stat
   ```

   After this commit, all future checkouts will produce LF for this file in the working tree (even on Windows), and editors that respect `.gitattributes` will preserve LF on save.

### If Other Developers Have Local CRLF Copies

After they pull the commit above, they should refresh their working tree:

```bash
git checkout -- .claude/settings.json
```

Or, to refresh all files affected by `.gitattributes` changes:

```bash
git rm --cached -r .
git reset --hard
```

> **Warning:** `git reset --hard` discards uncommitted changes. Stash or commit first.

---

## For Not Yet Deployed Projects

No action needed. Zpit already writes LF natively (Go behavior). To prevent future drift, you have two options:

### Option A: Pre-create `.gitattributes` (Recommended)

Before the first `zpit` deploy, add the rule to the project:

```bash
echo ".claude/settings.json text eol=lf" >> .gitattributes
git add .gitattributes
git commit -m "chore: add .gitattributes for consistent line endings"
```

This way, the first deploy will already be properly normalized.

### Option B: Do Nothing

Git's default behavior with `core.autocrlf=true` (Windows default) will convert LF to CRLF on checkout and back to LF on commit. This works but may cause confusing diffs in editors. Option A is cleaner.

---

## Reference

- Go's `json.MarshalIndent()` always outputs LF — this is by design and correct for cross-platform consistency.
- Zpit's own repo uses `.gitattributes` with `.claude/settings.json text eol=lf` for the same reason.
- Affected code: `internal/worktree/hooks.go` → `mergeSettingsHooks()` (line 287-295).
