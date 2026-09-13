# wikictl

[日本語版](README.ja.md)

wikictl is a command-line tool for a Markdown wiki that lives in a Git repository on a Git host (GitHub, GitLab, Gitea, ...). It is built for a knowledge base shared between AI agents and people: agents search and write pages from the shell, people read and edit the same pages in the Git host's web UI or in Obsidian.

wikictl runs no server, keeps no index and uses no working tree. It fetches into a bare mirror and reads with `git grep`, `git cat-file` and `git log`; it writes by building a commit with git plumbing and pushing it with `--force-with-lease`. The wiki does not depend on wikictl: it is plain Markdown that any editor can handle.

## Install

Requires `git` on `PATH`. Linux and macOS are supported.

Install the latest release into `~/.local/bin`:

    curl -fsSL https://raw.githubusercontent.com/roamer7038/wikictl/main/install.sh | sh

The script picks the binary for your OS and architecture, verifies its checksum and warns if `~/.local/bin` is not on your `PATH`. Set `WIKICTL_VERSION` to install a specific tag and `WIKICTL_INSTALL_DIR` to change the directory.

Or build from source with Go:

    go install github.com/roamer7038/wikictl/cmd/wikictl@latest

Binaries for Linux and macOS (x86_64 and arm64) and their checksums are on the [releases page](https://github.com/roamer7038/wikictl/releases).

## Quick start

1. Create an empty repository on your Git host and make sure `git push` to it works without prompting (credential helper or SSH agent).
2. Write `~/.config/wikictl/config.yaml`:

       repo: git@github.com:you/wiki.git
       author:
         name: claude-code@laptop
         email: claude-code@laptop.invalid

3. Create the initial pages, then write and read:

       wikictl init
       printf -- '---\nsummary: A push with --force-with-lease is rejected unless the remote ref still has the expected sha\n---\n# What does --force-with-lease guarantee?\n\nBody.\n\n## Links\n- part_of: [index](index.md)\n' \
         | wikictl put global/git-force-with-lease.md
       wikictl search lease
       wikictl get global/git-force-with-lease.md
       wikictl lint

Add `--json` to any command for machine-readable output.

## Commands

| Command | Purpose |
|---|---|
| `init` | Create `README.md` and `global/index.md` in an empty repository |
| `search <word>...` | Pages containing all of the words (`--any` for any of them) |
| `get <path>` | One page with its sha, frontmatter, body, links and backlinks |
| `ls` | List pages (`--type`, `--tag`, `--all`) |
| `put <path> < content` | Create or replace a page; `--base <sha>` when updating |
| `mv <path> <newpath>` | Move or rename a page and rewrite links to it |
| `mv <dir>/ <newdir>/` | Move every page under a directory |
| `rm <path>` | Delete a page |
| `lint [<path>...]` | Report format violations |
| `context` | Show the resolved configuration and search directories |

`wikictl help <command>` describes each command and its flags. `wikictl version` prints the version.

Global flags, accepted before or after the command: `--json`, `--dirs a,b`, `--config <path>`, `--profile <name>`, `--no-fetch` (skip the fetch that normally precedes every command).

### Updating a page without overwriting someone else's change

`get` prints the blob `sha` of the page. Pass it back with `put --base`. If the page changed in between, `put` exits with code 3 and prints the current content and `sha`: re-read, reapply your change and `put` again. Writing to an existing page without `--base` is also a conflict (exit code 3). No merge state is ever created.

### Where commands look

By default a command searches up to three directories: `global/`, `projects/<name>/` where `<name>` comes from the `origin` remote of the current directory (skipped outside a git repository), and `machines/<name>/` where `<name>` is the hostname. `--dirs a,b` overrides the list and `wikictl context` shows it.

## Configuration

`~/.config/wikictl/config.yaml` (`$XDG_CONFIG_HOME` is honoured), or the file named by `$WIKICTL_CONFIG` or `--config <path>`:

| Key | Required | Meaning |
|---|---|---|
| `repo` | yes | URL of the wiki repository; may be set in a profile instead |
| `branch` | no | Branch to use; taken from the remote HEAD when omitted |
| `author.name`, `author.email` | no | Commit author; falls back to `git config user.name` and `user.email` |
| `machine` | no | Name for `machines/<name>/`; defaults to the hostname up to the first `.` |
| `dirs` | no | Fixed list of search directories instead of the default three |
| `projects` | no | Map from remote name to directory name under `projects/` |
| `profiles` | no | Named profiles that override the keys above; see below |
| `default_profile` | no | Profile to use when no other rule selects one |

An unknown key, such as a misspelt `default_profle` or `match.remote`, is a configuration error (exit code 2) that names the key, so a typo never silently selects another wiki.

### Profiles

Profiles keep several wikis, such as a personal one and a work one, in one file. The top-level keys are defaults; a profile overrides them:

```yaml
author:
  name: claude-code@laptop
default_profile: personal
profiles:
  personal:
    repo: git@github.com:you/wiki.git
    author:
      email: you@example.invalid
  work:
    repo: git@github.example.com:team/wiki.git
    author:
      email: you@company.example
    match:
      remotes: ["github.example.com/team/*"]
      paths: ["~/work"]
```

The profile is chosen by the first of these that applies:

1. `--profile <name>`
2. `$WIKICTL_PROFILE`
3. `match`: `remotes` are globs over the `origin` remote of the current directory, written as `host/path` in lowercase without scheme, user, port and `.git`, so SSH and HTTPS URLs of the same repository match the same pattern. A `*` in the middle matches one path element; a trailing `/*` matches every path below it. For example, `gitlab.example.com/team/*` matches `team/app` and the subgroup repository `team/sub/app`, but not `team` itself, while `gitlab.example.com/*/app` matches `team/app` but not `team/sub/app`. `paths` are directories given as absolute paths or paths starting with `~`; the current directory or any directory below one matches. If more than one profile matches, the command fails with exit code 2.
4. `default_profile`
5. No profile: only the top-level keys are used.

An unknown profile name is an error (exit code 2). In a profile, `author.name` and `author.email` override separately, `dirs` and `projects` replace the top-level values, and `branch` is not inherited when the profile sets `repo`. `wikictl context` shows the selected profile, how it was selected and the repository.

The mirror lives under `~/.cache/wikictl/` (or `$XDG_CACHE_HOME/wikictl/`). Delete it if it ever breaks; the next command recreates it.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | error, for example a missing page |
| 2 | usage or configuration error |
| 3 | conflict: the page changed since it was read |
| 4 | the page violates the wiki format (missing or invalid frontmatter, no summary, bad path) |
| 5 | a git command failed |

With `--json`, errors are printed as `{"error": "<kind>", "message": "..."}` where `<kind>` is `error`, `usage`, `conflict`, `invalid` or `git`.

## Page format

A page is a Markdown file in a subdirectory (never at the root) whose frontmatter has a one-line `summary`:

```markdown
---
summary: What the page answers, in one sentence
type: concept
---
# Title

Body. Link to other pages with relative paths: [index](index.md).

## Links
- part_of: [index](index.md)
- cites: https://example.com/spec | what this source supports
```

- File and directory names match `^[a-z0-9][a-z0-9-]*$`; pages end in `.md`.
- Optional frontmatter keys: `type`, `status` (`deprecated` hides the page from `search` and `ls`), `tags`, `aliases`, `review_after`.
- The `## Links` section, when present, is the last heading. Each line is `- <type>: <target> | <note>`; `<target>` is a relative path or a URL.
- Code fences are never interpreted; a `## Links` heading inside one does not start the section.

## Development

    go test ./...
    go build -o wikictl ./cmd/wikictl

## License

[MIT](LICENSE)
