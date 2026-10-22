# bonsai shim: load the user's .zshrc with the real ZDOTDIR, add the prompt
# hook, and leave the real ZDOTDIR exported for child shells. Runs at top
# level, not in an anonymous function: the user's `typeset`/`local`-style
# declarations would otherwise become function-local and vanish on return.
if [[ -n $__bonsai_real ]]; then ZDOTDIR=$__bonsai_real; else unset ZDOTDIR; fi
[[ -r ${ZDOTDIR:-$HOME}/.zshrc ]] && source ${ZDOTDIR:-$HOME}/.zshrc
source ${__bonsai_shim:h}/hook.zsh
unset BONSAI_ORIG_ZDOTDIR __bonsai_shim __bonsai_real
