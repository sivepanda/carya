# Carya

A layer over git for small teams and coding agents.

> Under active development; expect rough edges. Ideas welcome: open an issue.

## Coding agents

Run several agents on one repository without them stepping on each other.

```bash
carya setup claude     # or: codex, opencode
```

Each agent's checkout gets its own identity and publishes its work (everything not yet in the branch you ran setup on, committed or not) after every tool call. When an agent edits a file another agent is changing, it sees that change and whether the two conflict. Setup changes no tracked files (`--scope project|user` to share it, `--remove` to undo). Codex asks you to approve the hooks once in `/hooks`.

| Command | |
|---|---|
| `carya check <file>` | Other agents' unfinished changes to a file |
| `carya related <file>` | Files that usually change with it |
| `carya why <file>[:line]` | Why it changed, and who is changing it now |
| `carya hunks` | Your uncommitted changes as hunks; `carya commit -m <msg> <ids\|files\|id:lines>` commits a group of them |
| `carya integrate` | Merge everyone's work, then set up, build and test it in a temporary worktree |
| `carya states` | List published states; `carya states prune` deletes stale ones |

States are shared through `refs/carya/*` on `origin`, which GitHub doesn't show as branches. Agents in worktrees of one repository see each other with no network. Cloud sessions don't share state yet. `carya integrate` runs other people's unfinished code on your machine.

## For people

- **Chunks:** Carya tracks your saves as chunks. `carya compose` turns them into commits; `carya view` browses them.
- **Housekeeping:** `carya pull` and `carya checkout` run your setup commands afterwards (`carya housekeeping` to configure). Detection rules live in [Mycelia](https://github.com/sivepanda/mycelia).
- **Team states:** `carya publish` shares your working state, `carya team` lists teammates', `carya team conflicts <user>` predicts conflicts, and `carya jump <user>` checks out theirs. Best for small teams.

## License

[GPL-3.0-only](LICENSE)
