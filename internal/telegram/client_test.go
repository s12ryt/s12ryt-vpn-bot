package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientSendPhotoUsesMultipartPNG(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret/sendPhoto" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("content type = %q, err=%v", request.Header.Get("Content-Type"), err)
		}
		reader := multipart.NewReader(request.Body, params["boundary"])
		form, err := reader.ReadForm(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		defer form.RemoveAll()
		if got := form.Value["chat_id"]; len(got) != 1 || got[0] != "9" {
			t.Fatalf("chat_id = %#v", got)
		}
		if got := form.Value["caption"]; len(got) != 1 || got[0] != "status" {
			t.Fatalf("caption = %#v", got)
		}
		files := form.File["photo"]
		if len(files) != 1 || files[0].Filename != "subscription.png" {
			t.Fatalf("files = %#v", files)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if !bytes.Equal(body, png) {
			t.Fatalf("photo = %q", body)
		}
		_, _ = response.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	client := NewClient("secret", server.URL, server.Client())
	if err := client.SendPhoto(context.Background(), 9, "status", png); err != nil {
		t.Fatalf("SendPhoto() error = %v", err)
	}
}

func TestClientGetUpdatesUsesOffsetLongPollingAndRequiredUpdateTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/botsecret-token/getUpdates" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		var body struct {
			Offset         int64    `json:"offset"`
			Timeout        int      `json:"timeout"`
			AllowedUpdates []string `json:"allowed_updates"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Offset != 42 || body.Timeout != 30 {
			t.Errorf("getUpdates body = %#v", body)
		}
		wantUpdates := []string{"message", "callback_query", "chat_member", "my_chat_member"}
		if !reflect.DeepEqual(body.AllowedUpdates, wantUpdates) {
			t.Errorf("allowed_updates = %v, want %v", body.AllowedUpdates, wantUpdates)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"chat":{"id":9,"type":"private"},"from":{"id":12345},"text":"/adminlogin"}}]}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	updates, err := client.GetUpdates(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 42 || updates[0].Message.From.ID != 12345 {
		t.Fatalf("GetUpdates() = %#v", updates)
	}
}

func TestClientGetUpdatesDecodesChatMemberUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true,"result":[{"update_id":43,"chat_member":{"chat":{"id":-1009,"type":"supergroup"},"from":{"id":77},"date":1786881600,"old_chat_member":{"user":{"id":12345},"status":"member"},"new_chat_member":{"user":{"id":12345},"status":"left"}}}]}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	updates, err := client.GetUpdates(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}
	if len(updates) != 1 || updates[0].ChatMember == nil {
		t.Fatalf("GetUpdates() = %#v", updates)
	}
	change := updates[0].ChatMember
	if change.Chat.ID != -1009 || change.NewChatMember.User.ID != 12345 || change.NewChatMember.Status != "left" {
		t.Fatalf("decoded chat member = %#v", change)
	}
}

func TestClientSendMessageUsesJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret-token/sendMessage" {
			t.Errorf("path = %q", request.URL.Path)
		}
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.ChatID != 9 || body.Text != "Ab12Cd34" {
			t.Errorf("sendMessage body = %#v", body)
		}
		_, _ = response.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	if err := client.SendMessage(context.Background(), 9, "Ab12Cd34"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
}

func TestClientSendApprovalRequestUsesInlineCallbackButton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			ChatID      int64  `json:"chat_id"`
			Text        string `json:"text"`
			ReplyMarkup struct {
				InlineKeyboard [][]struct {
					Text         string `json:"text"`
					CallbackData string `json:"callback_data"`
				} `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ChatID != 9 || !strings.Contains(body.Text, "12345") || len(body.ReplyMarkup.InlineKeyboard) != 1 || body.ReplyMarkup.InlineKeyboard[0][0].CallbackData != "approve:12345" {
			t.Fatalf("payload = %#v", body)
		}
		_, _ = response.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()
	client := NewClient("secret", server.URL, server.Client())
	if err := client.SendApprovalRequest(context.Background(), 9, 12345); err != nil {
		t.Fatalf("SendApprovalRequest() error = %v", err)
	}
}

func TestClientAnswerCallbackQueryUsesFixedIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret/answerCallbackQuery" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		var payload struct {
			CallbackQueryID string `json:"callback_query_id"`
			Text            string `json:"text"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.CallbackQueryID != "cb-1" || payload.Text != "已核准" {
			t.Fatalf("payload = %#v", payload)
		}
		_, _ = response.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	client := NewClient("secret", server.URL, server.Client())
	if err := client.AnswerCallbackQuery(context.Background(), "cb-1", "已核准"); err != nil {
		t.Fatalf("AnswerCallbackQuery() error = %v", err)
	}
}

func TestClientGetChatMemberUsesNumericIdentifiersAndDecodesMembership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret-token/getChatMember" {
			t.Errorf("path = %q", request.URL.Path)
		}
		var body struct {
			ChatID int64 `json:"chat_id"`
			UserID int64 `json:"user_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.ChatID != -1009 || body.UserID != 12345 {
			t.Errorf("getChatMember body = %#v", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true,"result":{"user":{"id":12345},"status":"administrator"}}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	member, err := client.GetChatMember(context.Background(), -1009, 12345)
	if err != nil {
		t.Fatalf("GetChatMember() error = %v", err)
	}
	if member.User.ID != 12345 || member.Status != "administrator" {
		t.Fatalf("GetChatMember() = %#v", member)
	}
}

func TestClientGetChatMemberRejectsInvalidIdentifiersBeforeRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	for _, ids := range [][2]int64{{0, 12345}, {-1009, 0}} {
		if _, err := client.GetChatMember(context.Background(), ids[0], ids[1]); err == nil {
			t.Fatalf("GetChatMember(%d, %d) error = nil", ids[0], ids[1])
		}
	}
	if calls != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls)
	}
}

func TestClientGetMeReturnsVerifiedBotIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/botsecret-token/getMe" {
			t.Errorf("path = %q", request.URL.Path)
		}
		_, _ = response.Write([]byte(`{"ok":true,"result":{"id":12345,"is_bot":true,"username":"vpn_example_bot"}}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if user.ID != 12345 || !user.IsBot || user.Username != "vpn_example_bot" {
		t.Fatalf("GetMe() = %#v", user)
	}
}

func TestClientGetMeRejectsNonBotOrMissingUsername(t *testing.T) {
	for _, result := range []string{
		`{"id":12345,"is_bot":false,"username":"vpn_example_bot"}`,
		`{"id":12345,"is_bot":true}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(`{"ok":true,"result":` + result + `}`))
		}))
		client := NewClient("secret-token", server.URL, server.Client())

		if _, err := client.GetMe(context.Background()); err == nil {
			t.Fatal("GetMe() accepted invalid bot identity")
		}
		server.Close()
	}
}

func TestClientErrorsNeverContainBotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	_, err := client.GetUpdates(context.Background(), 0)
	if err == nil {
		t.Fatal("GetUpdates() error = nil")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("GetUpdates() error leaks token: %v", err)
	}
}

func TestClientReturnsStructuredRetryAfterWithoutLeakingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":17}}`))
	}))
	defer server.Close()
	client := NewClient("secret-token", server.URL, server.Client())

	_, err := client.GetChatMember(context.Background(), -1009, 12345)
	if err == nil {
		t.Fatal("GetChatMember() error = nil")
	}
	delay, retryable := RetryAfter(err)
	if !retryable || delay != 17*time.Second {
		t.Fatalf("RetryAfter() = (%v, %v), want (17s, true); err=%v", delay, retryable, err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("GetChatMember() error leaks token: %v", err)
	}
}

func TestClientDistinguishesPermanentAndTemporaryAPIErrors(t *testing.T) {
	for _, testCase := range []struct {
		status    int
		temporary bool
	}{
		{status: http.StatusBadRequest, temporary: false},
		{status: http.StatusBadGateway, temporary: true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(testCase.status)
			_, _ = response.Write([]byte(`{"ok":false,"error_code":` + fmt.Sprint(testCase.status) + `}`))
		}))
		client := NewClient("secret-token", server.URL, server.Client())
		_, err := client.GetChatMember(context.Background(), -1009, 12345)
		if err == nil || IsTemporary(err) != testCase.temporary {
			t.Fatalf("status %d: error=%v temporary=%v, want %v", testCase.status, err, IsTemporary(err), testCase.temporary)
		}
		server.Close()
	}
}
