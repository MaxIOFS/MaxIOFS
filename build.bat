@echo off
REM MaxIOFS Build Script for Windows
REM This script builds MaxIOFS with embedded frontend

REM --- VERSION (can be overridden with environment variable) ---
IF "%VERSION%"=="" SET VERSION=dev

REM --- GET COMMIT FROM GIT ---
FOR /F %%i IN ('git rev-parse --short HEAD 2^>nul') DO SET COMMIT=%%i
IF "%COMMIT%"=="" SET COMMIT=unknown

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
if exist out rmdir /s /q out
echo Installing dependencies...
call npm ci --silent
if %errorlevel% neq 0 (
    echo Frontend dependencies installation failed!
    cd ..\..
    exit /b 1
)
echo Building Next.js static export...
set NODE_ENV=production
call npm run build
if %errorlevel% neq 0 (
    echo Frontend build failed!
    cd ..\..
    exit /b 1
)
REM Verify out directory was created
if not exist out (
    echo Error: Static export directory 'out' was not created!
    cd ..\..
    exit /b 1
)
cd ..\..
echo Frontend built successfully (static export in web\frontend\out)
echo.

REM --- STEP 2: BUILD BACKEND ---
echo [2/2] Building backend with embedded frontend...
if not exist build mkdir build
go build -buildvcs=false -ldflags "-X main.version=%VERSION% -X main.commit=%COMMIT% -X main.date=%BUILD_DATE%" -o build\maxiofs.exe ./cmd/maxiofs

REM --- CHECK BUILD RESULT ---
if %errorlevel% equ 0 (
    echo.
    echo ========================================
    echo Build successful!
    echo ========================================
    echo Binary: build\maxiofs.exe
    echo Version: %VERSION% ^(commit: %COMMIT%^)
    echo Frontend: Embedded in binary
    echo.
    echo Usage:
    echo   build\maxiofs.exe --data-dir .\data
    echo   build\maxiofs.exe --version
    echo   build\maxiofs.exe --help
    echo.
    echo Endpoints:
    echo   Web Console: http://localhost:8081
    echo   S3 API:      http://localhost:8080
    echo.
    echo TLS Support ^(optional^):
    echo   build\maxiofs.exe --data-dir .\data --tls-cert cert.pem --tls-key key.pem
    exit /b 0
) else (
    echo.
    echo ========================================
    echo   BACKEND BUILD FAILED!
    echo ========================================
    exit /b 1
)
