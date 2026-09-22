@echo off
setlocal enabledelayedexpansion

:: bonsai Windows installer — builds the binary and installs it onto your PATH.
::
:: Usage:
::   install.bat                       # install to %USERPROFILE%\go\bin
::   set "PREFIX=C:\custom" & install.bat   # install to %PREFIX%\bin

set "BINARY=bonsai.exe"
set "SRC=."

:: Resolve repo root
set "SCRIPT_DIR=%~dp0"
cd /d "%SCRIPT_DIR%"

where go >nul 2>&1
if errorlevel 1 (
    echo [error] Go is required but not found on your PATH. See https://go.dev/dl/
    exit /b 1
)

:: Pick install directory: PREFIX\bin, GOBIN, GOPATH\bin, or %USERPROFILE%\go\bin
if not "%PREFIX%"=="" (
    set "PREFIX_CLEAN=%PREFIX%"
    call :trim_prefix
    set "BINDIR=!PREFIX_CLEAN!\bin"
) else if not "%GOBIN%"=="" (
    set "BINDIR=%GOBIN%"
) else if not "%GOPATH%"=="" (
    set "BINDIR=%GOPATH%\bin"
) else (
    set "BINDIR=%USERPROFILE%\go\bin"
)

if not exist "%BINDIR%" (
    mkdir "%BINDIR%"
)

echo [info] Building %BINARY%...
set "TMPDIR=%TEMP%\bonsai_build_%RANDOM%"
mkdir "%TMPDIR%" 2>nul

go build -o "%TMPDIR%\%BINARY%" "%SRC%"
if errorlevel 1 (
    echo [error] Build failed.
    rmdir /s /q "%TMPDIR%" 2>nul
    exit /b 1
)

echo [info] Installing to %BINDIR%\%BINARY%...
copy /y "%TMPDIR%\%BINARY%" "%BINDIR%\%BINARY%" >nul
if errorlevel 1 (
    echo [error] Failed to copy %BINARY% to %BINDIR%
    rmdir /s /q "%TMPDIR%" 2>nul
    exit /b 1
)
rmdir /s /q "%TMPDIR%" 2>nul

echo [info] Installed %BINARY% to %BINDIR%

:: Copy bcd.bat helper if present
if exist "%SCRIPT_DIR%bcd.bat" (
    copy /y "%SCRIPT_DIR%bcd.bat" "%BINDIR%\bcd.bat" >nul
)

:: Check if BINDIR is in PATH
echo ;%PATH%; | find /i ";%BINDIR%;" >nul 2>&1
if errorlevel 1 (
    echo.
    echo [warn] %BINDIR% is not on your PATH. Add it to your PATH via:
    echo     setx PATH "%%PATH%%;%BINDIR%"
)

echo.
echo Done. Try it:
echo.
echo   cd your-repo
echo   bonsai
echo.
echo Optional — enable 'bcd' to cd into worktrees from CMD:
echo   (bcd.bat has been installed to %BINDIR%)
echo.
exit /b 0

:trim_prefix
if "!PREFIX_CLEAN:~-1!"==" " (
    set "PREFIX_CLEAN=!PREFIX_CLEAN:~0,-1!"
    goto trim_prefix
)
if "!PREFIX_CLEAN:~-1!"=="\" (
    set "PREFIX_CLEAN=!PREFIX_CLEAN:~0,-1!"
    goto trim_prefix
)
exit /b 0

