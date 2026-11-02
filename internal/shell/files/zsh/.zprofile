# clingon: reads your .zprofile (see .zshenv).
ZDOTDIR=$CLINGON_USER_ZDOTDIR
[[ -r $ZDOTDIR/.zprofile ]] && source "$ZDOTDIR/.zprofile"
ZDOTDIR=$_clingon_zdotdir
