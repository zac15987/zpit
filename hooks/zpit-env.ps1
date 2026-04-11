# zpit-env.ps1 — Sets ZPIT_AGENT=1 and forwards to the specified command.
# Usage: pwsh -NoProfile -File .claude\hooks\zpit-env.ps1 <command> [args...]
$env:ZPIT_AGENT = "1"
if ($args.Count -eq 0) { exit 1 }
$cmd = $args[0]
$cmdArgs = @()
if ($args.Count -gt 1) { $cmdArgs = $args[1..($args.Count - 1)] }
& $cmd @cmdArgs
# Always exit 0 so Windows Terminal closes the tab (closeOnExit: graceful).
exit 0
