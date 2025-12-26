package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run jwt_gen.go <user_id>")
		return
	}

	userIDStr := os.Args[1]
	userID, _ := strconv.ParseUint(userIDStr, 10, 64)

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "mysecret" // Default used in local dev
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		panic(err)
	}

	fmt.Println(tokenString)
}
