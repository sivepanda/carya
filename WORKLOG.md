# cmd
Contains external executables, so pretty much just the main method

# internal
Contains business logic, each system is in its own package.

## internal/engine
Main engine and coordination logic that orchestrates chunk management, storage, and file watching

## internal/chunk
Defines what chunks are, their schema, what needs to be saved and tracked, etc

## internal/reactor
Reacts to push/pulls and other system changes outside of filesys. Essentially the housekeeping factory (can detect frameworks tech stack, etc and automatically can run "houskeeping" like npm install, etc)
*May bundle with watcher?*

## internal/store
Internal (to be shared) datastore for chunks and other save data changes so that chunks and states can be stored locally (and potentially stored in git, whatever ends up working best) Also keeps track of current config and other things (like what housekeeping to run)

## internal/watcher
Defines when chunks are made, changes to system files, etc

## internal/housekeeping
Defines and handles actions taken after a pull or switching branches -- things such as npm install, bun install, etc


# TODO

1. Improve housekeeping engine - presently all configs need to be registered, make it smarter
2. Make things look nicer. Implement LipGloss, BubbleTea.




# HEY BIG NOTE THINGS ARE VERY BROKEN RN FIX PLSE
