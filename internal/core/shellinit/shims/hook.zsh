# bonsai prompt hook (zsh): puts $BONSAI_PROMPT_PREFIX in front of PROMPT.
# The guard is a non-exported variable so each shell process installs once.
if [[ -z ${__BONSAI_PROMPT_INSTALLED-} ]]; then
  typeset -g __BONSAI_PROMPT_INSTALLED=1
  __bonsai_prompt() {
    local p=${BONSAI_PROMPT_PREFIX-}
    [[ -n $p && $PROMPT != "$p"* ]] && PROMPT=$p$PROMPT
    # Plugins that register precmd hooks lazily would otherwise run after us
    # and overwrite PROMPT.
    [[ ${precmd_functions[-1]-} == __bonsai_prompt ]] ||
      precmd_functions=(${precmd_functions:#__bonsai_prompt} __bonsai_prompt)
    return 0
  }
  # Last entry, so it runs after p10k/oh-my-zsh rebuild PROMPT.
  precmd_functions+=(__bonsai_prompt)

  # Async themes (p10k, pure) rebuild PROMPT after precmd, then reset-prompt;
  # re-apply the prefix there. Chain any existing zle function.
  if (( $+functions[zle] )); then
    functions[__bonsai_orig_zle]=$functions[zle]
    zle() {
      [[ $1 == (.|)reset-prompt ]] && __bonsai_prompt
      __bonsai_orig_zle "$@"
    }
  else
    zle() {
      [[ $1 == (.|)reset-prompt ]] && __bonsai_prompt
      builtin zle "$@"
    }
  fi
fi
