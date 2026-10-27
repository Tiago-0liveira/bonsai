# bonsai prompt hook (bash): puts $BONSAI_PROMPT_PREFIX in front of PS1.
# The guard is a plain shell variable, never exported, so a nested shell
# installs its own hook while one shell never gets the prefix twice.
if [ -z "${__BONSAI_PROMPT_INSTALLED-}" ]; then
  __BONSAI_PROMPT_INSTALLED=1
  __bonsai_prompt() {
    local s=$? p=${BONSAI_PROMPT_PREFIX-}
    if [[ -n $p && $PS1 != "$p"* ]]; then PS1=$p$PS1; fi
    return $s
  }
  # Last in PROMPT_COMMAND so it runs after prompt tools (starship) rebuild PS1.
  if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == "declare -a"* ]]; then
    PROMPT_COMMAND+=(__bonsai_prompt)
  else
    PROMPT_COMMAND="${PROMPT_COMMAND:+${PROMPT_COMMAND%;}; }__bonsai_prompt"
  fi
fi
