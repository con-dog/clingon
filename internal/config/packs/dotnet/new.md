# New
Create projects and solutions from templates, and link projects together

## Add a project reference
Makes this project depend on another project.

```sh when=./*.csproj,./*.fsproj,./*.sln,./*.slnx
dotnet add reference <project>
```

- `<project>` Which project

## Add a project to the solution
Adds a project to the solution file here.

```sh when=./*.sln,./*.slnx
dotnet sln add <project>
```

- `<project>` Which project

## New from a template
Creates a new project or file from a template.

```sh
dotnet new <template> -o <name>
```

- `<template>` Template
- `<name>` Name

## New solution
Creates a solution file here named after the folder.

```sh
dotnet new sln
```
