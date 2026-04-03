@echo off

echo === go build . ===
go build .
if %errorlevel% neq 0 (
    echo BUILD FAILED
    pause
    exit /b 1
)

echo === go install . ===
go install .
if %errorlevel% neq 0 (
    echo INSTALL FAILED
    pause
    exit /b 1
)

echo === Done ===
pause
