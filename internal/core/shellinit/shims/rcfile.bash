# bonsai shell integration: loaded via `bash --rcfile`, so run the user's own
# rc first and add the depth prefix last.
[ -r ~/.bashrc ] && . ~/.bashrc
. "${BASH_SOURCE%/*}/hook.bash"
