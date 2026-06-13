# gg

Interactive local diffs. Review, stage, and polish your changes with agents. Think `git add -p`, but with super powers.

- Interactive staging with a live diff view
- Ask Copilot questions about your code as you review
- Make finishing touches to your changes inline
- Respond to and interact with PR review comments
- Commit, push, and create PRs without leaving the TUI

## Install

```
cargo install --git https://github.com/BlakeWilliams/gg gg
```

Requires Rust nightly and a GitHub token (via the `gh` CLI or `GITHUB_TOKEN` env var).

## Agent Workspace

`gg` opens into the **agent workspace**: a multi-agent view where each agent maps
1:1 to a git branch (and optional PR). The layout is **agent list (left) | stage
(center) | chat (right)**.

- `Shift+Tab` — toggle the active agent between **plan** (read-only; the agent may
  only write its plan file) and **execution** (edits applied) mode
- `Tab` — cycle the stage between **Plan**, **Diff**, and **PR**
- `1` / `2` / `3` — jump to the Plan / Diff / PR panel
- `Ctrl+R` — toggle review mode on the Diff/PR (hides the chat; `c` to message the
  agent or reply to a reviewer)
- `h` / `l` / `f` — move focus between agent list, stage, and chat
- In the agent list: `j` / `k` navigate, `enter` select, `n` new agent, `d` remove
- In the chat: type a message and press `enter` to send

The Plan panel opens while a plan is generated; the stage swaps to Diff once the
agent starts editing, and the PR tab activates when a pull request is detected.

Agents are isolated using git worktrees by default. Configure via `config.yaml`:

```yaml
agent_isolation: worktree   # or "checkout" to switch branches in place
agent_backend: copilot
worktree_root: null         # defaults to <repo>/.git/gg-worktrees
plan_path: .gg/plan.md      # where the agent writes its plan in plan mode
```

## Local Diff

The classic interactive local-diff experience is still available as a separate
view. Run `gg` in a branch with changes.

**Navigation**

- `j` / `k` — move up/down
- `J` / `K` — extend selection
- `h` / `l` — focus tree / diff / panel
- `f` — cycle focus between panes
- `ctrl+j` / `ctrl+k` — next/previous file
- `ctrl+p` — fuzzy file picker
- `gg` / `G` — top/bottom
- `ctrl+d` / `ctrl+u` — half-page scroll
- `ctrl+f` / `ctrl+b` — full-page scroll
- `]c` / `[c` — next/previous comment thread

**Staging**

- `s` — stage line (working tree view)
- `S` — stage hunk
- `u` / `U` — unstage line or hunk (staged view)
- `m` — cycle view: working → staged → branch

**Search**

- `/` — open search (supports regex, smart-case)
- `n` / `N` — next/previous match
- `esc` — cancel search (restores cursor position)

**Comments**

- `c` — new comment on current line
- `r` — reply to comment thread
- `x` — resolve/unresolve thread
- `ctrl+g` — open `$EDITOR` while composing

**Commit / Push**

- `C` — commit picker (commit, push, create PR)
- `ctrl+g` — open `$EDITOR` for commit message

**Actions**

- `enter` — open comment thread / ask Copilot
- `:` — command palette
- `?` — help (all keybindings)
- `q` — close panel
- `ctrl+c` (×2) — quit

Press `?` for the full list of keybindings.
