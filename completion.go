package main

import (
	"fmt"
	"os"
)

// completion prints a tab-completion script for a shell. Homebrew installs
// them; otherwise e.g. `source <(clingon completion zsh)` in ~/.zshrc.
func completion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: clingon completion zsh|bash")
	}
	script, ok := map[string]string{"zsh": zshCompletion, "bash": bashCompletion}[args[0]]
	if !ok {
		return fmt.Errorf("no completion for %q: use zsh or bash", args[0])
	}
	_, err := os.Stdout.WriteString(script)
	return err
}

const zshCompletion = `#compdef clingon

_clingon() {
  local -a cmds
  cmds=(
    'print:pick a command and print it'
    'pack:list, add and remove packs'
    'command:add and list your own commands'
    'recipe:add, remove and list recipes'
    'edit:open your pack in micro'
    'check:check your packs for mistakes'
    'doctor:check tmux, your packs and their tools'
    'crash:show why clingon last crashed'
    'update:upgrade clingon'
    'welcome:show the banner and anything worth doing'
    'completion:print a tab-completion script'
    'version:show the version'
    'help:show help'
  )
  if (( CURRENT == 2 )); then
    _describe command cmds
    return
  fi
  case $words[2] in
    pack|packs)
      if (( CURRENT == 3 )); then
        compadd list available add remove create update edit modules describe guide
        return
      fi
      case $words[3] in
        add) compadd -- ${(f)"$(clingon pack available --short 2>/dev/null | cut -f1)"} ;;
        remove|rm|modules) compadd -- ${(f)"$(clingon pack list --short 2>/dev/null | cut -f1)"} ;;
        update) compadd -- ${(f)"$(clingon pack list --remote --short 2>/dev/null | cut -f1)"} ;;
        edit) compadd -- ${(f)"$(clingon pack list --mine --short 2>/dev/null | cut -f1)"} ;;
        describe|cat) compadd -- ${(f)"$(clingon pack list --short 2>/dev/null | cut -f1)"} ${(f)"$(clingon pack available --short 2>/dev/null | cut -f1)"} ;;
      esac
      ;;
    command|commands|cmd) (( CURRENT == 3 )) && compadd add list ;;
    recipe|recipes) (( CURRENT == 3 )) && compadd add remove list describe ;;
    doctor) (( CURRENT == 3 )) && compadd -- ${(f)"$(clingon pack list --remote --short 2>/dev/null | cut -f1)"} ;;
    completion) (( CURRENT == 3 )) && compadd zsh bash ;;
  esac
}

if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
  _clingon "$@"
else
  compdef _clingon clingon
fi
`

const bashCompletion = `_clingon() {
  local cur=${COMP_WORDS[COMP_CWORD]} words=
  case $COMP_CWORD in
    1) words="print pack command recipe edit check doctor crash update welcome completion version help" ;;
    2)
      case ${COMP_WORDS[1]} in
        pack|packs) words="list available add remove create update edit modules describe guide" ;;
        command|commands|cmd) words="add list" ;;
        recipe|recipes) words="add remove list describe" ;;
        doctor) words=$(clingon pack list --remote --short 2>/dev/null | cut -f1) ;;
        completion) words="zsh bash" ;;
      esac
      ;;
    *)
      case ${COMP_WORDS[1]}:${COMP_WORDS[2]} in
        pack:add|packs:add) words=$(clingon pack available --short 2>/dev/null | cut -f1) ;;
        pack:update|packs:update) words=$(clingon pack list --remote --short 2>/dev/null | cut -f1) ;;
        pack:edit|packs:edit) words=$(clingon pack list --mine --short 2>/dev/null | cut -f1) ;;
        pack:describe|packs:describe|pack:cat|packs:cat) words="$(clingon pack list --short 2>/dev/null | cut -f1) $(clingon pack available --short 2>/dev/null | cut -f1)" ;;
        pack:*|packs:*) words=$(clingon pack list --short 2>/dev/null | cut -f1) ;;
      esac
      ;;
  esac
  COMPREPLY=($(compgen -W "$words" -- "$cur"))
}
complete -F _clingon clingon
`
