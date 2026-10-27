# bonsai shim: ZDOTDIR points here only until .zshrc has run, then the real
# value is restored so child processes never see the shim directory.
typeset -g __bonsai_shim=$ZDOTDIR
if [[ -n ${BONSAI_ORIG_ZDOTDIR-} ]]; then ZDOTDIR=$BONSAI_ORIG_ZDOTDIR; else unset ZDOTDIR; fi
[[ -r ${ZDOTDIR:-$HOME}/.zshenv ]] && source ${ZDOTDIR:-$HOME}/.zshenv
# The user's .zshenv may move ZDOTDIR; zsh would read .zprofile/.zshrc from
# there, so remember where it landed (empty = unset) for the later shims.
typeset -g __bonsai_real=${ZDOTDIR-}
ZDOTDIR=$__bonsai_shim
