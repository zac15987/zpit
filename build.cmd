@echo off

echo === git fetch ===
git fetch -v
if %errorlevel% neq 0 (
    echo FETCH FAILED
    pause
    exit /b 1
)

echo === git pull ===
git pull -v
if %errorlevel% neq 0 (
    echo PULL FAILED
    pause
    exit /b 1
)

for /f "delims=" %%v in ('git describe --tags --always --dirty 2^>nul') do set "GIT_VERSION=%%v"
if not defined GIT_VERSION set "GIT_VERSION=dev"
echo === version: %GIT_VERSION% ===

echo === go build . ===
go build -v -ldflags "-X main.version=%GIT_VERSION%" .
if %errorlevel% neq 0 (
    echo BUILD FAILED
    pause
    exit /b 1
)

echo === go install . ===
go install -v -ldflags "-X main.version=%GIT_VERSION%" .
if %errorlevel% neq 0 (
    echo INSTALL FAILED
    pause
    exit /b 1
)

echo === Done ===
pause
