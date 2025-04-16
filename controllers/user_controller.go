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

	// 24時間前の時刻を計算
	oneDayAgo := time.Now().Add(-24 * time.Hour)
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

		// 24時間以内のメッセージをフィルタリング
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
					fmt.Printf("メッセージの内容が取得できません: %v\n", message)
					continue
				}

				// [が含まれているメッセージはスキップ
				if strings.Contains(body, "[") {
					fmt.Printf("[が含まれているメッセージをスキップ: %s\n", body)
					continue
				}

				// メッセージを追加
				message["room_id"] = roomID
				allMessages = append(allMessages, message)
				fmt.Printf("現在のメッセージ数: %d\n", len(allMessages))

				// :)が含まれているメッセージの場合、直近のボットのメッセージを表示
				if strings.Contains(body, ":)") {
					fmt.Printf("\n=== :)メッセージ検出 ===\n")
					fmt.Printf("ルームID: %s\n", roomID)
					fmt.Printf("受信メッセージ: %s\n", body)

					fmt.Printf(":)を含むメッセージを検出: %s\n", body)
					// 直近のメッセージを取得（最新5件に制限）
					chatworkReq, err := http.NewRequest("GET",
						fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages?force=1&count=5", roomID),
						nil)
					if err != nil {
						fmt.Printf("直近のメッセージ取得リクエストの作成に失敗しました: %v\n", err)
						continue
					}

					chatworkReq.Header.Set("X-ChatWorkToken", chatworkToken)

					chatworkResp, err := client.Do(chatworkReq)
					if err != nil {
						fmt.Printf("直近のメッセージの取得に失敗しました: %v\n", err)
						continue
					}
					defer chatworkResp.Body.Close()

					var recentMessages []map[string]interface{}
					if err := json.NewDecoder(chatworkResp.Body).Decode(&recentMessages); err != nil {
						fmt.Printf("直近のメッセージのパースに失敗しました: %v\n", err)
						continue
					}

					// 最新のメッセージを探す（最新のものから順に処理）
					var latestMessage string
					var latestTime int64
					for _, msg := range recentMessages {
						accountInfo, ok := msg["account"].(map[string]interface{})
						if !ok {
							continue
						}

						accountID, ok := accountInfo["account_id"].(float64)
						if !ok {
							continue
						}

						if accountID == myAccountID {
							currentMessage, ok := msg["body"].(string)
							if !ok {
								continue
							}
							// 「記事」というワードを含むメッセージのみを対象とする
							if !strings.Contains(currentMessage, "記事") {
								continue
							}

							// 送信時間を取得
							sendTime, ok := msg["send_time"].(float64)
							if !ok {
								continue
							}

							// より新しいメッセージの場合のみ更新
							if latestTime == 0 || int64(sendTime) > latestTime {
								latestMessage = currentMessage
								latestTime = int64(sendTime)
							}
						}
					}

					// 最新のメッセージが見つかった場合のみ保存
					if latestMessage != "" {
						// reserve_articleテーブルに保存
						articleData := map[string]interface{}{
							"room_id": roomID,
							"content": latestMessage,
						}
						articleJSON, err := json.Marshal(articleData)
						if err != nil {
							continue
						}

						articleReq, err := http.NewRequest("POST", supabaseURL+"/rest/v1/reserve_article", bytes.NewBuffer(articleJSON))
						if err != nil {
							continue
						}

						articleReq.Header.Set("apikey", supabaseKey)
						articleReq.Header.Set("Authorization", "Bearer "+supabaseKey)
						articleReq.Header.Set("Content-Type", "application/json")
						articleReq.Header.Set("Prefer", "return=minimal")

						articleResp, err := client.Do(articleReq)
						if err != nil {
							continue
						}
						defer articleResp.Body.Close()

						if articleResp.StatusCode == http.StatusCreated {
							// 送信時間をJST（日本時間）で表示
							jst := time.FixedZone("Asia/Tokyo", 9*60*60)
							messageTime := time.Unix(latestTime, 0).In(jst)
							fmt.Printf("🤖 記事を保存しました [%s]:\n%s\n", messageTime.Format("2006/01/02 15:04:05"), latestMessage)
						}
					}
				}

				// [が含まれているメッセージはfieldテーブルに追加しない
				if !strings.Contains(body, "[") {
					// カンマや句読点で区切られたワードを分割
					words := strings.FieldsFunc(body, func(r rune) bool {
						return r == ',' || r == '、'
					})

					// 各ワードに対して処理
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
							fmt.Printf("Qiitaリクエストの作成に失敗しました: %v\n", err)
							continue
						}

						// Qiitaのアクセストークンを設定
						qiitaToken := os.Getenv("QIITA_ACCESS_TOKEN")
						if qiitaToken == "" {
							fmt.Printf("QIITA_ACCESS_TOKENが設定されていません\n")
							continue
						}
						qiitaReq.Header.Set("Authorization", "Bearer "+qiitaToken)

						qiitaResp, err := client.Do(qiitaReq)
						if err != nil {
							fmt.Printf("Qiitaからの記事取得に失敗しました: %v\n", err)
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
							fmt.Printf("field数取得リクエストの作成に失敗しました: %v\n", err)
							continue
						}

						fieldCountReq.Header.Set("apikey", supabaseKey)
						fieldCountReq.Header.Set("Authorization", "Bearer "+supabaseKey)
						fieldCountReq.Header.Set("Content-Type", "application/json")

						fieldCountResp, err := client.Do(fieldCountReq)
						if err != nil {
							fmt.Printf("field数の取得に失敗しました: %v\n", err)
							continue
						}
						defer fieldCountResp.Body.Close()

						var fieldCount []map[string]interface{}
						if err := json.NewDecoder(fieldCountResp.Body).Decode(&fieldCount); err != nil {
							fmt.Printf("field数のパースに失敗しました: %v\n", err)
							continue
						}

						count, ok := fieldCount[0]["count"].(float64)
						if !ok {
							fmt.Printf("field数の取得に失敗しました\n")
							continue
						}

						// 20件以上の場合、登録をスキップ
						if count >= 20 {
							fmt.Printf("room_id %s のfield数が20件を超えているため、登録をスキップします\n", roomID)
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

						if fieldResp.StatusCode != http.StatusCreated {
							continue
						}
					}
				}
			}
		}
	}

	return c.JSON(http.StatusOK, allMessages)
}
