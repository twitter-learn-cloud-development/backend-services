@echo off
echo ========================================
echo Generating gRPC code from .proto files
echo ========================================

REM 创建输出目录
if not exist "api\tweet\v1" mkdir "api\tweet\v1"
if not exist "api\user\v1" mkdir "api\user\v1"
if not exist "api\follow\v1" mkdir "api\follow\v1"

REM 生成 Tweet Service
echo.
echo Generating Tweet Service...
protoc --go_out=. --go_opt=paths=source_relative ^
       --go-grpc_out=. --go-grpc_opt=paths=source_relative ^
       api/tweet/v1/tweet.proto

if %errorlevel% neq 0 (
    echo ERROR: Failed to generate Tweet Service
    exit /b 1
)
echo ✅ Tweet Service generated

REM 生成 User Service
echo.
echo Generating User Service...
protoc --go_out=. --go_opt=paths=source_relative ^
       --go-grpc_out=. --go-grpc_opt=paths=source_relative ^
       api/user/v1/user.proto

if %errorlevel% neq 0 (
    echo ERROR: Failed to generate User Service
    exit /b 1
)
echo ✅ User Service generated

REM 生成 Follow Service
echo.
echo Generating Follow Service...
protoc --go_out=. --go_opt=paths=source_relative ^
       --go-grpc_out=. --go-grpc_opt=paths=source_relative ^
       api/follow/v1/follow.proto

if %errorlevel% neq 0 (
    echo ERROR: Failed to generate Follow Service
    exit /b 1
)
echo ✅ Follow Service generated

echo.
echo ========================================
echo ✅ All gRPC code generated successfully!
echo ========================================
echo.
echo Generated files:
echo   api/tweet/v1/tweet.pb.go
echo   api/tweet/v1/tweet_grpc.pb.go
echo   api/user/v1/user.pb.go
echo   api/user/v1/user_grpc.pb.go
echo   api/follow/v1/follow.pb.go
echo   api/follow/v1/follow_grpc.pb.go
echo ========================================