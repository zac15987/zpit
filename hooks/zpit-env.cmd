@echo off
rem zpit-env.cmd — Sets ZPIT_AGENT=1 / ZPIT_AGENT_TYPE=<role> and forwards to the command.
rem Usage: zpit-env.cmd <role> <command> [args...]
set ZPIT_AGENT=1
set "ZPIT_AGENT_TYPE=%~1"
for /f "tokens=1,*" %%a in ("%*") do set "REST=%%b"
%REST%
rem Always exit 0 so Windows Terminal closes the tab (closeOnExit: graceful).
exit /b 0
