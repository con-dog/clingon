# clingon: reads your .zshrc, then puts clingon's prompt on top (see .zshenv).
ZDOTDIR=$CLINGON_USER_ZDOTDIR
[[ -r $ZDOTDIR/.zshrc ]] && source "$ZDOTDIR/.zshrc"
# From here ZDOTDIR is yours again: zsh reads your .zlogin itself, and
# anything that looks in $ZDOTDIR later (plugin managers, say) finds yours.
if [[ $ZDOTDIR == $HOME && -z $CLINGON_HAD_ZDOTDIR ]]; then
  unset ZDOTDIR
fi
unset _clingon_zdotdir CLINGON_USER_ZDOTDIR CLINGON_HAD_ZDOTDIR
[[ -z $CLINGON_BIN ]] || path=("$CLINGON_BIN" "${path[@]:#$CLINGON_BIN}") # a local build stays first

autoload -Uz vcs_info
zstyle ':vcs_info:*' enable git
zstyle ':vcs_info:git:*' formats ' %b'
zstyle ':vcs_info:git:*' actionformats ' %b|%a'
setopt prompt_subst

_clingon_prompt() {
  local ok=$? # before anything else runs
  vcs_info
  local mark='%F{212}'
  (( ok == 0 )) || mark='%F{204}'
  local aws=''
  [[ -z $AWS_PROFILE ]] || aws=" %F{208}aws:\${AWS_PROFILE//\%/%%}%f"
  PROMPT="%B%F{99}%~%f%b%F{103}\${vcs_info_msg_0_//\%/%%}%f${aws}"$'\n'"${mark}❯%f "
  RPROMPT=''
}
# Other prompts' hooks would only redraw theirs; clingon's runs last, so it
# has the final word.
precmd_functions=(${precmd_functions:#(starship_precmd|_clingon_prompt)} _clingon_prompt)
preexec_functions=(${preexec_functions:#starship_preexec})
