package desk

import (
	"testing"
)

// 事件流断点续传：after=seq 补空档丢失；缺省只投实时；客户端 seq 领先（重启）不回放。
func TestUIBusReplayAfter(t *testing.T) {
	b := newUIBus()
	for i := 0; i < 5; i++ {
		b.Emit("desk:chat", i)
	}

	// after=2 → 回放 3,4,5；订阅保持，实时事件继续进同一通道
	id, ch := b.subscribe(2)
	got := []int{}
	for len(got) < 3 {
		got = append(got, (<-ch).Seq)
	}
	if got[0] != 3 || got[1] != 4 || got[2] != 5 {
		b.unsubscribe(id)
		t.Fatalf("after=2 应回放 3..5, got %v", got)
	}
	b.Emit("desk:chat", "live")
	if ev := (<-ch).Seq; ev != 6 {
		b.unsubscribe(id)
		t.Fatalf("回放后实时投递异常: seq=%d", ev)
	}
	b.unsubscribe(id)

	// 缺省 after（首连/老客户端）→ 不回放
	id2, ch2 := b.subscribe(0)
	select {
	case ev := <-ch2:
		b.unsubscribe(id2)
		t.Fatalf("缺省 after 不应回放, got seq=%d", ev.Seq)
	default:
	}
	// after 领先当前 seq（hub 重启后客户端旧值）→ 不回放
	id3, ch3 := b.subscribe(999)
	select {
	case ev := <-ch3:
		b.unsubscribe(id2)
		b.unsubscribe(id3)
		t.Fatalf("after 领先不应回放, got seq=%d", ev.Seq)
	default:
	}
	b.Emit("desk:chat", "live2")
	if ev := (<-ch2).Seq; ev != 7 {
		b.unsubscribe(id2)
		b.unsubscribe(id3)
		t.Fatalf("缺省客户端实时投递异常: seq=%d", ev)
	}
	if ev := (<-ch3).Seq; ev != 7 {
		b.unsubscribe(id2)
		b.unsubscribe(id3)
		t.Fatalf("领先客户端实时投递异常: seq=%d", ev)
	}
	b.unsubscribe(id2)
	b.unsubscribe(id3)
}

// 回放环容量上限：emit 超限后只回放最近 256 条（=订阅通道缓冲，保证持锁写入不阻塞）。
func TestUIBusReplayCap(t *testing.T) {
	b := newUIBus()
	for i := 0; i < 600; i++ {
		b.Emit("e", i)
	}
	id, ch := b.subscribe(1)
	first := (<-ch).Seq
	last := first
drain:
	for {
		select {
		case ev := <-ch:
			last = ev.Seq
		default:
			break drain
		}
	}
	b.unsubscribe(id)
	if last-first != 255 || last != 600 {
		t.Fatalf("回放应恰为最近 256 条: first=%d last=%d", first, last)
	}
}

// desk:state 高频快照不进回放环：密集 state 不应把关键事件挤出补传窗口。
func TestUIBusStateNotReplayed(t *testing.T) {
	b := newUIBus()
	b.Emit("desk:chat", "keep-me") // seq 1
	for i := 0; i < uiBusHistCap+10; i++ {
		b.Emit("desk:state", nil) // 密集 state 不占环
	}
	id, ch := b.subscribe(0)
	select {
	case ev := <-ch:
		b.unsubscribe(id)
		t.Fatalf("after=0 不应回放, got seq=%d", ev.Seq)
	default:
	}
	// after=0 不回放；用 after=... 无从构造，直接验证环内容：回放触发后
	// 只有 chat 一条（state 全部被排除在环外）。
	b.mu.Lock()
	n := len(b.hist)
	b.mu.Unlock()
	if n != 1 {
		b.unsubscribe(id)
		t.Fatalf("state 不应入回放环: hist=%d", n)
	}
	b.unsubscribe(id)
}
