package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

func (uc *UserController) Register(c echo.Context) error {
	// リクエストパラメータを取得
	message := c.QueryParam("message")
	roomID := c.QueryParam("room_id")

	fmt.Printf("受信したメッセージ: %s\n", message)
	fmt.Printf("受信したroom_id: %s\n", roomID)

	if message == "" || roomID == "" {
		return c.String(http.StatusBadRequest, "メッセージとルームIDは必須です")
	}

	// メッセージをURLデコード
	decodedMessage, err := url.QueryUnescape(message)
	if err != nil {
		fmt.Printf("URLデコードエラー: %v\n", err)
		return c.String(http.StatusBadRequest, "メッセージのデコードに失敗しました")
	}
	fmt.Printf("デコード後のメッセージ: %s\n", decodedMessage)

	// Supabaseの設定を取得
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_KEY")
	if supabaseURL == "" || supabaseKey == "" {
		return c.String(http.StatusInternalServerError, "Supabaseの設定が不足しています")
	}

	// ChatworkのAPIトークンを取得
	chatworkToken := os.Getenv("CHATWORK_API_TOKEN")
	if chatworkToken == "" {
		return c.String(http.StatusInternalServerError, "ChatworkのAPIトークンが設定されていません")
	}

	// HTTPクライアントの作成
	client := &http.Client{}

	// メッセージ本文からワードを抽出
	words := strings.FieldsFunc(decodedMessage, func(r rune) bool {
		return r == ',' || r == '、' || r == '\n'
	})
	fmt.Printf("抽出されたワード: %v\n", words)

	// 各ワードに対して処理
	var registeredWords []string
	for _, word := range words {
		// 全角スペースを半角に変換し、複数のスペースを1つに統一
		word = strings.Join(strings.Fields(strings.ReplaceAll(word, "　", " ")), " ")
		if word == "" {
			continue
		}
		fmt.Printf("処理前のワード: %s\n", word)

		// 単語の正規化処理
		// 1. 全角英数字を半角に変換
		word = strings.Map(func(r rune) rune {
			switch {
			case r >= 'Ａ' && r <= 'Ｚ':
				return r - 'Ａ' + 'A'
			case r >= 'ａ' && r <= 'ｚ':
				return r - 'ａ' + 'a'
			case r >= '０' && r <= '９':
				return r - '０' + '0'
			default:
				return r
			}
		}, word)

		// 2. 最初の文字を大文字に、それ以外を小文字に（英数字の場合のみ）
		if len(word) > 0 {
			firstChar := word[0]
			if (firstChar >= 'A' && firstChar <= 'Z') || (firstChar >= 'a' && firstChar <= 'z') {
				word = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
			}
		}

		fmt.Printf("変換後: %s\n", word)

		// スペースで分割
		subWords := strings.Fields(word)
		if len(subWords) == 0 {
			continue
		}

		// 検索クエリを構築
		var queryParts []string
		for _, subWord := range subWords {
			// 日本語のワードをURLエンコード
			encodedWord := url.QueryEscape(subWord)
			fmt.Printf("エンコード前のワード: %s\n", subWord)
			fmt.Printf("エンコード後のワード: %s\n", encodedWord)
			queryParts = append(queryParts, fmt.Sprintf("title:%s", encodedWord))
		}
		query := strings.Join(queryParts, "+")
		fmt.Printf("Qiita検索クエリ: %s\n", query)

		// QiitaのAPIで検索
		qiitaReq, err := http.NewRequest("GET",
			fmt.Sprintf("https://qiita.com/api/v2/items?query=%s&per_page=1", query),
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

		// Supabaseのfieldテーブルにメッセージを追加
		fieldData := map[string]interface{}{
			"room_id":    roomID,
			"field_name": word,
			"priority":   3, // デフォルトの興味の強さを3（普通）に設定
		}
		fieldJSON, err := json.Marshal(fieldData)
		if err != nil {
			fmt.Printf("JSONマーシャリングエラー: %v\n", err)
			continue
		}

		fmt.Printf("Supabaseに保存するデータ: %s\n", string(fieldJSON))

		fieldReq, err := http.NewRequest("POST", supabaseURL+"/rest/v1/field", bytes.NewBuffer(fieldJSON))
		if err != nil {
			fmt.Printf("Supabaseリクエスト作成エラー: %v\n", err)
			continue
		}

		fieldReq.Header.Set("apikey", supabaseKey)
		fieldReq.Header.Set("Authorization", "Bearer "+supabaseKey)
		fieldReq.Header.Set("Content-Type", "application/json")
		fieldReq.Header.Set("Prefer", "return=minimal")

		fieldResp, err := client.Do(fieldReq)
		if err != nil {
			fmt.Printf("Supabaseリクエスト送信エラー: %v\n", err)
			continue
		}
		defer fieldResp.Body.Close()

		fmt.Printf("Supabaseレスポンスステータス: %d\n", fieldResp.StatusCode)

		if fieldResp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(fieldResp.Body)
			fmt.Printf("Supabaseエラーレスポンス: %s\n", string(body))
			continue
		}

		// 登録成功したワードを記録
		registeredWords = append(registeredWords, word)
		fmt.Printf("登録成功: %s\n", word)
	}

	// 登録成功したワードがある場合、まとめて通知
	if len(registeredWords) > 0 {
		messageText := fmt.Sprintf("%s を登録しました", strings.Join(registeredWords, "、"))
		chatworkReq, err := http.NewRequest("POST",
			fmt.Sprintf("https://api.chatwork.com/v2/rooms/%s/messages", roomID),
			strings.NewReader(fmt.Sprintf("body=%s", messageText)))
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

	return c.String(http.StatusOK, "OK")
}
