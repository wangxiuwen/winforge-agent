package desk

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestReadFrameRejectsHugeLength 超大长度前缀必须被拒（防内存放大攻击）。
func TestReadFrameRejectsHugeLength(t *testing.T) {
	var head [5]byte
	binary.BigEndian.PutUint32(head[1:], uint32(1<<30)) // 1GB 长度
	r := bytes.NewReader(head[:])
	_, _, err := readFrame(r)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超大帧应被拒绝, got %v", err)
	}
}

// TestWriteFrameRejectsOversize 超过上限的写侧同样拒绝。
func TestWriteFrameRejectsOversize(t *testing.T) {
	big := make([]byte, maxFrameSize+1)
	if err := writeFrame(&bytes.Buffer{}, FrameJSON, big); err == nil {
		t.Fatal("超限写应被拒绝")
	}
}

// TestReadFrameExact 边界值：恰好等于上限的帧应可读。
func TestReadFrameExact(t *testing.T) {
	payload := make([]byte, maxFrameSize)
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, byte(FrameJSON))
	binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)
	typ, got, err := readFrame(&buf)
	if err != nil || typ != FrameJSON || !bytes.Equal(got, payload) {
		t.Fatalf("边界帧读取失败: %v", err)
	}
}
