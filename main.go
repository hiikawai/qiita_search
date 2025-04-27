package main

import (
	"log"
	"net/http"
	"os"

	"qiita-search/controllers"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// カレントディレクトリを表示
	dir, err := os.Getwd()
	if err != nil {
		log.Printf("Error getting current directory: %v", err)
	}
	log.Printf("Current directory: %s", dir)

	// .envファイルの存在確認
	if _, err := os.Stat(".env"); err != nil {
		log.Printf(".env file not found in current directory: %v", err)
	} else {
		log.Printf(".env file found in current directory")
	}

	// .envファイルを読み込む
	if err := godotenv.Load(); err != nil {
		log.Printf("Error loading .env file: %v", err)
	} else {
		log.Printf("Successfully loaded .env file")
	}

	// 環境変数の確認
	log.Printf("Environment variables:")
	log.Printf("GO_ENV: %s", os.Getenv("GO_ENV"))
	log.Printf("SUPABASE_URL: %s", os.Getenv("SUPABASE_URL"))
	log.Printf("SUPABASE_KEY exists: %v", os.Getenv("SUPABASE_KEY") != "")
	log.Printf("CHATWORK_TOKEN exists: %v", os.Getenv("CHATWORK_API_TOKEN") != "")

	e := echo.New()

	// ミドルウェアの設定
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// コントローラーのインスタンスを作成
	articleController := controllers.NewArticleController()
	userController := controllers.NewUserController()

	// ルーティングの設定
	e.GET("/", articleController.Index)
	e.GET("/register", userController.Register)
	e.GET("/save", articleController.SaveArticle)
	e.POST("/save", articleController.SaveArticle)

	e.HEAD("/keepalive", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	// サーバーの起動
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	e.Logger.Fatal(e.Start(":" + port))
}
