# zpit-exit.ps1 — Runs the specified command and exits 0 for WT tab auto-close.
if ($args.Count -eq 0) { exit 0 }
$cmd = $args[0]
$cmdArgs = @()
if ($args.Count -gt 1) { $cmdArgs = $args[1..($args.Count - 1)] }
& $cmd @cmdArgs
# Always exit 0 so Windows Terminal closes the tab (closeOnExit: graceful).
exit 0
