package desk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// 线路协议：所有消息都是 [1B 类型][4B BE 长度][载荷]。
// 配对与握手阶段载荷是明文 JSON；握手完成后全部走 AES-256-GCM 密封帧
// （8B BE 计数器 nonce + 密文），方向各自独立计数。

const (
	// FrameJSON 是控制/事件消息（UTF-8 JSON）。
	FrameJSON byte = 0x01
	// FrameChunk 是文件数据块：[16B 传输ID][4B BE 序号][数据]。
	FrameChunk byte = 0x02
	// FrameInput 是键鼠输入事件（16B 定长二进制）。
	FrameInput byte = 0x03
)

const (
	maxFrameSize   = 8 << 20 // 单帧上限 8 MiB
	ChunkSize      = 256 << 10
	maxChatLength  = 16 << 10
	maxNameLength  = 64
	handshakeLimit = 10 * time.Second
	dialTimeout    = 5 * time.Second
	pingInterval   = 15 * time.Second
	dropAfter      = 45 * time.Second
	pairingTimeout = 90 * time.Second
)

// 会话内 JSON 消息类型（"t" 字段）。
const (
	msgHello        = "hello"
	msgHelloAck     = "hello_ack"
	msgHelloReady   = "hello_ready"
	msgChat         = "chat"
	msgFileOffer    = "file_offer"
	msgFileAccept   = "file_accept"
	msgFileReject   = "file_reject"
	msgFileDone     = "file_done"
	msgFileAbort    = "file_abort"
	msgCtlTake      = "ctl_take"
	msgCtlTakeOK    = "ctl_take_ok"
	msgCtlDeny      = "ctl_deny"
	msgCtlChain     = "ctl_chain"
	msgCtlRelease   = "ctl_release"
	msgCtlReleaseOK = "ctl_release_ok"
	msgPing         = "ping"
	msgPong         = "pong"
	msgState        = "state"
)

// 配对阶段 JSON 消息类型。
const (
	msgPairRequest  = "pair_request"
	msgPairAck      = "pair_ack"
	msgPairConfirmA = "pair_confirm_a"
	msgPairConfirmB = "pair_confirm_b"
	msgPairDone     = "pair_done"
	msgPairReject   = "pair_reject"
)

var errFrameTooLarge = fmt.Errorf("帧超过 %d 字节上限", maxFrameSize)

// writeFrame 写一帧：[type][4B len][payload]。
func writeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > maxFrameSize {
		return errFrameTooLarge
	}
	head := [5]byte{typ}
	binary.BigEndian.PutUint32(head[1:], uint32(len(payload)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame 读一帧，载荷上限 maxFrameSize。
func readFrame(r io.Reader) (typ byte, payload []byte, err error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	typ = head[0]
	n := binary.BigEndian.Uint32(head[1:])
	if n > maxFrameSize {
		return 0, nil, errFrameTooLarge
	}
	payload = make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return typ, payload, nil
}

// chunkPayload 拼装文件块载荷。
func chunkPayload(transferID [16]byte, seq uint32, data []byte) []byte {
	out := make([]byte, 20, 20+len(data))
	copy(out, transferID[:])
	binary.BigEndian.PutUint32(out[16:], seq)
	return append(out, data...)
}

// parseChunk 拆文件块载荷。
func parseChunk(payload []byte) (id [16]byte, seq uint32, data []byte, err error) {
	if len(payload) < 20 {
		return id, 0, nil, errors.New("文件块过短")
	}
	copy(id[:], payload[:16])
	seq = binary.BigEndian.Uint32(payload[16:20])
	return id, seq, payload[20:], nil
}
