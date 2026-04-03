@echo off

echo === git fetch ===
git fetch
if %errorlevel% neq 0 (
    echo FETCH FAILED
    pause
    exit /b 1
)

echo === git pull ===
git pull
if %errorlevel% neq 0 (
    echo PULL FAILED
    pause
    exit /b 1
)

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
