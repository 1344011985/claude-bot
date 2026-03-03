@echo off
chcp 65001 >nul
echo 正在创建每日早间推送定时任务...

:: 删除已存在的任务
schtasks /delete /tn "ClaudeBotDailyPush" /f 2>nul

:: 创建新任务 - 每天早上 8:00 执行
schtasks /create /tn "ClaudeBotDailyPush" ^
    /tr "python D:\myselfClow\scripts\daily_morning_push.py" ^
    /sc daily ^
    /st 08:00 ^
    /ru "%USERNAME%" ^
    /rl highest ^
    /f

if %errorlevel% equ 0 (
    echo.
    echo ✅ 定时任务创建成功！
    echo.
    echo 任务名称: ClaudeBotDailyPush
    echo 执行时间: 每天 08:00
    echo 执行脚本: D:\myselfClow\scripts\daily_morning_push.py
    echo.
    echo 查看任务: schtasks /query /tn "ClaudeBotDailyPush"
    echo 手动执行: schtasks /run /tn "ClaudeBotDailyPush"
    echo 删除任务: schtasks /delete /tn "ClaudeBotDailyPush" /f
) else (
    echo.
    echo ❌ 任务创建失败，请以管理员权限运行此脚本
)

pause
