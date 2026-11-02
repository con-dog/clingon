# clingon's zsh setup, used only for the shell inside clingon. Each of these
# files reads yours (from your own ZDOTDIR, or ~) first, as zsh would, so
# your aliases, PATH and history are all there. Then .zshrc gives the prompt
# clingon's look. Your own files are never changed.
_clingon_zdotdir=$ZDOTDIR
ZDOTDIR=${CLINGON_USER_ZDOTDIR:-$HOME}
[[ -r $ZDOTDIR/.zshenv ]] && source "$ZDOTDIR/.zshenv"
CLINGON_USER_ZDOTDIR=$ZDOTDIR # yours may set ZDOTDIR itself
ZDOTDIR=$_clingon_zdotdir
