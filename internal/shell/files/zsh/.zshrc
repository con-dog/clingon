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
  # The path: folders above periwinkle, slashes grey, and the folder you're in
  # bold purple. A
  # name is text: its %s are doubled, and the prompt only reads it, never runs it.
  local p=${(%):-%~}
  p=${p//\%/%%}
  local here=${p##*/} up=''
  [[ $p == */* ]] && up=${p%/*}/
  [[ $p == / ]] && here=/ up=''
  _clingon_path="%F{147}${up//\//%F{243\}/%F{147\}}%F{99}%B${here}%b%f"
  # In a repository, git's logo (clingon's font has it) before the branch.
  local git=''
  [[ -n $vcs_info_msg_0_ ]] && git=" %F{203}"$'\uE004'"%f"
  PROMPT="\${_clingon_path}${git}%F{103}\${vcs_info_msg_0_//\%/%%}%f${aws}"$'\n'"${mark}❯%f "
  RPROMPT=''
}
# Typed commands in clingon's colours, if a highlighting plugin is loaded: the
# same as a command in the menu, in micro and in `clingon pack guide` (programs
# green, flags periwinkle, strings tan, operators grey). Only in clingon's shell.
() {
  local -A want=(
    command fg=79 builtin fg=79 alias fg=79 function fg=79 precommand fg=79
    subcommand none path none path-to-dir none
    single-hyphen-option fg=147 double-hyphen-option fg=147
    single-quoted-argument fg=180 double-quoted-argument fg=180 dollar-quoted-argument fg=180
    commandseparator fg=103 redirection fg=103
  )
  local k
  if (( ${+FAST_HIGHLIGHT_STYLES} )); then # fast-syntax-highlighting
    for k in ${(k)want}; FAST_HIGHLIGHT_STYLES[${FAST_THEME_NAME}$k]=$want[$k]
  fi
  if (( ${+ZSH_HIGHLIGHT_STYLES} )); then # zsh-syntax-highlighting
    for k in ${(k)want}; ZSH_HIGHLIGHT_STYLES[$k]=$want[$k]
  fi
}
# Other prompts' hooks would only redraw theirs; clingon's runs last, so it
# has the final word.
precmd_functions=(${precmd_functions:#(starship_precmd|_clingon_prompt)} _clingon_prompt)
preexec_functions=(${preexec_functions:#starship_preexec})
