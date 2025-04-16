package main

import (
	"log"
	"os"

	"qiita-search/controllers"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 開発環境でのみ.envを読み込む
	if os.Getenv("GO_ENV") == "development" {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	e := echo.New()

	// ミドルウェアの設定
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// テンプレートレンダラーの設定
	e.Renderer = controllers.NewTemplate()

	// コントローラーのインスタンスを作成
	articleController := controllers.NewArticleController()
	userController := controllers.NewUserController()

	// ルーティングの設定
	e.GET("/", articleController.Index)
	e.GET("/register", userController.Index)

	// サーバーの起動
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	e.Logger.Fatal(e.Start(":" + port))
}
