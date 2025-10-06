@echo off
REM MaxIOFS Build Script for Windows
REM This script builds MaxIOFS with embedded frontend

REM --- VERSION MANUAL ---
SET VERSION=v1.1.0

REM --- GET COMMIT FROM GIT ---
FOR /F %%i IN ('git rev-parse --short HEAD') DO SET COMMIT=%%i

REM --- GET BUILD DATE (robusto, formato ISO 8601) ---
FOR /F "tokens=1 delims=." %%A IN ('wmic os get localdatetime ^| find "."') DO SET ldt=%%A
SET BUILD_DATE=%ldt:~0,4%-%ldt:~4,2%-%ldt:~6,2%T%ldt:~8,2%:%ldt:~10,2%:%ldt:~12,2%Z

echo ========================================
echo Building MaxIOFS %VERSION%
echo ========================================
echo Commit: %COMMIT%
echo Build Date: %BUILD_DATE%
echo.

REM --- STEP 1: BUILD FRONTEND ---
echo [1/2] Building frontend...
cd web\frontend
if exist .next rmdir /s /q .next
set NODE_ENV=production
call npm install --silent
call npm run build
if %errorlevel% neq 0 (
    echo Frontend build failed!
    cd ..\..
    exit /b 1
)
cd ..\..
echo Frontend built successfully!
echo.

REM --- STEP 2: BUILD BACKEND ---
echo [2/2] Building backend with embedded frontend...
go build -ldflags "-X main.version=%VERSION% -X main.commit=%COMMIT% -X main.date=%BUILD_DATE%" -o maxiofs.exe ./cmd/maxiofs

REM --- CHECK BUILD RESULT ---
if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo Build successful!
    echo ========================================
    echo Binary: maxiofs.exe
    echo Frontend: Embedded in binary
    echo.
    echo To run: maxiofs.exe --data-dir .\data
    echo Version: maxiofs.exe --version
    echo.
    echo Web Console: http://localhost:8081
    echo S3 API:      http://localhost:8080
) else (
    echo.
    echo Backend build failed!
    exit /b 1
)
