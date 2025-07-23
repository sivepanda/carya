# Carya
Reimagining what's possible with version control software.

Built using Go, Cobra, Sqlite, LipGloss, and BubbleTea to be feature-rich, user-friendly, and highly collaborative.

## Components
Carya uses a component-based structure. Each component was built to work well with the others, but allows you to decide what is the most useful to you. It doesn't matter if you just want to remove the work of running the same 3 commands over and over every time you pull, or if you want a highly interactive layer over git to improve its portability for small and fast-moving teams - Carya can help you (and can get out of the way when it can't).

### Feature-based commits
Construct commits using dependency tracked "chunks" generated while you code and hit save. That means that if you forget to commit before making major changes to your code, it is easy as ever to revert to the latest working version!

### Housekeeping
Configure default commands to run following a pull or checkout -- no more having to remember to run the same 3 commands over and over!



## Components *FOR LATER*
### Housekeeping (more stuff)
Will automatically detect your stack and provide you templates of housekeeping commands to run.

### Asynchronous Team Tracking
Utilizes your LSP to give you "windows" into the states of other active users' repositories, making it easier to ensure that you don't hit a merge conflict. (Of course, this feature really only works with small teams, as such **DANGER** this can be really messy for large teams. Don't say we didn't warn you!)

### Synchronous Development & Environment Sharing
Creates a shell of a host user's environment and recreates it on the client, while also streaming files between users, enabling a faster, more localized synchronous development pathway.
