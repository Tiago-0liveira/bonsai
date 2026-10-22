# bonsai prompt hook (fish): wraps fish_prompt so $BONSAI_PROMPT_PREFIX prints
# first. The guard is a global (not exported) variable, so it is per process.
if not set -q __BONSAI_PROMPT_INSTALLED
    set -g __BONSAI_PROMPT_INSTALLED 1
    if functions -q fish_prompt
        functions -c fish_prompt __bonsai_orig_fish_prompt
        function __bonsai_restore_status
            return $argv[1]
        end
        function fish_prompt
            set -l last_status $status
            if test -n "$BONSAI_PROMPT_PREFIX"
                printf '%s' $BONSAI_PROMPT_PREFIX
            end
            # The wrapped prompt still needs to see the real $status.
            __bonsai_restore_status $last_status
            __bonsai_orig_fish_prompt
        end
    end
end
