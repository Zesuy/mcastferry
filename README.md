# McastFerry

McastFerry 是面向 OpenWrt 的轻量 IPv4 ASM 组播转 HTTP 单播代理。它兼容现有的：

```text
GET /udp/<group>:<port>/
```

同一频道的多个客户端共享一个组播上游。未知 UDP payload 会原样透传；可靠识别为 RTP/MPEG-TS 时，会完整处理 CSRC、header extension 和 padding 后输出纯 RTP payload。

## 当前范围

- RAW、raw MPEG-TS 和 RTP/MPEG-TS 自动处理；
- close-delimited HTTP/1.0 与 HTTP/1.1，不使用 chunked；
- 有界 Session、客户端和每客户端队列；
- 可选的只读播放列表 GET/HEAD；
- 最小只读 `/status` 状态接口；
- 分级 text/JSON 日志和 SIGINT/SIGTERM 优雅退出。

不包含播放器、转码、录像、EPG、HLS、SSM、RTP 重排、FEC、TLS、认证或 IGMP proxy。

## 构建

```sh
go build ./cmd/mcastferry
```

## 运行示例

```sh
./mcastferry \
  -multicast-input pppoe-IPTV \
  -http-listen 192.168.4.1:4022 \
  -allowed-group 239.0.0.0/8 \
  -allowed-client 192.168.4.0/24 \
  -allowed-port 1-65535
```

默认最多 5 个活动 Session 和 5 个并发 HTTP 客户端。所有 allowlist 都必须显式提供。

可选播放列表参数：

```text
-playlist-path /www/iptv.m3u8
-playlist-route /playlist.m3u
```

## 许可证

MIT
