# cmd
Contains external executables, so pretty much just the main method

# internal
Contains business logic, each system is in its own package.

## internal/core
Core stuff, probably will rename to commands or something and this will just house each of the commands that Cobra can call

## internal/chunk
Defines what chunks are, their schema, what needs to be saved and tracked, etc

## internal/reactor
Reacts to push/pulls and other system changes outside of filesys. Essentially the housekeeping factory (can detect frameworks tech stack, etc and automatically can run "houskeeping" like npm install, etc)
*May bundle with watcher?*

## internal/store
Internal (to be shared) datastore for chunks and other save data changes so that chunks and states can be stored locally (and potentially stored in git, whatever ends up working best) Also keeps track of current config and other things (like what housekeeping to run)

## internal/watcher
Defines when chunks are made, changes to system files, etc


# TODO

1. Still need to rework `watcher` to use the .gitignore as a basis for its tracking
2. Need to roll in and implement some sort of databasing, probably sqlite (store)
3. Build housekeeping engine - reads for common package manager configs + optionally can be configured
