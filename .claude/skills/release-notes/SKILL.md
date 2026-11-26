---
name: release-notes
description: Write the release note entries of a pull request, preview the notes of the next release, pick the next version and tag it. Use when opening or editing a pull request that has a feat, fix or breaking commit, when the release notes preview warns about a blockquote count, when asked what the next version is, when asked to release or tag main, or when a published release note is wrong.
---

# Release notes & versioning

The repository has no changelog file. When a `vX.Y.Z` tag is pushed, `tools/releaser` builds the
GitHub release notes from the commits on `main` since the previous tag, and goreleaser publishes
them. `main` is always ready to tag, so every pull request carries its own entries.

## Which commits get an entry

Pull requests are rebase-merged, so each commit of a pull request lands on `main` with its own
title. A commit gets one entry when its title has:

| Title | Section of the notes |
|---|---|
| a `!` before the colon, with any type, such as `feat!:` or `refactor(client)!:` | Breaking changes |
| `feat:` | Features |
| `fix:` | Fixes |

A scope is allowed and has no effect: `fix(client): ...` is a fix. Every other commit, such as
`docs:`, `test:`, `ci:`, `chore:`, `build:` or `refactor:`, gets no entry. Choose the type by what a
user of the CLI, or a caller of `client/` such as the Terraform provider, notices: a change that
nobody notices is no `feat` and no `fix`.

## Write the entries in the pull request description

Start the pull request description with one blockquote per entry commit, in the order of the
commits. A blank line separates two blockquotes. The first line of normal text ends the entries.

<example>
A pull request with the commits `fix: keep the profile when a login is aborted`,
`test: cover the aborted login` and `feat(client): list building block runners` has two entry
commits, so its description starts with two blockquotes:

```markdown
> `meshstack login`: an aborted browser login keeps the stored profile, so the previous login
> still works.

> Go client: `Client.BuildingBlockRunner` lists the building block runners.

## Summary

The rest of the description is free text.
```
</example>

- **The count must match.** The releaser uses the blockquotes of a pull request only when their
  number equals the number of its entry commits. With any other number it uses none of them and
  prints a warning.
- **Only the leading blockquotes count.** Put every other blockquote, such as a quoted error
  message, below the first line of normal text. A blockquote above that line is taken as an entry.
- **A pull request written by an AI agent keeps `> 🤖 _Written by AI agent_` as its first line.** The
  entries follow it as the next blockquotes, separated from it by a blank line. The releaser skips
  every leading blockquote that starts with 🤖: it is no entry and does not count.
- An entry can have several lines and several paragraphs. Keep paragraphs in one blockquote with a
  `>` line between them.
- Wrap the lines of an entry freely, in the pull request description and in a commit body. The
  releaser joins the lines of a paragraph, because GitHub shows every newline of a release body as
  a line break. Code blocks and lists stay as written.

### Wording of an entry

- Name what is affected first: the command in backticks, such as `` `meshstack login`: ``, or
  `Go client:` for a change of `client/`.
- Describe what the user notices. For a fix, name the symptom that goes away. For a breaking change,
  say what the user or the caller of `client/` must change.
- Write complete sentences in present tense. Leave out the internal diff.
- The first line of the notes, `Requires meshStack X or later.`, is written by the releaser from
  `client.MinMeshStackVersion`, below which the CLI refuses a meshStack. When one feature needs a
  newer meshStack than that, say so as plain text in its entry.

## Where the releaser takes the text from

For each entry commit the releaser takes the first text that exists:

1. The matching leading blockquote of the pull request description.
2. The leading blockquotes of the commit message body.
3. The commit title without its `type(scope)!: ` prefix.

The pull request description wins over the commit body. GitHub is read when the tag is pushed, so
the description of a merged pull request can still be edited until then. Each entry ends with its
pull request number and the GitHub handle of the commit author, such as `(#421 by @octocat)`. The
releaser takes the handle from the commit, so a commit by another person than the one who opened
the pull request names its own author. A commit that was pushed without a pull request shows its
short commit id.

An entry also names the issues that GitHub linked to its pull request as closed, such as
`(#423 by @octocat, closes #389)`. Link an issue with a closing keyword, such as `Fixes #389`. The
preferred place is the body of the commit that fixes the issue, because this ties the issue to the
entry of that commit. A keyword in the pull request description also links the issue. Such an issue
is shown when the pull request has one entry commit, or when exactly one of its entry commits has no
issue of its own.

## Preview the notes

```bash
GOWORK=off go run ./tools/releaser "$(git describe --tags --abbrev=0 HEAD^)..HEAD"
```

The notes go to stdout. The warnings and the next version go to stderr. The releaser needs a token
in `GITHUB_TOKEN` or `GH_TOKEN` to read the pull requests, the authors and the issues from GitHub;
`export GH_TOKEN=$(gh auth token)` sets one. Without a token it warns, and every entry uses the
commit text. The `Release notes` workflow runs the same command on every push to `main` and
shows the notes in its run summary.

Fix a warning about a blockquote count by editing the pull request description, then run the
preview again. The releaser reads merged pull requests only, so the commits of an open pull request
use the commit text.

## Pick the version

The CLI never raises the major version. Since the last tag:

- a `feat` commit, or any commit with `!`, raises the minor version and sets the patch version to 0
  (`v0.11.2` → `v0.12.0`);
- else the patch version goes up (`v0.11.2` → `v0.11.3`).

The preview prints the version and its reason, such as
`Next version: v0.11.3 (no feature and no breaking change since v0.11.2)`.

## Tag and release

1. Run the preview on an up-to-date `main` and read the notes.
2. Tag and push:

   ```bash
   git tag vX.Y.Z && git push origin vX.Y.Z
   ```

The `Release` workflow writes the notes and publishes the release. A wrong commit title on `main`
stays as it is, because the history of `main` is not rewritten. When it put an entry into the wrong
section or left one out, correct the notes by hand on the published release page.
