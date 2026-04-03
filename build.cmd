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

echo === go build . ===
go build -v .
if %errorlevel% neq 0 (
    echo BUILD FAILED
    pause
    exit /b 1
)

echo === go install . ===
go install -v .
if %errorlevel% neq 0 (
    echo INSTALL FAILED
    pause
    exit /b 1
)

echo === Done ===
pause
