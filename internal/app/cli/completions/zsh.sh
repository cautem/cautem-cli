#compdef cautem
_cautem() {
    local -a commands=(
        version sandbox sb cp exec provider profile policy pol gateway gw logs lg term status
        health init doctor dr whoami workspace ws forward fwd service svc settings
        inference rule rl install completions ssh-proxy
    )

    _describe 'command' commands
}

compdef _cautem cautem
