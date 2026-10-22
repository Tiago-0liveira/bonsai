# bonsai prompt hook (zsh): puts $BONSAI_PROMPT_PREFIX in front of PROMPT.
# The guard is a non-exported variable so each shell process installs once.
if [[ -z ${__BONSAI_PROMPT_INSTALLED-} ]]; then
  typeset -g __BONSAI_PROMPT_INSTALLED=1
  __bonsai_prompt() {
    local p=${BONSAI_PROMPT_PREFIX-}
    [[ -n $p && $PROMPT != "$p"* ]] && PROMPT=$p$PROMPT
    return 0
  }
  # Last entry, so it runs after p10k/oh-my-zsh rebuild PROMPT.
  precmd_functions+=(__bonsai_prompt)
fi
