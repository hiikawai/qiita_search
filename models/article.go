package models

import (
	"context"
	"fmt"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type Tag struct {
	Name string `json:"name"`
}

type Article struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	Stocks    int    `json:"stocks_count"`
	Tags      []Tag  `json:"tags"`
	User      struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Likes   int    `json:"likes_count"`
	Body    string `json:"body"`
	Summary string `json:"summary"`
}

type PageData struct {
	Title   string
	Message string
	Items   []Article
	Query   string
}

func (a *Article) Summarize() error {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is not set")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")
	prompt := fmt.Sprintf("以下の記事を日本語の箇条書きで80字以内で読みたくなるように要約して（箇条の部分以外で*を使わないで）：\n\n%s", a.Body)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return fmt.Errorf("error generating content: %v", err)
	}

	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		a.Summary = fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])
	} else {
		return fmt.Errorf("no content generated")
	}

	return nil
}
