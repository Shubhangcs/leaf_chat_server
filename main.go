package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
	"cloud.google.com/go/firestore"
)

type ChatRequest struct {
	UserID   string `json:"user_id"`
	ChatID   string `json:"chat_id"` // This is the plant name
	Question string `json:"question"`
}

type FirebaseData struct {
	UserID   string `json:"user_id"`
	ChatID   string `json:"chat_id"`
	Message  string `json:"message"`
	UserType string `json:"user_type"` // "ai"
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func ollamaInformationModel(prompt string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"model":  "llama3.2",
		"prompt": prompt,
	})
	if err != nil {
		return "", err
	}

	res, err := http.Post("http://34.172.219.173:11434/api/generate", "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Println("HTTP error:", err)
		return "", err
	}
	defer res.Body.Close()

	var fullResponse strings.Builder
	decoder := json.NewDecoder(res.Body)

	for {
		var chunk OllamaResponse
		if err := decoder.Decode(&chunk); err == io.EOF {
			break
		} else if err != nil {
			fmt.Println("Decoding error:", err)
			return "", err
		}

		fullResponse.WriteString(chunk.Response)

		if chunk.Done {
			break
		}
	}

	return fullResponse.String(), nil
}

func saveToFirestore(ctx context.Context, app *firebase.App, data FirebaseData) error {
	client, err := app.Firestore(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	_, _, err = client.Collection("chats").
		Doc(data.UserID).
		Collection("chat").
		Doc(data.ChatID).
		Collection("messages").
		Add(ctx, map[string]interface{}{
			"message":   data.Message,
			"timestamp": firestore.ServerTimestamp,
			"user_id":   data.UserID,
			"user_type": "AI",
		})

	return err
}

func chatHandler(app *firebase.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		var chatReq ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		response, err := ollamaInformationModel(chatReq.Question)
		if err != nil {
			http.Error(w, "Failed to get response from LLaMA", http.StatusInternalServerError)
			return
		}

		data := FirebaseData{
			UserID:   chatReq.UserID,
			ChatID:   chatReq.ChatID,
			Message:  response,
			UserType: "ai",
		}

		if err := saveToFirestore(r.Context(), app, data); err != nil {
			http.Error(w, "Failed to save to Firestore", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(data)
	}
}

func main() {
	ctx := context.Background()
	sa := option.WithCredentialsFile("leafscan-d0ee4-firebase-adminsdk-fbsvc-cb14153170.json") // replace with actual file if needed
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase app: %v", err)
	}

	http.Handle("/chat", withCORS(chatHandler(app)))
	fmt.Println("🚀 Server running at :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
