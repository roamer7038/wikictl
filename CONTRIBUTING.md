# Contributing

[日本語版](CONTRIBUTING.ja.md)

These are the rules for branches, pull requests and releases of wikictl. For building and testing, see the Development section of the README.

## Overview

```
(Issue) ──> branch (from main) ──> PR (base: main) ──> squash merge ──> release by tag
```

- main is always kept releasable.
- Do not create long-lived or integration branches (such as `release/*`).

## Issues

- Changes that need discussion or a decision (bug reports, new features, changes in behavior) start from an issue. For a bug, write the steps to reproduce it; for a proposal, write its purpose.
- Small changes such as typo fixes, CI adjustments and dependency updates may be sent as a PR without an issue.

## Branches

- Name a branch `<type>/<short-name>`. If there is a matching issue, use `<type>/<issue-number>-<short-name>` (example: `fix/24-push-retry`). `<type>` is the same as the type of the commit message.
- Create the branch from main, and use one branch per PR.
- Branches created by Dependabot do not follow this naming.

## Pull requests

- The base is always main.
- A PR has a single purpose. If there is a matching issue, write `Closes #<issue-number>` in the description (`Refs #<issue-number>` if the PR covers only part of it).
- The description has a "Compatibility impact" section (either this heading or its Japanese equivalent, 互換性への影響, is fine).
  - A compatibility impact is a change in the result of existing correct usage: command output, JSON keys, exit codes, the meaning of a setting, or default behavior. If there is one, write what changes and how, and what existing users need to do (example: renaming a key in the `--json` output).
  - A fix that only makes input or settings that used to fail work as expected is not a compatibility impact. In that case, write "None" with a one-sentence reason (example: `put` now works where it failed with a `repo` setting written as a relative path). Also write "None" when no result changes.
- A PR that changes behavior includes updates to the help text (`wikictl help` and `wikictl help <command>`). Details such as command flags, edge cases and output formats are written only in the help text.
- Update the README (English and Japanese) only when what it covers changes (introduction, wiki layout, command list, configuration, exit codes, mirror location, development), and keep the two versions in agreement.
- When changing this document, keep its English and Japanese versions in agreement as well.
- If the branch conflicts with main, merge main into the branch to resolve it. Because PRs are squash merged, the branch history does not remain in main, and unlike a rebase no force push is needed.

## Commit messages and PR titles

PRs are squash merged, so the PR title becomes the commit message on main and is also used in the release notes.

- Use the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) format, with the description in English or Japanese (example: `fix: retry when locking the ref fails on push`).
- The type is one of `feat`, `fix`, `docs`, `refactor`, `test`, `ci` or `chore`.
- Show a compatibility impact in the "Compatibility impact" section of the PR description, not in the title.

## Use of AI tools

Even when the work is produced with AI tools (such as coding agents), do not put the tool's signature or information about how it was generated in commit messages, PR descriptions and comments, or issues.

- Examples: `Co-Authored-By:` lines, text such as "Generated with ...", URLs of sessions or logs

## Review and merge

- Merges into main are done by a maintainer. AI tools go as far as creating, reviewing and revising PRs; they do not approve or merge them.
- main is protected by a ruleset, and changes cannot be merged into it unless:
  - the change goes through a PR
  - the required CI check `test` (`.github/workflows/test.yml`) has passed
  - all review threads are resolved
  - the merge method is squash (the history of main stays linear)
- The ruleset does not require a branch to contain the latest main before merging. So when parallel PRs are merged one after another, tests can fail on main even though each PR's CI passed. After another PR touching the same files or test expectations has been merged, merge main into the branch and get CI to pass before merging. After merging, check the CI on main.
- Branches are deleted automatically after merging.

## Versions

wikictl follows [Semantic Versioning](https://semver.org/). While on 0.x, versions are raised as follows.

| PRs in the release | Version to bump |
|---|---|
| Includes a PR with a compatibility impact, or a new feature (`feat`) | minor (0.2.0 → 0.3.0) |
| Only other PRs | patch (0.2.0 → 0.2.1) |

The scope of a compatibility impact follows the definition in the Pull requests section. A fix that only makes input or settings that used to fail work is released as a patch.

## Releases

1. Confirm that CI has passed on the latest commit of main.
2. Put an annotated tag on that commit and push it.

   ```sh
   git tag -a v0.3.0 -m "wikictl v0.3.0"
   git push origin v0.3.0
   ```

3. Pushing the tag runs the release workflow in GitHub Actions, and GoReleaser publishes the binaries and `checksums.txt` to a GitHub Release.
4. Add to the release notes a section that collects the "Compatibility impact" of each PR, together with migration steps.

Tags starting with `v` are protected by a ruleset and cannot be deleted or moved. A faulty release is fixed by releasing a new version.
