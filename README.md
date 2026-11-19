## DISCLAIMER: THIS PROJECT IS UNDER DEVELOPMENT AND IS CURRENTLY NOT FUNCTIONAL YET!
That also means you can easily hit me up if you have ideas and have them be integrated into the core platform. Leave an issue!  :)

# Carya
Reimagining what's possible with version control software.

Built using Go, Cobra, Sqlite, LipGloss, and BubbleTea to be feature-rich, user-friendly, and highly collaborative.

## Components
Carya uses a component-based structure. Each component was built to work well with the others, but allows you to decide what is the most useful to you. It doesn't matter if you just want to remove the work of running the same 3 commands over and over every time you pull, or if you want a highly interactive layer over git to improve its portability for small and fast-moving teams - Carya can help you (and can get out of the way when it can't).

### Feature-based commits
Construct commits using dependency tracked "chunks" generated while you code and hit save. That means that if you forget to commit before making major changes to your code, it is easy as ever to revert to the latest working version!

Use `carya compose` to interactively select and commit specific changes, or `carya view` to browse tracked chunks.

### Housekeeping
Configure default commands to run following a pull or checkout -- no more having to remember to run the same 3 commands over and over!

See [AUTODETECT.md](internal/housekeeping/AUTODETECT.md) to contribute package manager detection rules.



### Asynchronous Team Tracking
Share your working state with your team via git refs. See what files your teammates are editing and predict merge conflicts before they happen.

Use `carya publish` to share your state, `carya team` to see teammates, and `carya team conflicts <user>` to predict conflicts.

**Note:** This feature works best with small teams. It can get noisy for large teams.

### Coding Agents
Run several coding agents on one repository without them stepping on each other. Each agent's checkout (a clone or a worktree) gets its own identity and publishes what it has on disk after every tool call, shell commands included. Before an agent edits a file another agent is changing, Carya adds that agent's change to its context, and says whether the two would conflict.

Set it up with one command, no prompts:

```bash
carya setup claude     # Claude Code: hooks in .claude/settings.local.json
carya setup codex      # Codex: hooks in .codex/hooks.json
carya setup opencode   # OpenCode: a plugin in .opencode/plugins/carya.ts
```

By default nothing tracked changes: Carya keeps `.carya/` and the files it writes out of git through `.git/info/exclude`. Use `--scope project` to commit the setup for everyone, or `--scope user` to install it once in your user config and have it switch on in each repository you run setup in. `--remove` takes it out again.

- **Claude Code** picks the hooks up immediately. Before Claude edits a file another agent is changing, that change is added to its context.
- **Codex** asks you to approve new hooks once: start `codex`, trust the project, then approve Carya's hooks in `/hooks`. Before Codex applies a patch to a file another agent is changing, that change is added to its context.
- **OpenCode** (1.x and 2.x) loads the plugin at startup. OpenCode plugins can't add context before a tool runs, so after an edit Carya appends other agents' changes to the edited files to the tool output.

Before anyone opens a pull request, check that everyone's unfinished work fits together:

```bash
carya integrate
```

This merges every published state (each agent's and yours) onto the default branch in memory, leaves out and reports any state that conflicts along with whose work it collides with, and keeps the result at `refs/carya/integration`. It then checks the result out in a temporary worktree, runs the repository's Mycelia post-checkout setup, and builds and tests it (`--run` to choose the commands; by default they are detected from `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml` or a `Makefile`). Your checkout is never touched. Note that this runs other people's unfinished code on your machine. `--since 24h` leaves out states nobody has published in a day, `--merge-only` skips the build and tests, and `--json` is there for agents.

Use `carya check <file>` to see other agents' unfinished changes to a file yourself. Agents in worktrees of the same repository see each other with no network; with an `origin` remote, states are also pushed to `refs/carya/*`, which does not show up as branches on GitHub. Cloud sessions don't share state yet.

## Planned Components
### Synchronous Development & Environment Sharing
Creates a shell of a host user's environment and recreates it on the client, while also streaming files between users, enabling a faster, more localized synchronous development pathway.

## License
Carya is free software, licensed under the [GNU General Public License v3.0](LICENSE) (`GPL-3.0-only`).
