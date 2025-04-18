package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

func (uc *UserController) Index(c echo.Context) error {
	// SupabaseのエンドポイントとAPIキーを設定
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")
	if supabaseURL == "" || supabaseKey == "" {
		return c.String(http.StatusInternalServerError, "Supabaseの設定が不足しています")
	}

	// Supabaseからユーザーデータを取得
	client := &http.Client{}
	req, err := http.NewRequest("GET", supabaseURL+"/rest/v1/user?select=*", nil)
	if err != nil {
		return c.String(http.StatusInternalServerError, "リクエストの作成に失敗しました")
	}

	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+supabaseKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return c.String(http.StatusInternalServerError, "APIリクエストに失敗しました")
	}
	defer resp.Body.Close()

	// レスポンスの内容を確認
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.String(http.StatusInternalServerError, "レスポンスの読み込みに失敗しました")
	}

	// ユーザーデータをパース
	var users []map[string]interface{}
	if err := json.Unmarshal(body, &users); err != nil {
		return c.String(http.StatusInternalServerError, "JSONのパースに失敗しました")
	}

	// ChatworkのAPIキーを取得
	chatworkToken := os.Getenv("CHATWORK_API_TOKEN")
	if chatworkToken == "" {
		return c.String(http.StatusInternalServerError, "ChatworkのAPIトークンが設定されていません")
	}

	// 自分のアカウントIDを取得
	meReq, err := http.NewRequest("GET", "https://api.chatwork.com/v2/me", nil)
	if err != nil {
		return c.String(http.StatusInternalServerError, "自分の情報取得リクエストの作成に失敗しました")
	}
	meReq.Header.Set("X-ChatWorkToken", chatworkToken)

	meResp, err := client.Do(meReq)
	if err != nil {
		return c.String(http.StatusInternalServerError, "自分の情報取得に失敗しました")
	}
	defer meResp.Body.Close()

	var meInfo map[string]interface{}
	if err := json.NewDecoder(meResp.Body).Decode(&meInfo); err != nil {
		return c.String(http.StatusInternalServerError, "自分の情報のパースに失敗しました")
	}

	myAccountID, ok := meInfo["account_id"].(float64)
	if !ok {
		return c.String(http.StatusInternalServerError, "自分のアカウントIDの取得に失敗しました")
	}

	// 5分前の時刻を計算
	oneDayAgo := time.Now().Add(-3 * time.Minute)
	unixTime := oneDayAgo.Unix()

	// 各ユーザーのroom_idからメッセージを取得
	var allMessages []map[string]interface{}
	for _, user := range users {
		roomID, ok := user["room_id"].(string)
		if !ok || roomID == "" {
			continue
		}

		// Chatworkからメッセージを取得
		chatworkReq, err := http.NewRequest("GET",
			fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages?force=1", roomID),
			nil)
		if err != nil {
			continue
		}

		chatworkReq.Header.Set("X-ChatWorkToken", chatworkToken)

		chatworkResp, err := client.Do(chatworkReq)
		if err != nil {
			continue
		}
		defer chatworkResp.Body.Close()

		// レスポンスの内容を確認
		chatworkBody, err := io.ReadAll(chatworkResp.Body)
		if err != nil {
			continue
		}

		// メッセージをパース
		var messages []map[string]interface{}
		if err := json.NewDecoder(bytes.NewReader(chatworkBody)).Decode(&messages); err != nil {
			continue
		}

		// 5分以内のメッセージをフィルタリング
		for _, message := range messages {
			sendTime, ok := message["send_time"].(float64)
			if !ok {
				continue
			}

			// 自分のアカウントIDと異なるメッセージのみをフィルタリング
			accountID, ok := message["account"].(map[string]interface{})["account_id"].(float64)
			if !ok {
				continue
			}

			if int64(sendTime) >= unixTime && accountID != myAccountID {
				// メッセージの内容を取得
				body, ok := message["body"].(string)
				if !ok {
					continue
				}

				// [が含まれているメッセージはスキップ
				if strings.Contains(body, "[") {
					continue
				}

				// メッセージを追加
				message["room_id"] = roomID
				allMessages = append(allMessages, message)

				// 分野登録機能
				// カンマや句読点で区切られたワードを分割
				words := strings.FieldsFunc(body, func(r rune) bool {
					return r == ',' || r == '、'
				})

				// 各ワードに対して処理
				var registeredWords []string
				for _, word := range words {
					// 空白を削除
					word = strings.TrimSpace(word)
					if word == "" {
						continue
					}

					// QiitaのAPIで検索
					qiitaReq, err := http.NewRequest("GET",
						fmt.Sprintf("https://qiita.com/api/v2/items?query=title:%s&per_page=1", word),
						nil)
					if err != nil {
						continue
					}

					// Qiitaのアクセストークンを設定
					qiitaToken := os.Getenv("QIITA_ACCESS_TOKEN")
					if qiitaToken == "" {
						continue
					}
					qiitaReq.Header.Set("Authorization", "Bearer "+qiitaToken)

					qiitaResp, err := client.Do(qiitaReq)
					if err != nil {
						continue
					}
					defer qiitaResp.Body.Close()

					// レスポンスの内容を確認
					qiitaBody, err := io.ReadAll(qiitaResp.Body)
					if err != nil {
						continue
					}

					var qiitaItems []map[string]interface{}
					if err := json.Unmarshal(qiitaBody, &qiitaItems); err != nil {
						continue
					}

					if len(qiitaItems) == 0 {
						continue
					}

					// 現在のroom_idのfield数を取得
					fieldCountReq, err := http.NewRequest("GET",
						fmt.Sprintf("%s/rest/v1/field?room_id=eq.%s&select=count", supabaseURL, roomID),
						nil)
					if err != nil {
						continue
					}

					fieldCountReq.Header.Set("apikey", supabaseKey)
					fieldCountReq.Header.Set("Authorization", "Bearer "+supabaseKey)
					fieldCountReq.Header.Set("Content-Type", "application/json")

					fieldCountResp, err := client.Do(fieldCountReq)
					if err != nil {
						continue
					}
					defer fieldCountResp.Body.Close()

					var fieldCount []map[string]interface{}
					if err := json.NewDecoder(fieldCountResp.Body).Decode(&fieldCount); err != nil {
						continue
					}

					count, ok := fieldCount[0]["count"].(float64)
					if !ok {
						continue
					}

					// 20件以上の場合、登録をスキップ
					if count >= 20 {
						continue
					}

					// 既に登録されている分野かチェック
					checkReq, err := http.NewRequest("GET",
						fmt.Sprintf("%s/rest/v1/field?room_id=eq.%s&field_name=eq.%s", supabaseURL, roomID, word),
						nil)
					if err != nil {
						continue
					}

					checkReq.Header.Set("apikey", supabaseKey)
					checkReq.Header.Set("Authorization", "Bearer "+supabaseKey)
					checkReq.Header.Set("Content-Type", "application/json")

					checkResp, err := client.Do(checkReq)
					if err != nil {
						continue
					}
					defer checkResp.Body.Close()

					var existingFields []map[string]interface{}
					if err := json.NewDecoder(checkResp.Body).Decode(&existingFields); err != nil {
						continue
					}

					// 既に登録されている場合はスキップ
					if len(existingFields) > 0 {
						continue
					}

					// Supabaseのfieldテーブルにメッセージを追加
					fieldData := map[string]interface{}{
						"room_id":    roomID,
						"field_name": word,
						"priority":   3, // デフォルトの興味の強さを3（普通）に設定
					}
					fieldJSON, err := json.Marshal(fieldData)
					if err != nil {
						continue
					}

					fieldReq, err := http.NewRequest("POST", supabaseURL+"/rest/v1/field", bytes.NewBuffer(fieldJSON))
					if err != nil {
						continue
					}

					fieldReq.Header.Set("apikey", supabaseKey)
					fieldReq.Header.Set("Authorization", "Bearer "+supabaseKey)
					fieldReq.Header.Set("Content-Type", "application/json")
					fieldReq.Header.Set("Prefer", "return=minimal")

					fieldResp, err := client.Do(fieldReq)
					if err != nil {
						continue
					}
					defer fieldResp.Body.Close()

					// 登録成功したワードを記録
					registeredWords = append(registeredWords, word)
				}

				// 登録成功したワードがある場合、まとめて通知
				if len(registeredWords) > 0 {
					messageText := fmt.Sprintf("%s を登録しました", strings.Join(registeredWords, "、"))
					chatworkReq, err := http.NewRequest("POST", fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages", roomID), strings.NewReader(fmt.Sprintf("body=%s", messageText)))
					if err != nil {
						return c.JSON(http.StatusInternalServerError, map[string]string{
							"error": "メッセージ送信リクエストの作成に失敗しました",
						})
					}

					chatworkReq.Header.Set("X-ChatWorkToken", chatworkToken)
					chatworkReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

					_, err = client.Do(chatworkReq)
					if err != nil {
						return c.JSON(http.StatusInternalServerError, map[string]string{
							"error": "メッセージの送信に失敗しました",
						})
					}
				}
			}
		}
	}

	return c.JSON(http.StatusOK, allMessages)
}

func (uc *UserController) SaveMessage(c echo.Context) error {
	// パラメータの取得
	roomID := c.QueryParam("room_id")
	messageID := c.QueryParam("message_id")

	// Chatworkの設定を取得
	chatworkToken := os.Getenv("CHATWORK_API_TOKEN")
	if chatworkToken == "" {
		return c.String(http.StatusInternalServerError, "CHATWORK_API_TOKENが設定されていません")
	}

	// Chatworkからメッセージを取得
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages/%s", roomID, messageID), nil)
	if err != nil {
		return c.String(http.StatusInternalServerError, "リクエストの作成に失敗しました")
	}

	req.Header.Set("X-ChatWorkToken", chatworkToken)

	resp, err := client.Do(req)
	if err != nil {
		return c.String(http.StatusInternalServerError, "メッセージの取得に失敗しました")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.String(http.StatusInternalServerError, "Chatwork APIからのレスポンスが不正です")
	}

	// レスポンスを解析
	var message struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		return c.String(http.StatusInternalServerError, "メッセージの解析に失敗しました")
	}

	// Supabaseの設定を取得
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return c.String(http.StatusInternalServerError, "Supabaseの設定が完了していません")
	}

	// reserve_articleテーブルに保存
	articleData := map[string]interface{}{
		"room_id": roomID,
		"content": message.Body,
	}
	articleJSON, err := json.Marshal(articleData)
	if err != nil {
		return c.String(http.StatusInternalServerError, "データの作成に失敗しました")
	}

	articleReq, err := http.NewRequest("POST", supabaseURL+"/rest/v1/reserve_article", bytes.NewBuffer(articleJSON))
	if err != nil {
		return c.String(http.StatusInternalServerError, "リクエストの作成に失敗しました")
	}

	articleReq.Header.Set("apikey", supabaseKey)
	articleReq.Header.Set("Authorization", "Bearer "+supabaseKey)
	articleReq.Header.Set("Content-Type", "application/json")
	articleReq.Header.Set("Prefer", "return=minimal")

	articleResp, err := client.Do(articleReq)
	if err != nil {
		return c.String(http.StatusInternalServerError, "保存に失敗しました")
	}
	defer articleResp.Body.Close()

	if articleResp.StatusCode != http.StatusCreated {
		return c.String(http.StatusInternalServerError, "Supabaseへの保存に失敗しました")
	}

	return c.HTML(http.StatusOK, `
		<html>
			<head>
				<title>保存完了</title>
				<meta charset="utf-8">
			</head>
			<body>
				<h1>メッセージを保存しました</h1>
				<p>このページは閉じて構いません。</p>
			</body>
		</html>
	`)
}
