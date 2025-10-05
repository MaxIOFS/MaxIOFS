@echo off
REM MaxIOFS Build Script for Windows
REM This script builds MaxIOFS with proper version information

SET VERSION=v1.1.0
SET COMMIT=ec1cecb

FOR /F "tokens=1 delims=." %%A IN ('wmic os get localdatetime ^| find "."') DO SET ldt=%%A
SET BUILD_DATE=%ldt:~0,4%-%ldt:~4,2%-%ldt:~6,2%T%ldt:~8,2%:%ldt:~10,2%:%ldt:~12,2%Z

echo Building MaxIOFS %VERSION%...
echo Commit: %COMMIT%
echo Build Date: %BUILD_DATE%
echo.

go build -ldflags "-X main.version=%VERSION% -X main.commit=%COMMIT% -X main.date=%BUILD_DATE%" -o maxiofs.exe ./cmd/maxiofs

if %errorlevel% equ 0 (
    echo.
    echo Build successful!
    echo Binary: maxiofs.exe
    echo.
    echo To run: maxiofs.exe --data-dir .\data
    echo Version info: maxiofs.exe --version
) else (
    echo.
    echo Build failed!
    exit /b 1
)
