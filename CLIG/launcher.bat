@echo off
:menu
cls
echo CLIG Fallback Launcher
echo ======================
echo 1. 2048
echo 2. Snake
echo 3. Tetris
echo 4. Type
echo Q. Quit
echo.

set /p choice="Select a game: "

if "%choice%"=="1" goto run_2048
if "%choice%"=="2" goto run_snake
if "%choice%"=="3" goto run_tetris
if "%choice%"=="4" goto run_type
if /i "%choice%"=="q" exit

goto menu

:run_2048
cd 2048
if exist "2048.exe" (
    2048.exe
) else (
    echo Game executable not found. Run setup.bat first.
    pause
)
cd ..
goto menu

:run_snake
cd snake
if exist "snake.exe" (
    snake.exe
) else (
    echo Game executable not found. Run setup.bat first.
    pause
)
cd ..
goto menu

:run_tetris
cd tetris
if exist "tetris.exe" (
    tetris.exe
) else (
    echo Game executable not found. Run setup.bat first.
    pause
)
cd ..
goto menu

:run_type
cd type
if exist "type.exe" (
    type.exe
) else (
    echo Game executable not found. Run setup.bat first.
    pause
)
cd ..
goto menu


