@echo off
setlocal EnableDelayedExpansion

echo CLIG Windows Setup
echo ==================

REM Check for GCC
where gcc >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo [ERROR] GCC not found. Please install MinGW or another C compiler and add it to your PATH.
    pause
    exit /b 1
)
echo [OK] GCC found

REM Compiler flags - adjust finding pdcurses if needed
set CFLAGS=-Wall -Wextra
set LDFLAGS=-lpdcurses

REM List of games to compile
set "GAMES=2048 snake tetris type"

echo.
echo Compiling Games...
echo ==================

for %%g in (%GAMES%) do (
    if exist "%%g\main.c" (
        echo Building %%g...
        pushd %%g
        gcc %CFLAGS% main.c -o %%g.exe %LDFLAGS%
        if !ERRORLEVEL! neq 0 (
            echo [X] Failed to build %%g
        ) else (
            echo [V] Built %%g
        )
        popd
    ) else (
        echo [?] Skipping %%g ^(main.c not found^)
    )
)

echo.
echo Compiling Launcher...
echo =====================
if exist "launcher.c" (
    gcc %CFLAGS% launcher.c -o launcher.exe %LDFLAGS%
    if %ERRORLEVEL% neq 0 (
        echo [X] Failed to build launcher
        echo     Ensure you have PDCurses installed and linked correctly.
        echo     Example: gcc launcher.c -o launcher.exe -lpdcurses
    ) else (
        echo [V] Built launcher.exe
    )
) else (
    echo [ERROR] launcher.c not found!
)

echo.
echo Setup Complete.
echo Run launcher.exe to start.
pause
