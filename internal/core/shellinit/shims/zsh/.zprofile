# bonsai shim: forward to the user's .zprofile with the real ZDOTDIR. Top
# level, not an anonymous function, so the user's declarations stay global.
if [[ -n $__bonsai_real ]]; then ZDOTDIR=$__bonsai_real; else unset ZDOTDIR; fi
[[ -r ${ZDOTDIR:-$HOME}/.zprofile ]] && source ${ZDOTDIR:-$HOME}/.zprofile
__bonsai_real=${ZDOTDIR-}
ZDOTDIR=$__bonsai_shim
