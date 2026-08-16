package telegram

import (
	"context"
	"time"
)

const pollingRetryDelay = time.Second

type UpdateClient interface {
	GetUpdates(ctx context.Context, offset int64) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type PhotoSender interface {
	SendPhoto(ctx context.Context, chatID int64, caption string, png []byte) error
}

type QRCodeEncoder interface {
	Encode(content string) ([]byte, error)
}

type MessageHandler interface {
	Handle(ctx context.Context, message Message) (Reply, bool)
}

type MembershipHandler interface {
	HandleMembership(ctx context.Context, event MembershipEvent) error
}

type CallbackHandler interface {
	HandleCallback(context.Context, Callback) error
}

type WaitFunc func(ctx context.Context, duration time.Duration) error

type Runner struct {
	client     UpdateClient
	handler    MessageHandler
	membership MembershipHandler
	callbacks  CallbackHandler
	wait       WaitFunc
	qr         QRCodeEncoder
}

func (runner *Runner) WithQRCodeEncoder(encoder QRCodeEncoder) *Runner {
	runner.qr = encoder
	return runner
}

func (runner *Runner) WithCallbackHandler(handler CallbackHandler) *Runner {
	runner.callbacks = handler
	return runner
}

func NewRunner(client UpdateClient, handler MessageHandler, wait WaitFunc, membershipHandlers ...MembershipHandler) *Runner {
	if wait == nil {
		wait = waitForContext
	}
	runner := &Runner{client: client, handler: handler, wait: wait, qr: PNGQRCodeEncoder{}}
	if len(membershipHandlers) > 0 {
		runner.membership = membershipHandlers[0]
	}
	return runner
}

func (runner *Runner) Run(ctx context.Context) error {
	var offset int64
poll:
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		updates, err := runner.client.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if err := runner.wait(ctx, pollingRetryDelay); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			continue
		}
		for _, update := range updates {
			if update.CallbackQuery != nil && runner.callbacks != nil {
				query := update.CallbackQuery
				if err := runner.callbacks.HandleCallback(ctx, Callback{ID: query.ID, SenderID: query.From.ID, Data: query.Data}); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					if err := runner.wait(ctx, pollingRetryDelay); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						return err
					}
					continue poll
				}
			}
			if update.ChatMember != nil && runner.membership != nil {
				change := update.ChatMember
				if change.Chat.ID != 0 && change.NewChatMember.User.ID > 0 {
					event := MembershipEvent{
						ChatID:     change.Chat.ID,
						ChatType:   change.Chat.Type,
						TelegramID: change.NewChatMember.User.ID,
						Result:     MembershipResult(change.NewChatMember),
					}
					if err := runner.membership.HandleMembership(ctx, event); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						if err := runner.wait(ctx, pollingRetryDelay); err != nil {
							if ctx.Err() != nil {
								return nil
							}
							return err
						}
						continue poll
					}
				}
			}
			if update.Message != nil && update.Message.From != nil {
				message := Message{
					ChatType: update.Message.Chat.Type,
					SenderID: update.Message.From.ID,
					Text:     update.Message.Text,
				}
				if reply, handled := runner.handler.Handle(ctx, message); handled {
					sentAsPhoto := false
					if reply.QRContent != "" && runner.qr != nil {
						if sender, ok := runner.client.(PhotoSender); ok {
							if png, err := runner.qr.Encode(reply.QRContent); err == nil {
								if err := sender.SendPhoto(ctx, update.Message.Chat.ID, reply.Text, png); err == nil {
									sentAsPhoto = true
								}
							}
						}
					}
					if !sentAsPhoto {
						_ = runner.client.SendMessage(ctx, update.Message.Chat.ID, reply.Text)
					}
				}
			}
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
		}
	}
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
