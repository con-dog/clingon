# Recipes

A recipe runs several commands in order, and stops if one fails or clingon can't tell whether it worked.

**Build one.** On any command, press a number to put it at that position:

1. Open a section and press `1` on the first command.
2. Press `2` on the next, from the same section or any other.
3. Press `r` to see the recipe, then `enter` on **Run**.

The recipe's own list shows its numbered steps, each with its pack, under a heading that says how many there are, how many are risky and whether it's added. Above the steps: **Run**, **Add as…**, **Describe** and **Clear**.

**Clingon › Recipes** lists the current recipe and **My recipes**, each with how many steps it has and a mark if any step is risky. Open one to see its steps, then **Run**, **Describe**, **Edit** or **Remove** it. **Edit** makes it the current recipe: change it with the number keys, then **Add as…** under the same name to replace it.

Each recipe you add is a Markdown file in `~/.clingon/recipes/`, next to your packs: a name, an optional description, and a numbered list of links to each command's heading in its pack's file. You can edit one by hand, or add a description:

```md
# Deploy check
Pull, then see what changed.

1. [Status](../packs/remote/code/git/everyday.md#status)
2. [Pull](../packs/remote/code/git/everyday.md#pull)
```

**Positions work like this:**

- A command can be in a recipe more than once, like `git status`, `git pull`, `git status`. Its row shows each number: `1 3`.
- Pressing a command's own number removes it from that position, and later ones close up.
- Putting a new command at a position clears every position after it.
- Recipes have up to 9 commands, with no gaps. Number keys don't work while you're filtering.
- Press `x` to clear the recipe.

**Blanks are asked for when each command is reached**, so earlier commands have already had their effect. Answers aren't kept: a recipe asks again every run. Risky commands still ask before they run.

**Run from anywhere.** Press `enter` on a command in the recipe's list to run the recipe from there, leaving out the ones before it. If a command stops the recipe, the cursor waits on it when you press `r`: press `enter` to run it again and carry on, or move down a row to skip it, say if you've fixed things by hand in the shell. **Run** always starts from the top.

**Need a command that isn't in a pack?** Number **Command Line › Commands › Type a command** like any other command. It asks what to run each time the recipe gets there. If you run the same line every time, [add it to a pack of your own](packs.md#your-own-commands).

From the command line:

```sh
clingon recipe list
clingon recipe describe "My flow"   # what each command runs; runs nothing
clingon recipe remove "My flow"
```

