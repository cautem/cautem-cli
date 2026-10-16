# cautem bash completions
_cautem() {
    local current_word="${COMP_WORDS[COMP_CWORD]}"
    local -a commands=(
        version sandbox sb cp exec provider profile policy pol gateway gw logs lg term status
        health init doctor dr whoami workspace ws forward fwd service svc settings
        inference rule rl install completions ssh-proxy
    )

    COMPREPLY=()
    if (( COMP_CWORD == 1 )); then
        # Command substitution keeps this compatible with macOS's Bash 3.2.
        COMPREPLY=( $(compgen -W "${commands[*]}" -- "$current_word") )
    fi
}

complete -F _cautem cautem
