# zpit-env.ps1 — Sets ZPIT_AGENT=1 / ZPIT_AGENT_TYPE=<role> and forwards to the command.
# Usage: pwsh -NoProfile -File .claude\hooks\zpit-env.ps1 <role> <command> [args...]
$env:ZPIT_AGENT = "1"
if ($args.Count -lt 2) { exit 1 }
$env:ZPIT_AGENT_TYPE = $args[0]
$cmd = $args[1]
$cmdArgs = @()
if ($args.Count -gt 2) { $cmdArgs = $args[2..($args.Count - 1)] }
& $cmd @cmdArgs
# Always exit 0 so Windows Terminal closes the tab (closeOnExit: graceful).
exit 0
