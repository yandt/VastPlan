package sharedstatebackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go"
)

const (
	defaultChunkBytes = 128 << 10
	apiSnapshotPrefix = "$JS.API.STREAM.SNAPSHOT."
	apiRestorePrefix  = "$JS.API.STREAM.RESTORE."
)

type apiError struct {
	Code        int    `json:"code"`
	ErrorCode   int    `json:"err_code"`
	Description string `json:"description"`
}

func (err *apiError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("NATS API code=%d err_code=%d: %s", err.Code, err.ErrorCode, err.Description)
}

type snapshotAPIResponse struct {
	Error  *apiError       `json:"error,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
}

type restoreAPIResponse struct {
	Error          *apiError `json:"error,omitempty"`
	DeliverSubject string    `json:"deliver_subject,omitempty"`
}

type completionAPIResponse struct {
	Error *apiError `json:"error,omitempty"`
}

func nativeSnapshot(ctx context.Context, nc *nats.Conn, stream string, target io.Writer) (json.RawMessage, json.RawMessage, error) {
	if nc == nil || !safeToken(stream) || target == nil {
		return nil, nil, errors.New("JetStream snapshot 输入无效")
	}
	deliver := nats.NewInbox()
	subscription, err := nc.SubscribeSync(deliver)
	if err != nil {
		return nil, nil, err
	}
	defer subscription.Unsubscribe()
	if err := nc.FlushWithContext(ctx); err != nil {
		return nil, nil, err
	}
	request, _ := json.Marshal(map[string]any{
		"deliver_subject": deliver,
		"no_consumers":    true,
		"chunk_size":      defaultChunkBytes,
		"jsck":            true,
	})
	message, err := nc.RequestWithContext(ctx, apiSnapshotPrefix+stream, request)
	if err != nil {
		return nil, nil, fmt.Errorf("请求 JetStream snapshot: %w", err)
	}
	var response snapshotAPIResponse
	if err := json.Unmarshal(message.Data, &response); err != nil {
		return nil, nil, fmt.Errorf("解析 JetStream snapshot 响应: %w", err)
	}
	if response.Error != nil {
		return nil, nil, response.Error
	}
	if !validJSONObject(response.Config) || !validJSONObject(response.State) {
		return nil, nil, errors.New("JetStream snapshot 响应缺少 config/state")
	}
	for {
		chunk, err := subscription.NextMsgWithContext(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("接收 JetStream snapshot: %w", err)
		}
		if status := messageStatus(chunk); status >= 400 {
			return nil, nil, fmt.Errorf("JetStream snapshot 数据流失败: status=%d description=%s", status, chunk.Header.Get("Description"))
		}
		if len(chunk.Data) == 0 {
			break
		}
		if _, err := target.Write(chunk.Data); err != nil {
			return nil, nil, fmt.Errorf("写入 JetStream snapshot: %w", err)
		}
		if chunk.Reply != "" {
			if err := chunk.Respond(nil); err != nil {
				return nil, nil, fmt.Errorf("确认 JetStream snapshot chunk: %w", err)
			}
		}
	}
	return append(json.RawMessage(nil), response.Config...), append(json.RawMessage(nil), response.State...), nil
}

func nativeRestore(ctx context.Context, nc *nats.Conn, stream string, config, state json.RawMessage, source io.Reader) error {
	if nc == nil || !safeToken(stream) || !validJSONObject(config) || !validJSONObject(state) || source == nil {
		return errors.New("JetStream restore 输入无效")
	}
	request, err := json.Marshal(struct {
		Config json.RawMessage `json:"config"`
		State  json.RawMessage `json:"state"`
	}{Config: config, State: state})
	if err != nil {
		return err
	}
	message, err := nc.RequestWithContext(ctx, apiRestorePrefix+stream, request)
	if err != nil {
		return fmt.Errorf("请求 JetStream restore: %w", err)
	}
	var response restoreAPIResponse
	if err := json.Unmarshal(message.Data, &response); err != nil {
		return fmt.Errorf("解析 JetStream restore 响应: %w", err)
	}
	if response.Error != nil {
		return response.Error
	}
	if !safeSubject(response.DeliverSubject) || !strings.HasPrefix(response.DeliverSubject, "$JS.SNAPSHOT.RESTORE."+stream+".") {
		return errors.New("JetStream restore 返回非法 deliver subject")
	}
	buffer := make([]byte, defaultChunkBytes)
	for {
		read, readErr := source.Read(buffer)
		if read > 0 {
			ack, requestErr := nc.RequestWithContext(ctx, response.DeliverSubject, buffer[:read])
			if requestErr != nil {
				return fmt.Errorf("发送 JetStream restore chunk: %w", requestErr)
			}
			if len(ack.Data) != 0 {
				var completion completionAPIResponse
				if json.Unmarshal(ack.Data, &completion) == nil && completion.Error != nil {
					return completion.Error
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取 JetStream snapshot: %w", readErr)
		}
	}
	completionMessage, err := nc.RequestWithContext(ctx, response.DeliverSubject, nil)
	if err != nil {
		return fmt.Errorf("完成 JetStream restore: %w", err)
	}
	var completion completionAPIResponse
	if err := json.Unmarshal(completionMessage.Data, &completion); err != nil {
		return fmt.Errorf("解析 JetStream restore 完成响应: %w", err)
	}
	if completion.Error != nil {
		return completion.Error
	}
	return nil
}

func messageStatus(message *nats.Msg) int {
	if message == nil || message.Header == nil {
		return 0
	}
	value := strings.TrimSpace(message.Header.Get("Status"))
	status, _ := strconv.Atoi(value)
	return status
}

func safeSubject(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n")
}
