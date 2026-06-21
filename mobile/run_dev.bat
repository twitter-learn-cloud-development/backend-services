@echo off
echo ===================================================
echo   Twitter Clone Mobile Port Forwarding (ADB)
echo ===================================================
echo [1/2] Setting up API Gateway reverse port: 9638 -^> 9638
adb reverse tcp:9638 tcp:9638
if %errorlevel% neq 0 (
    echo [WARNING] adb reverse tcp:9638 failed. Please check if your device is connected and USB debugging is enabled.
) else (
    echo [SUCCESS] API Gateway port forwarded successfully.
)

echo [2/2] Setting up MinIO Media Server reverse port: 9000 -^> 9000
adb reverse tcp:9000 tcp:9000
if %errorlevel% neq 0 (
    echo [WARNING] adb reverse tcp:9000 failed.
) else (
    echo [SUCCESS] MinIO port forwarded successfully.
)
echo ===================================================
echo Starting Flutter App using config_dev_web.json configuration...
flutter run --dart-define-from-file=config_dev_web.json
