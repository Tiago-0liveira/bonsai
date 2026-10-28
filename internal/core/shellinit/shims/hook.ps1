# bonsai prompt hook (PowerShell): wraps `prompt`. The guard is a global
# variable, not an env var, so it is per process.
if (-not (Test-Path variable:global:__BONSAI_PROMPT_INSTALLED)) {
    $global:__BONSAI_PROMPT_INSTALLED = $true
    $global:__bonsai_orig_prompt = $function:prompt
    function global:prompt {
        $p = $env:BONSAI_PROMPT_PREFIX
        $o = & $global:__bonsai_orig_prompt
        "$p$o"
    }
}
