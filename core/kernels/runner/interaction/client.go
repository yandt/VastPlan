// Package interaction is the Runner-side, UI-free interaction coordinator.
package interaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
)

var (
	ErrRejected  = errors.New("交互被响应者拒绝")
	ErrCancelled = errors.New("交互已取消")
	ErrExpired   = errors.New("交互已过期")
)

// Broker is the Runner's sole dependency. A profile-specific transport adapts
// it to authenticated Backend addressing; Runner never imports Portal/Mobile.
type Broker interface {
	Open(context.Context, interactionapi.Subject, uiv1.InteractionRequest) (interactionapi.Record, error)
	Watch(context.Context, interactionapi.Subject, string, time.Time) (interactionapi.Record, error)
	Cancel(context.Context, interactionapi.Subject, string) (interactionapi.Record, error)
}

type Client struct {
	broker Broker
	source interactionapi.Subject
}

func New(broker Broker, source interactionapi.Subject) (*Client, error) {
	if broker == nil || source.ID == "" || source.TenantID == "" {
		return nil, errors.New("Runner interaction client 必须配置 Broker、来源 capability 和 tenant")
	}
	return &Client{broker: broker, source: source}, nil
}

// Request opens a bound interaction then watches with the durable cursor until
// a terminal Broker decision. It does not consume a Portal or renderer API.
func (c *Client) Request(ctx context.Context, request uiv1.InteractionRequest) (uiv1.InteractionResponse, error) {
	if request.Source.Capability != c.source.ID || request.TenantID != c.source.TenantID {
		return uiv1.InteractionResponse{}, errors.New("Runner 交互请求的来源 capability 和 tenant 必须由当前 Runner 绑定")
	}
	record, err := c.broker.Open(ctx, c.source, request)
	if err != nil {
		return uiv1.InteractionResponse{}, err
	}
	cursor := record.UpdatedAt
	for {
		record, err = c.broker.Watch(ctx, c.source, request.ID, cursor)
		if err != nil {
			return uiv1.InteractionResponse{}, err
		}
		cursor = record.UpdatedAt
		switch record.State {
		case interactionapi.StateAnswered:
			if record.Response == nil {
				return uiv1.InteractionResponse{}, fmt.Errorf("Broker 返回 answered 但没有响应")
			}
			return *record.Response, nil
		case interactionapi.StateRejected:
			return uiv1.InteractionResponse{}, ErrRejected
		case interactionapi.StateCancelled:
			return uiv1.InteractionResponse{}, ErrCancelled
		case interactionapi.StateExpired:
			return uiv1.InteractionResponse{}, ErrExpired
		}
	}
}

func (c *Client) Cancel(ctx context.Context, id string) error {
	_, err := c.broker.Cancel(ctx, c.source, id)
	return err
}
