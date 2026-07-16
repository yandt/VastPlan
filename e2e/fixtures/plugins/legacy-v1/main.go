// legacy-v1 是不依赖 sdk/go/plugin 的旧协议客户端夹具。
// 它只实现 Plugin-Host v1 最小消息集，用来证明 SDK 内部重构后既有 wire 客户端仍可接入。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
)

func main() {
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve() error {
	if os.Getenv(protocol.MagicEnvKey) != protocol.MagicCookie {
		return errors.New("legacy v1 插件必须由宿主拉起")
	}
	conn, err := grpc.NewClient(os.Getenv(protocol.HostAddrEnvKey), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx := context.Background()
	client := pluginhostv1.NewPluginHostClient(conn)
	ack, err := client.Handshake(ctx, &pluginhostv1.Hello{
		ProtoVersions: []int32{1},
		Magic:         protocol.MagicCookie,
		PluginId:      "com.vastplan.fixture.legacy-v1",
		PluginVersion: "0.1.0",
		Engines:       map[string]string{"backend": ">=0.1.0 <2.0.0"},
		LaunchToken:   os.Getenv(protocol.LaunchTokenEnvKey),
	})
	if err != nil {
		return err
	}
	streamCtx := metadata.AppendToOutgoingContext(ctx, protocol.SessionMetadataKey, ack.SessionId)
	stream, err := client.Channel(streamCtx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Declare{
		Declare: &pluginhostv1.Declaration{Contributions: []*pluginhostv1.Contribution{{
			ExtensionPoint: "tool.package",
			Id:             "fixture.legacy-v1",
			DescriptorJson: []byte(`{"title":"Legacy v1 raw client"}`),
		}}},
	}}); err != nil {
		return err
	}

	active := false
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		switch body := msg.Msg.(type) {
		case *pluginhostv1.FromHost_Registered:
			if len(body.Registered.Rejected) != 0 {
				return fmt.Errorf("legacy v1 contribution 被拒: %v", body.Registered.Rejected)
			}
		case *pluginhostv1.FromHost_Lifecycle:
			active = body.Lifecycle.Op == pluginhostv1.Lifecycle_OP_ACTIVATE
			shutdown := body.Lifecycle.Op == pluginhostv1.Lifecycle_OP_SHUTDOWN
			if err := stream.Send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_LifecycleAck{
				LifecycleAck: &pluginhostv1.LifecycleAck{RequestId: body.Lifecycle.RequestId, Ready: true},
			}}); err != nil {
				return err
			}
			if shutdown {
				return nil
			}
		case *pluginhostv1.FromHost_Invoke:
			result := &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
			payload := body.Invoke.Payload
			if !active {
				result = &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR,
					Error: &contractv1.Error{Code: "legacy.inactive", Message: "legacy v1 插件未激活"}}
				payload = nil
			}
			if err := stream.Send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_InvokeResult{
				InvokeResult: &pluginhostv1.InvokeResponse{RequestId: body.Invoke.RequestId, Result: result, Payload: payload},
			}}); err != nil {
				return err
			}
		case *pluginhostv1.FromHost_Ping:
			if err := stream.Send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Pong{
				Pong: &pluginhostv1.Pong{RequestId: body.Ping.RequestId},
			}}); err != nil {
				return err
			}
		}
	}
}
