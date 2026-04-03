@echo off

echo === go build . ===
go build .
if %errorlevel% neq 0 (
    echo BUILD FAILED
    exit /b %errorlevel%
)

echo === go install . ===
go install .
if %errorlevel% neq 0 (
    echo INSTALL FAILED
    exit /b %errorlevel%
)

echo === Done ===
pause
