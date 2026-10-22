# bonsai prompt hook (nu): wraps PROMPT_COMMAND. nu env vars are inherited, so
# the guard is keyed to BONSAI_DEPTH: a nested bonsai shell has a new depth.
if ($env.__BONSAI_PROMPT_INSTALLED? | default "") != ($env.BONSAI_DEPTH? | default "") {
    $env.__BONSAI_PROMPT_INSTALLED = ($env.BONSAI_DEPTH? | default "")
    let orig = ($env.PROMPT_COMMAND? | default "")
    $env.PROMPT_COMMAND = {||
        let prefix = ($env.BONSAI_PROMPT_PREFIX? | default "")
        let body = if ($orig | describe | str starts-with "closure") { do $orig } else { $orig }
        $"($prefix)($body)"
    }
}
