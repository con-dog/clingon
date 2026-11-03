# Renders demo.gif for the README: `vhs demo.tape` (brew install vhs)
Output demo.gif
Set Shell zsh
Set FontSize 16
Set Width 1400
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms

Hide
# clingon normally opens its own window, which vhs wouldn't record.
Type "export CLINGON_WINDOW=kitty; cd ~/Code/Projects/clingon && clear" Enter
Show

Type "clingon" Enter
Sleep 1s

# global search, a real cd
Type "/go to folder" Sleep 600ms Enter
Sleep 800ms
Type "internal" Sleep 300ms Enter
Sleep 1.2s

# back to the menu, a flags checklist building the command line
Shift+Tab
Type "/pretty" Sleep 500ms Enter
Sleep 500ms Enter
Sleep 400ms Space Sleep 300ms Down Down Sleep 300ms Space
Sleep 800ms Enter
Sleep 2s

# a guarded command
Shift+Tab
Type "/delete files matching" Sleep 500ms Enter
Type "*.log" Enter Enter
Sleep 1.5s
Type "n"
Sleep 1s

Escape
Sleep 1s
