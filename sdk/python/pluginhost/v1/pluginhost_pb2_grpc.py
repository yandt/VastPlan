"""Minimal generated-style gRPC client for the host-owned PluginHost service."""

import grpc

from pluginhost.v1 import pluginhost_pb2 as pluginhost__pb2


class PluginHostStub:
    def __init__(self, channel: grpc.Channel):
        self.Handshake = channel.unary_unary(
            "/vastplan.pluginhost.v1.PluginHost/Handshake",
            request_serializer=pluginhost__pb2.Hello.SerializeToString,
            response_deserializer=pluginhost__pb2.HelloAck.FromString,
        )
        self.Channel = channel.stream_stream(
            "/vastplan.pluginhost.v1.PluginHost/Channel",
            request_serializer=pluginhost__pb2.FromPlugin.SerializeToString,
            response_deserializer=pluginhost__pb2.FromHost.FromString,
        )
