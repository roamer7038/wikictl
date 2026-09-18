# wikictl

[日本語版](README.ja.md)

wikictl is a command-line tool for a Markdown wiki kept in a Git repository. It is built for a knowledge base shared between AI agents and people: agents search and write pages from the shell, and people read and edit the same pages in a Git host's web UI or in a clone opened with an editor such as Obsidian. People who edit in a clone commit and push as usual; their changes and wikictl's meet in the repository.

wikictl runs no server, keeps no index and uses no working tree. The wiki does not depend on wikictl: it is plain Markdown that any editor can handle.

```mermaid
flowchart LR
  agent["AI agent or shell"] -->|wikictl| mirror["bare mirror<br>~/.cache/wikictl/"]
  mirror <-->|"fetch / push"| repo[("wiki repository")]
  person["person"] <-->|"web UI, or clone and push"| repo
```

## Requirements

- Linux or macOS.
- `git` on `PATH`.
- Fetch and push access to the wiki repository without any prompt (a credential helper, an SSH agent or file access for a local path). Every command fetches first, so this applies to reading as well, unless `fetch_ttl` skips it (see the Mirror section).
- Direct pushes to the wiki branch. Branch protection that requires pull requests blocks every write.
- For the install script: `curl`, and `sha256sum` or `shasum`. For building from source: Go 1.26.5 or later.

The repository can be on a Git host, on a server reachable over SSH or in a local directory; `repo` in the configuration is passed to git as it is:

| Location | Example of `repo` |
|---|---|
| Git host | `git@github.com:you/wiki.git` |
| Server over SSH | `ssh://you@server.example/srv/git/wiki.git` |
| Local directory | `/home/you/wiki.git` |

Do not write a password or token in `repo`: git saves the URL in the mirror as it is. Use an HTTPS URL together with a git credential helper, or an SSH URL with an SSH agent.

On a server or in a local directory, create the repository with `git init --bare -b main ~/wiki.git`. In an empty repository wikictl writes to `main`, so if the repository's HEAD points to another branch, such as `master`, set `branch` in the configuration to that branch.

## Install

Install the latest release into `~/.local/bin`:

    curl -fsSL https://raw.githubusercontent.com/roamer7038/wikictl/main/install.sh | sh

The script picks the binary for your OS and architecture. Set `WIKICTL_VERSION` to install a specific tag, such as `v0.2.0`, and `WIKICTL_INSTALL_DIR` to change the directory. Or build from source:

    go install github.com/roamer7038/wikictl/cmd/wikictl@latest

Binaries for Linux and macOS (x86_64 and arm64) are on the [releases page](https://github.com/roamer7038/wikictl/releases).

## Quick start

1. Create an empty wiki repository and make sure `git push` to it works without prompting.
2. Write `~/.config/wikictl/config.yaml`:

       repo: git@github.com:you/wiki.git
       author:
         name: claude-code@laptop
         email: claude-code@laptop.invalid

3. Check the configuration, then write and read a page. The first `put` creates the branch in the empty repository:

       wikictl context
       printf -- '---\nsummary: A push with --force-with-lease is rejected unless the remote ref still has the expected sha\n---\n# What does --force-with-lease guarantee?\n\nBody.\n' \
         | wikictl put global/git-force-with-lease.md
       wikictl grep lease
       wikictl cat global/git-force-with-lease.md
       wikictl lint

## Wiki layout

wikictl gives no meaning to directory names: a page can be placed in any directory, and commands whose paths are optional (`grep`, `links`, `ls`, `find`, `lint`, `tree`) read the whole wiki when the paths are omitted. wikictl does not choose directories from the current directory.

For a new wiki, one layout is to make each top-level directory a scope that answers "where is this knowledge valid?" and to place a page in the narrowest scope that fits:

| Directory | Valid for |
|---|---|
| `global/` | everyone |
| `personal/` | only this user, on every machine and in every project |
| `projects/<name>/` | one project |
| `machines/<name>/` | one execution environment |

In this layout, `personal/` holds facts that an agent looks up when they become relevant; rules for every conversation belong in the agent's standing instructions, such as `CLAUDE.md`. In a wiki shared by several people, everyone reads the same `personal/`, so do not use it there.

A page is a Markdown file, at the wiki root or in a directory, whose path has no component starting with a dot. Its frontmatter should have a one-line `summary`, and relations to other pages go in a `## Links` section at the end:

```markdown
---
summary: What the page answers, in one sentence
type: concept
---
# Title

Body. Link to other pages with relative paths: [push](git-push.md).

## Links
- see_also: [push](git-push.md)
- cites: https://example.com/spec | what this source supports
```

- Frontmatter keys that wikictl interprets: `summary` (or `description`), `type`, `tags`, `aliases` and `status: deprecated`, which hides the page from `ls` and `tree`.
- Write links to pages as `[text](path)`, so that `mv` can rewrite them.
- Names made of lowercase ASCII letters, digits and hyphens are recommended.

`wikictl help lint` describes the format rules in detail.

## Commands

| Command | Purpose |
|---|---|
| `grep <pattern> [<path>...]` | Print the lines that match a pattern |
| `cat <path>...` | Print files as stored |
| `stat <path>...` | Show the sha, last update and attributes of files |
| `log [<path>...]` | Show the commits that changed files |
| `links [<path>...]` | List the links in pages and to them |
| `ls [<path>...]` | List the entries of directories |
| `find [<path>...] [<expression>]` | Find files and directories by name, type, update time or frontmatter |
| `put <path> < content` | Create or replace a file from standard input |
| `edit <path>` | Edit a file in an editor and commit it |
| `mv <src>... <dst>` | Move or rename files and directories, rewriting links |
| `rm <path>...` | Delete files or directories |
| `lint [<path>...]` | Report pages that violate the wiki format |
| `tree [<dir>...]` | Show files and directories as a tree |
| `context` | Show the resolved configuration |
| `help [<command>]` | Show help for a command |
| `version` | Print the version |

`wikictl help <command>` describes the flags, the behavior and the JSON output of each command. Add `--json` to any command except `help` for machine-readable output.

Paths printed one per line, such as those of `grep -l`, are passed to another command with `| tr '\n' '\0' | xargs -0 -r wikictl ls -lt`: names holding quotation marks or spaces stay intact, and `-r` runs nothing when nothing matched. With `set -o pipefail`, the pipeline exits with 1 when `grep` matched nothing. A name holding a newline cannot be passed this way, and neither can one holding a control character, which text output escapes as `\xNN`; `--json` prints the exact names, but wikictl rejects a path holding a control character with exit code 4.

`find --frontmatter=KEY,...` reads the frontmatter of every file that has one, not only of the pages, and prints each key with the line it is written on, so that the attributes of many pages can be collected in one command. Without keys it prints every key; the `=` is required.

To update an existing page, pass the `sha` printed by `stat` to `put --base`. If the page changed in between, `put` exits with code 3 and prints the current content; wikictl never merges.

`wikictl log [<path>...]` reads the history: one line per commit, newest first, with the commit sha, the author date, the commit date, the author and the subject. At most 20 commits are shown, `-n 0` shows all of them, `--since` and `--until` take a date (`YYYY-MM-DD`, read as a whole day in UTC) or an RFC 3339 time, `-S <string>` keeps the commits that changed how often a string occurs, and `--follow`, which takes exactly one path, follows a file across renames and adds the path each commit held. `wikictl cat --at <rev> <path>...` then reads a file as of a commit that `log` printed.

The `sha` of a past version is not a `--base`: to restore an old version, read its content with `cat --at` and write it with `put --base` and the `sha` that `stat` shows now.

## Configuration

wikictl reads the configuration from `--config <path>`, else `$WIKICTL_CONFIG`, else `$XDG_CONFIG_HOME/wikictl/config.yaml` (`~/.config/wikictl/config.yaml`).

| Key | Required | Meaning |
|---|---|---|
| `repo` | yes | URL or path of the wiki repository; a local path may be relative to the directory of the configuration file; may be set in a profile instead |
| `branch` | no | Branch to use; defaults to the branch saved in the mirror, else the remote HEAD, else `main`, and the saved branch is kept (see `wikictl help context`) |
| `author.name`, `author.email` | no | Commit author; each falls back to `git config user.name` or `user.email` |
| `lint.ignore` | no | Rule names left out of `lint`'s report and the same warnings from `put`, `edit` and `mv`; only `name_style` and `missing_summary` may be listed (see `wikictl help lint`) |
| `fetch_ttl` | no | Seconds within which a read skips fetching, if the mirror was already fetched that recently; 0, the default, never skips. Ignored by `put`, `edit`, `mv` and `rm`, which always fetch (see the Mirror section) |
| `profiles` | no | Named profiles that override the keys above |
| `default_profile` | no | Profile to use when no other rule selects one |

An unknown key is ignored with a warning on standard error, so that a file written for another version of wikictl still works. A key that is not a string, such as `1:`, is a configuration error.

Profiles keep several wikis, such as a personal one and a work one, in one file. The top-level keys are defaults, and a profile overrides them:

```yaml
author:
  name: claude-code@laptop
lint:
  ignore: [name_style, missing_summary]
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
    lint:
      ignore: []
    match:
      remotes: ["github.example.com/team/*"]
      paths: ["~/work"]
```

The profile is chosen by `--profile`, else `$WIKICTL_PROFILE`, else `match` (the `origin` remote or the current directory), else `default_profile`. In a profile, `author.name` and `author.email` override separately, `branch` is not inherited when the profile sets `repo`, and `lint` replaces the top-level value rather than merging into it, so a profile can set `lint: {}` or `lint: {ignore: []}` to ignore nothing. `fetch_ttl: 0` in a profile disables a top-level `fetch_ttl`, since a profile that leaves it out inherits the top-level value instead. `wikictl help context` describes the selection in detail.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | error, for example a missing page |
| 2 | usage or configuration error, including a mirror that cannot be prepared, such as a cache directory that cannot be created, and a `cat --at` rev that names no commit in the mirror |
| 3 | conflict: the page already exists, or changed or was deleted since it was read, or another push moved the branch while the change was being pushed |
| 4 | the page or path violates the wiki format; `lint` exits with 4 on any finding, or with 1 when a path does not exist |
| 5 | a git command failed, while reading or writing |

`grep` is the exception: it exits with 1 when no line matched, and with 2 when a path does not exist, while the other paths are still searched.

A conflict never writes anything: re-read the page and reapply the change, or, when the reason is `moved`, simply run the command again.

## Mirror

wikictl keeps a bare mirror of each wiki repository under `$XDG_CACHE_HOME/wikictl/` (`~/.cache/wikictl/`), readable only by the user. `wikictl context` shows its path. The wiki content is on the remote, so a mirror can be deleted at any time; the next command creates it again.

A read fetches before every command, like a write, unless `fetch_ttl` (see the Configuration section) says the mirror was already fetched within that many seconds, or `--no-fetch` skips a single read regardless of `fetch_ttl`. `fetch_ttl` is meant for reads repeated over time, such as a shell session or an agent's use of wikictl; it never applies to `put`, `edit`, `mv` or `rm`, which always fetch, because they decide what they change from what they read, and a stale read could leave a file added on the remote out of an `rm -r` or a `mv`. `fetch_ttl` also has no effect while the mirror has no tracking ref yet, such as right after it was created. `wikictl context` shows `fetch_ttl` and `fetched`, the last fetch time recorded in the mirror.

The mirror also limits what the history can show. `cat --at HEAD` and `cat --at <branch>` do not resolve, because the mirror keeps only the tracking refs (`refs/remotes/origin/*`) and the tags; a tag is neither updated nor deleted by a fetch, so one moved on the remote stays as it was; and a commit made unreachable by a force push can be removed by the mirror's automatic gc (two weeks by default), after which a sha that `log` printed no longer resolves.

## Development

    go test -race ./...
    go vet ./...
    gofmt -l .
    go build -o wikictl ./cmd/wikictl

GitHub Actions runs these checks and more on pull requests (see `.github/workflows/`). See [CONTRIBUTING.md](CONTRIBUTING.md) for the branch, pull request and release rules.

## License

[MIT](LICENSE)
