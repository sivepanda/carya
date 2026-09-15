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

## Planned Components
### Synchronous Development & Environment Sharing
Creates a shell of a host user's environment and recreates it on the client, while also streaming files between users, enabling a faster, more localized synchronous development pathway.
