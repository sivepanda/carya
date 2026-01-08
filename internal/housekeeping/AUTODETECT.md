# Contributing to `autodetect.json`
`Autodetect.json` is a list of package managers/libraries that often require some sort of syncing after a pull or checkout. This list is used to suggest potential post- pull and checkout commands, however, it is always the user's choice on whether or not they use them. Users can also add their own custom commands to their `.carya`  config file, so additions to autodetect must be general and widely used libraries.

Feel free to make a pull request!

## Keys
`name` The name of the package manager/library
`detectFile` File that when detected, can be used to infer a potential package manager/library used
`detectFiles` Files that only when detected *together*, can be used to infer a potential package manager/library used **CANNOT BE USED WITH detectFile**
`description` Description of the command/library/pkgman
`commands` contains post-pull and post-checkout commands. **Both post-pull and post-checkout use the same schema.**
`post-pull` contains associated post-pull command
`post-checkout` contains associated post-checkout command
`command` bash command to run
`workingDir` directory to run bash command
`description` description of bash command

## Examples
Below is a simple example of an addition to autodetect:
```json
{
    "name": "npm",
    "detectFile": "package.json",
    "description": "Node.js (npm)",
    "commands": {
      "post-pull": [
        {
          "command": "npm install",
          "workingDir": ".",
          "description": "Install npm dependencies"
        }
      ],
      "post-checkout": [
        {
          "command": "npm install",
          "workingDir": ".",
          "description": "Install npm dependencies"
        }
      ]
}
```

Alternatively, if multiple files must be detected to infer, you can add multiple detectFiles:
```json
    "name": "prisma-pnpm",
    "detectFiles": [
      "prisma/schema.prisma",
      "pnpm-lock.yaml"
    ],
    "excludes": [
      "prisma-npm"
    ],
    "description": "Prisma ORM (pnpm)",
    "commands": {
      "post-pull": [
        {
          "command": "pnpm prisma generate",
          "workingDir": ".",
          "description": "Generate Prisma Client (pnpm)"
        },
        {
          "command": "pnpm prisma migrate deploy",
          "workingDir": ".",
          "description": "Apply Prisma migrations (pnpm)"
        }
      ],
      "post-checkout": [
        {
          "command": "pnpm prisma generate",
          "workingDir": ".",
          "description": "Generate Prisma Client (pnpm)"
        },
        {
          "command": "pnpm prisma migrate deploy",
          "workingDir": ".",
          "description": "Apply Prisma migrations (pnpm)"
        }
      ]
    }
```

