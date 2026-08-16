package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxTelegramResponseBytes = 1 << 20

var requiredUpdateTypes = []string{"message", "callback_query", "chat_member", "my_chat_member"}

type Client struct {
	endpoint   string
	httpClient *http.Client
}

type Update struct {
	UpdateID      int64              `json:"update_id"`
	Message       *APIMessage        `json:"message,omitempty"`
	CallbackQuery *CallbackQuery     `json:"callback_query,omitempty"`
	ChatMember    *ChatMemberUpdated `json:"chat_member,omitempty"`
}

type CallbackQuery struct {
	ID   string `json:"id"`
	From User   `json:"from"`
	Data string `json:"data"`
}

type APIMessage struct {
	Chat Chat   `json:"chat"`
	From *User  `json:"from,omitempty"`
	Text string `json:"text,omitempty"`
}

type Chat struct {
	ID   int64    `json:"id"`
	Type ChatType `json:"type"`
}

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username,omitempty"`
}

type ChatMemberUpdated struct {
	Chat          Chat       `json:"chat"`
	From          User       `json:"from"`
	Date          int64      `json:"date"`
	OldChatMember ChatMember `json:"old_chat_member"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

type ChatMember struct {
	User     User   `json:"user"`
	Status   string `json:"status"`
	IsMember *bool  `json:"is_member,omitempty"`
}

type apiResponse[T any] struct {
	OK         bool          `json:"ok"`
	Result     T             `json:"result"`
	ErrorCode  int           `json:"error_code,omitempty"`
	Parameters apiParameters `json:"parameters,omitempty"`
}

type apiParameters struct {
	RetryAfter int `json:"retry_after,omitempty"`
}

type APIError struct {
	StatusCode         int
	ErrorCode          int
	RetryAfterDuration time.Duration
	Temporary          bool
}

func (err *APIError) Error() string {
	return fmt.Sprintf("Telegram API returned status %d", err.StatusCode)
}

type requestError struct{}

func (*requestError) Error() string { return "Telegram request failed" }

func RetryAfter(err error) (time.Duration, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfterDuration <= 0 {
		return 0, false
	}
	return apiErr.RetryAfterDuration, true
}

func IsTemporary(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Temporary
	}
	var transportErr *requestError
	return errors.As(err, &transportErr)
}

func NewClient(token, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		endpoint:   strings.TrimRight(baseURL, "/") + "/bot" + token + "/",
		httpClient: httpClient,
	}
}

func (client *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	payload := struct {
		Offset         int64    `json:"offset"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{
		Offset:         offset,
		Timeout:        30,
		AllowedUpdates: requiredUpdateTypes,
	}
	var updates []Update
	if err := client.call(ctx, "getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (client *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	payload := struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}{ChatID: chatID, Text: text}
	var result json.RawMessage
	return client.call(ctx, "sendMessage", payload, &result)
}

func (client *Client) SendApprovalRequest(ctx context.Context, administratorID, targetTelegramID int64) error {
	if administratorID <= 0 || targetTelegramID <= 0 {
		return errors.New("approval request identifiers are invalid")
	}
	type inlineButton struct {
		Text         string `json:"text"`
		CallbackData string `json:"callback_data"`
	}
	payload := struct {
		ChatID      int64  `json:"chat_id"`
		Text        string `json:"text"`
		ReplyMarkup struct {
			InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}{ChatID: administratorID, Text: fmt.Sprintf("VPN 使用者 %d 等待核准。", targetTelegramID)}
	payload.ReplyMarkup.InlineKeyboard = [][]inlineButton{{
		{Text: "核准", CallbackData: fmt.Sprintf("approve:%d", targetTelegramID)},
		{Text: "拒絕", CallbackData: fmt.Sprintf("reject:%d", targetTelegramID)},
	}}
	var result json.RawMessage
	return client.call(ctx, "sendMessage", payload, &result)
}

func (client *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	if strings.TrimSpace(callbackID) == "" || strings.TrimSpace(text) == "" {
		return errors.New("callback answer is invalid")
	}
	payload := struct {
		CallbackQueryID string `json:"callback_query_id"`
		Text            string `json:"text"`
	}{CallbackQueryID: callbackID, Text: text}
	var result bool
	return client.call(ctx, "answerCallbackQuery", payload, &result)
}

func (client *Client) GetChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error) {
	if chatID == 0 || userID <= 0 {
		return ChatMember{}, errors.New("invalid Telegram chat member identifiers")
	}
	payload := struct {
		ChatID int64 `json:"chat_id"`
		UserID int64 `json:"user_id"`
	}{ChatID: chatID, UserID: userID}
	var member ChatMember
	if err := client.call(ctx, "getChatMember", payload, &member); err != nil {
		return ChatMember{}, err
	}
	if member.User.ID != userID || strings.TrimSpace(member.Status) == "" {
		return ChatMember{}, errors.New("Telegram chat member response is invalid")
	}
	return member, nil
}

func (client *Client) GetMe(ctx context.Context) (User, error) {
	var user User
	if err := client.call(ctx, "getMe", struct{}{}, &user); err != nil {
		return User{}, err
	}
	if user.ID <= 0 || !user.IsBot || strings.TrimSpace(user.Username) == "" {
		return User{}, errors.New("Telegram bot identity is invalid")
	}
	return user, nil
}

func (client *Client) call(ctx context.Context, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Telegram request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("create Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return &requestError{}
	}
	defer response.Body.Close()

	var envelope apiResponse[json.RawMessage]
	if err := json.NewDecoder(io.LimitReader(response.Body, maxTelegramResponseBytes)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode Telegram response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || !envelope.OK {
		errorCode := envelope.ErrorCode
		if errorCode == 0 {
			errorCode = response.StatusCode
		}
		return &APIError{
			StatusCode:         response.StatusCode,
			ErrorCode:          errorCode,
			RetryAfterDuration: time.Duration(envelope.Parameters.RetryAfter) * time.Second,
			Temporary:          response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError || errorCode == http.StatusTooManyRequests || errorCode >= http.StatusInternalServerError,
		}
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode Telegram result: %w", err)
	}
	return nil
}
