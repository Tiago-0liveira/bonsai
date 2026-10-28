# bonsai shim for sh/dash/ksh/mksh, loaded through $ENV. Run the user's own
# $ENV first, give children their original $ENV back, then prefix PS1.
if [ -n "${BONSAI_ORIG_ENV-}" ] && [ -r "$BONSAI_ORIG_ENV" ]; then
  . "$BONSAI_ORIG_ENV"
fi
if [ -n "${BONSAI_ORIG_ENV-}" ]; then
  ENV=$BONSAI_ORIG_ENV
  export ENV
else
  unset ENV
fi
unset BONSAI_ORIG_ENV
if [ -z "${__BONSAI_PROMPT_INSTALLED-}" ]; then
  __BONSAI_PROMPT_INSTALLED=1
  if [ -z "${PS1+x}" ]; then PS1='$ '; fi
  case $PS1 in
    "${BONSAI_PROMPT_PREFIX-}"*) ;;
    *) PS1="${BONSAI_PROMPT_PREFIX-}$PS1" ;;
  esac
fi
