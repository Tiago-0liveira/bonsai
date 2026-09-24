@echo off
:: bcd — change directory into a bonsai worktree from Windows CMD
:: Usage:
::   bcd <branch>
::   bcd main
if "%~1"=="" (
    for /f "delims=" %%i in ('bonsai path main') do (
        if not "%%i"=="" cd /d "%%i"
    )
) else (
    for /f "delims=" %%i in ('bonsai path %*') do (
        if not "%%i"=="" cd /d "%%i"
    )
)
