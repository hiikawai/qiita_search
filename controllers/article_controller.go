package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"qiita-search/models"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

type ArticleController struct{}

func NewArticleController() *ArticleController {
	rand.Seed(time.Now().UnixNano())
	return &ArticleController{}
}

func (ac *ArticleController) Index(c echo.Context) error {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")
	chatworkToken := os.Getenv("CHATWORK_API_TOKEN")

	userReq, err := http.NewRequest("GET", supabaseURL+"/rest/v1/user?select=room_id", nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "ユーザー情報の取得に失敗しました",
		})
	}
	userReq.Header.Set("apikey", supabaseKey)
	userReq.Header.Set("Authorization", "Bearer "+supabaseKey)
	userReq.Header.Set("Content-Type", "application/json")

	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "ユーザー情報の取得に失敗しました",
		})
	}
	defer userResp.Body.Close()

	userBody, err := io.ReadAll(userResp.Body)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "ユーザー情報の取得に失敗しました",
		})
	}

	var users []struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(userBody, &users); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "ユーザー情報の解析に失敗しました",
		})
	}

	if len(users) == 0 {
		return c.JSON(http.StatusOK, map[string]interface{}{"message": "登録されているユーザーがいません"})
	}

	fieldReq, err := http.NewRequest("GET", supabaseURL+"/rest/v1/field?select=room_id,field_name,priority", nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "分野情報の取得に失敗しました",
		})
	}
	fieldReq.Header.Set("apikey", supabaseKey)
	fieldReq.Header.Set("Authorization", "Bearer "+supabaseKey)
	fieldReq.Header.Set("Content-Type", "application/json")

	fieldResp, err := http.DefaultClient.Do(fieldReq)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "分野情報の取得に失敗しました",
		})
	}
	defer fieldResp.Body.Close()

	fieldBody, err := io.ReadAll(fieldResp.Body)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "分野情報の取得に失敗しました",
		})
	}

	var fields []struct {
		RoomID    string `json:"room_id"`
		FieldName string `json:"field_name"`
		Priority  int    `json:"priority"`
	}
	if err := json.Unmarshal(fieldBody, &fields); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"message": "分野情報の解析に失敗しました",
		})
	}

	// ルームIDごとに分野と優先度をマッピング
	type FieldInfo struct {
		Name     string
		Priority int
	}
	roomFields := make(map[string][]FieldInfo)
	for _, field := range fields {
		roomFields[field.RoomID] = append(roomFields[field.RoomID], FieldInfo{
			Name:     field.FieldName,
			Priority: field.Priority,
		})
	}

	for _, user := range users {
		if user.RoomID == "" {
			continue
		}

		fieldInfos, hasFields := roomFields[user.RoomID]
		var selectedField string
		var apiURL string
		var articles []models.Article

		if hasFields && len(fieldInfos) > 0 {
			// 優先度に基づく重み付け合計を計算
			totalWeight := 0
			for _, field := range fieldInfos {
				totalWeight += field.Priority
			}

			// 重み付けランダム選択
			randomNum := rand.Intn(totalWeight)
			currentWeight := 0
			for _, field := range fieldInfos {
				currentWeight += field.Priority
				if randomNum < currentWeight {
					selectedField = field.Name
					break
				}
			}

			foundNewArticle := false

			for page := 1; page <= 4; page++ {
				// 単語を分割
				searchWords := strings.Fields(selectedField)

				if len(searchWords) > 1 {
					// 複数単語の場合（例：cursor rules）
					// すべてのタグを含む記事を検索（AND検索）
					tagQueries := make([]string, len(searchWords))
					for i, word := range searchWords {
						tagQueries[i] = fmt.Sprintf("tag:%s", url.QueryEscape(word))
					}
					apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30+%s",
						page,
						strings.Join(tagQueries, "+")) // + はAND条件

				} else {
					// 単一単語の場合（既存の検索方法）
					apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30+tag:%s",
						page,
						url.QueryEscape(selectedField))
				}

				articles, err = ac.searchArticles(apiURL)
				if err != nil {
					continue
				}

				if len(articles) == 0 {
					break
				}

				// 履歴チェックと記事の保存
				for _, article := range articles {
					historyReq, err := http.NewRequest("GET",
						fmt.Sprintf("%s/rest/v1/article_history?article_url=eq.%s&room_id=eq.%s",
							supabaseURL,
							url.QueryEscape(article.URL),
							url.QueryEscape(user.RoomID)),
						nil)
					if err != nil {
						continue
					}

					historyReq.Header.Set("apikey", supabaseKey)
					historyReq.Header.Set("Authorization", "Bearer "+supabaseKey)
					historyReq.Header.Set("Content-Type", "application/json")

					historyResp, err := http.DefaultClient.Do(historyReq)
					if err != nil {
						continue
					}
					defer historyResp.Body.Close()

					var history []struct{}
					if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
						continue
					}

					if len(history) == 0 {
						foundNewArticle = true
						articles = []models.Article{article}
						break
					}
				}

				if foundNewArticle {
					break
				}
			}

			if !foundNewArticle {
				for page := 1; page <= 4; page++ {
					// 単語を分割
					searchWords := strings.Fields(selectedField)
					var apiURL string
					if len(searchWords) > 1 {
						titleQueries := make([]string, len(searchWords))
						for i, word := range searchWords {
							titleQueries[i] = fmt.Sprintf("title:%s", url.QueryEscape(word))
						}
						apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30+%s",
							page,
							strings.Join(titleQueries, "+"))
					} else {
						apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30+title:%s",
							page,
							url.QueryEscape(selectedField))
					}
					articles, err = ac.searchArticles(apiURL)
					if err != nil {
						continue
					}

					if len(articles) == 0 {
						break
					}

					for _, article := range articles {
						historyReq, err := http.NewRequest("GET",
							fmt.Sprintf("%s/rest/v1/article_history?article_url=eq.%s&room_id=eq.%s",
								supabaseURL,
								url.QueryEscape(article.URL),
								url.QueryEscape(user.RoomID)),
							nil)
						if err != nil {
							continue
						}

						historyReq.Header.Set("apikey", supabaseKey)
						historyReq.Header.Set("Authorization", "Bearer "+supabaseKey)
						historyReq.Header.Set("Content-Type", "application/json")

						historyResp, err := http.DefaultClient.Do(historyReq)
						if err != nil {
							continue
						}
						defer historyResp.Body.Close()

						var history []struct{}
						if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
							continue
						}

						if len(history) == 0 {
							foundNewArticle = true
							articles = []models.Article{article}
							break
						}
					}

					if foundNewArticle {
						break
					}
				}
			}

			if !foundNewArticle {
				deleteReq, err := http.NewRequest("DELETE",
					fmt.Sprintf("%s/rest/v1/field?room_id=eq.%s&field_name=eq.%s",
						supabaseURL,
						url.QueryEscape(user.RoomID),
						url.QueryEscape(selectedField)),
					nil)
				if err != nil {
					continue
				}

				deleteReq.Header.Set("apikey", supabaseKey)
				deleteReq.Header.Set("Authorization", "Bearer "+supabaseKey)

				deleteResp, err := http.DefaultClient.Do(deleteReq)
				if err != nil {
					continue
				}
				defer deleteResp.Body.Close()

				hasFields = false

				foundNewArticle := false
				for page := 1; page <= 4; page++ {
					apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30", page)
					articles, err = ac.searchArticles(apiURL)
					if err != nil {
						continue
					}

					if len(articles) == 0 {
						break
					}

					for _, article := range articles {
						historyReq, err := http.NewRequest("GET",
							fmt.Sprintf("%s/rest/v1/article_history?article_url=eq.%s&room_id=eq.%s",
								supabaseURL,
								url.QueryEscape(article.URL),
								url.QueryEscape(user.RoomID)),
							nil)
						if err != nil {
							continue
						}

						historyReq.Header.Set("apikey", supabaseKey)
						historyReq.Header.Set("Authorization", "Bearer "+supabaseKey)
						historyReq.Header.Set("Content-Type", "application/json")

						historyResp, err := http.DefaultClient.Do(historyReq)
						if err != nil {
							continue
						}
						defer historyResp.Body.Close()

						var history []struct{}
						if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
							continue
						}

						if len(history) == 0 {
							foundNewArticle = true
							articles = []models.Article{article}
							break
						}
					}

					if foundNewArticle {
						break
					}
				}

				if !foundNewArticle {
					continue
				}
			}
		} else {
			foundNewArticle := false
			for page := 1; page <= 4; page++ {
				apiURL = fmt.Sprintf("https://qiita.com/api/v2/items?per_page=30&page=%d&query=stocks:>=30", page)
				articles, err = ac.searchArticles(apiURL)
				if err != nil {
					continue
				}

				if len(articles) == 0 {
					break
				}

				for _, article := range articles {
					historyReq, err := http.NewRequest("GET",
						fmt.Sprintf("%s/rest/v1/article_history?article_url=eq.%s&room_id=eq.%s",
							supabaseURL,
							url.QueryEscape(article.URL),
							url.QueryEscape(user.RoomID)),
						nil)
					if err != nil {
						continue
					}

					historyReq.Header.Set("apikey", supabaseKey)
					historyReq.Header.Set("Authorization", "Bearer "+supabaseKey)
					historyReq.Header.Set("Content-Type", "application/json")

					historyResp, err := http.DefaultClient.Do(historyReq)
					if err != nil {
						continue
					}
					defer historyResp.Body.Close()

					var history []struct{}
					if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
						continue
					}

					if len(history) == 0 {
						foundNewArticle = true
						articles = []models.Article{article}
						break
					}
				}

				if foundNewArticle {
					break
				}
			}

			if !foundNewArticle {
				continue
			}
		}

		if len(articles) == 0 {
			continue
		}

		if err := articles[0].Summarize(); err != nil {
			continue
		}

		tags := make([]string, len(articles[0].Tags))
		for i, tag := range articles[0].Tags {
			tags[i] = tag.Name
		}
		tagMessage := ""
		if len(tags) > 0 {
			tagMessage = "\nタグ: " + strings.Join(tags, ", ")
		}

		message := "本日の記事"
		if hasFields && len(fieldInfos) > 0 {
			message = fmt.Sprintf("「%s」の記事", selectedField)
		}

		// 最初のメッセージを送信
		initialMessage := fmt.Sprintf("[info][title]%s[/title]%s\n%s\n\n%s%s[/info]",
			message,
			articles[0].Title,
			articles[0].URL,
			articles[0].Summary,
			tagMessage)

		formData := url.Values{}
		formData.Set("body", initialMessage)

		chatworkURL := fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages", user.RoomID)

		chatworkReq, err := http.NewRequest("POST", chatworkURL, strings.NewReader(formData.Encode()))
		if err != nil {
			continue
		}
		chatworkReq.Header.Set("X-ChatWorkToken", chatworkToken)
		chatworkReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		chatworkResp, err := http.DefaultClient.Do(chatworkReq)
		if err != nil {
			continue
		}
		defer chatworkResp.Body.Close()

		// レスポンスからメッセージIDを取得
		var messageResponse struct {
			MessageID string `json:"message_id"`
		}
		if err := json.NewDecoder(chatworkResp.Body).Decode(&messageResponse); err != nil {
			continue
		}

		// 保存リンクを含むメッセージを送信
		baseURL := os.Getenv("BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:8080" // デフォルト値
		}
		saveLinkMessage := fmt.Sprintf("保存する場合は以下のリンクをクリック:\n%s/save?room_id=%s&message_id=%s",
			baseURL,
			url.QueryEscape(user.RoomID),
			url.QueryEscape(messageResponse.MessageID))

		formData = url.Values{}
		formData.Set("body", saveLinkMessage)

		chatworkReq, err = http.NewRequest("POST", chatworkURL, strings.NewReader(formData.Encode()))
		if err != nil {
			continue
		}
		chatworkReq.Header.Set("X-ChatWorkToken", chatworkToken)
		chatworkReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		chatworkResp, err = http.DefaultClient.Do(chatworkReq)
		if err != nil {
			continue
		}
		defer chatworkResp.Body.Close()

		historyData := map[string]interface{}{
			"article_url": articles[0].URL,
			"room_id":     user.RoomID,
		}
		historyJSON, err := json.Marshal(historyData)
		if err != nil {
			continue
		}

		historyPostReq, err := http.NewRequest("POST",
			supabaseURL+"/rest/v1/article_history",
			strings.NewReader(string(historyJSON)))
		if err != nil {
			continue
		}

		historyPostReq.Header.Set("apikey", supabaseKey)
		historyPostReq.Header.Set("Authorization", "Bearer "+supabaseKey)
		historyPostReq.Header.Set("Content-Type", "application/json")

		historyPostResp, err := http.DefaultClient.Do(historyPostReq)
		if err != nil {
			continue
		}
		defer historyPostResp.Body.Close()
	}

	return c.String(http.StatusOK, "処理が完了しました")
}

// 記事検索用のヘルパー関数
func (ac *ArticleController) searchArticles(apiURL string) ([]models.Article, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("API URLが指定されていません")
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("リクエストの作成に失敗しました: %v", err)
	}

	// Qiitaのアクセストークンを設定
	qiitaToken := os.Getenv("QIITA_ACCESS_TOKEN")
	if qiitaToken == "" {
		return nil, fmt.Errorf("QIITA_ACCESS_TOKENが設定されていません")
	}
	req.Header.Set("Authorization", "Bearer "+qiitaToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("APIリクエストに失敗しました: %v", err)
	}
	defer resp.Body.Close()

	// レスポンスボディを読み込む
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンスの読み込みに失敗しました: %v", err)
	}

	// レート制限エラーのチェック
	var rateLimitError struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(body, &rateLimitError); err == nil && rateLimitError.Type == "rate_limit_exceeded" {
		return nil, fmt.Errorf("Qiita APIのレート制限に達しました: %s", rateLimitError.Message)
	}

	// レスポンスが空の配列かどうかをチェック
	if string(body) == "[]" {
		return []models.Article{}, nil
	}

	// レスポンスが配列かオブジェクトかを判定
	var articles []models.Article
	if err := json.Unmarshal(body, &articles); err != nil {
		// 配列として解析できなかった場合、単一の記事として解析を試みる
		var article models.Article
		if err := json.Unmarshal(body, &article); err != nil {
			return nil, fmt.Errorf("レスポンスの解析に失敗しました: %v", err)
		}
		articles = []models.Article{article}
	}

	return articles, nil
}

// SaveArticle は記事を保存するハンドラー
func (ac *ArticleController) SaveArticle(c echo.Context) error {
	// パラメータの取得
	roomID := c.QueryParam("room_id")
	messageID := c.QueryParam("message_id")

	fmt.Printf("SaveArticle: room_id=%s, message_id=%s\n", roomID, messageID)

	// Chatworkの設定を取得
	chatworkToken := os.Getenv("CHATWORK_API_TOKEN")
	if chatworkToken == "" {
		fmt.Println("Error: CHATWORK_API_TOKEN is not set")
		return c.String(http.StatusInternalServerError, "CHATWORK_API_TOKENが設定されていません")
	}

	// Chatworkからメッセージを取得
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages/%s", roomID, messageID), nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return c.String(http.StatusInternalServerError, "リクエストの作成に失敗しました")
	}

	req.Header.Set("X-ChatWorkToken", chatworkToken)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error getting message: %v\n", err)
		return c.String(http.StatusInternalServerError, "メッセージの取得に失敗しました")
	}
	defer resp.Body.Close()

	fmt.Printf("Chatwork API response: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Chatwork API error response: %s\n", string(body))
		return c.String(http.StatusInternalServerError, "Chatwork APIからのレスポンスが不正です")
	}

	// レスポンスを解析
	var message struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		fmt.Printf("Error parsing message: %v\n", err)
		return c.String(http.StatusInternalServerError, "メッセージの解析に失敗しました")
	}

	// Supabaseの設定を取得
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		fmt.Println("Error: Supabase credentials are not set")
		return c.String(http.StatusInternalServerError, "Supabaseの設定が完了していません")
	}

	// reserve_articleテーブルに保存
	articleData := map[string]interface{}{
		"room_id": roomID,
		"content": message.Body,
	}
	articleJSON, err := json.Marshal(articleData)
	if err != nil {
		fmt.Printf("Error marshaling article data: %v\n", err)
		return c.String(http.StatusInternalServerError, "データの作成に失敗しました")
	}

	articleReq, err := http.NewRequest("POST", supabaseURL+"/rest/v1/reserve_article", bytes.NewBuffer(articleJSON))
	if err != nil {
		fmt.Printf("Error creating Supabase request: %v\n", err)
		return c.String(http.StatusInternalServerError, "リクエストの作成に失敗しました")
	}

	articleReq.Header.Set("apikey", supabaseKey)
	articleReq.Header.Set("Authorization", "Bearer "+supabaseKey)
	articleReq.Header.Set("Content-Type", "application/json")
	articleReq.Header.Set("Prefer", "return=minimal")

	articleResp, err := client.Do(articleReq)
	if err != nil {
		fmt.Printf("Error saving to Supabase: %v\n", err)
		return c.String(http.StatusInternalServerError, "保存に失敗しました")
	}
	defer articleResp.Body.Close()

	fmt.Printf("Supabase response: %d\n", articleResp.StatusCode)

	if articleResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(articleResp.Body)
		fmt.Printf("Supabase error response: %s\n", string(body))
		return c.String(http.StatusInternalServerError, "Supabaseへの保存に失敗しました")
	}

	return c.HTML(http.StatusOK, `
		<html>
			<head>
				<title>保存完了</title>
				<meta charset="utf-8">
			</head>
			<body>
				<h1>記事を保存しました</h1>
				<p>このページは閉じて構いません。</p>
			</body>
		</html>
	`)
}
