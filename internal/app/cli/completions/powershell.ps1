Register-ArgumentCompleter -CommandName cautem -ScriptBlock {
    param($wordToComplete)

    @(
        'version', 'sandbox', 'sb', 'exec', 'provider', 'profile', 'policy', 'pol',
        'gateway', 'gw', 'logs', 'lg', 'term', 'status', 'health', 'init', 'doctor', 'dr',
        'whoami', 'workspace', 'ws', 'forward', 'fwd', 'service', 'svc', 'settings',
        'inference', 'rule', 'rl', 'install', 'completions', 'ssh-proxy', 'cp'
    ) |
        Where-Object { $_ -like "$wordToComplete*" } |
        ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
}
