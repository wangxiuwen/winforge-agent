package agent

import (
	"bytes"
	"sync"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func collectScan(t *testing.T, decoder *encoding.Decoder, input []byte) []ExecEvent {
	t.Helper()
	events := make(chan ExecEvent, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go scanOutputWith(&wg, decoder, bytes.NewReader(input), "stdout", events)
	go func() {
		wg.Wait()
		close(events)
	}()
	var got []ExecEvent
	for e := range events {
		got = append(got, e)
	}
	return got
}

// TestScanOutputDecodesGBK 用真实 GBK 字节验证 scanOutput 的解码管线：
// 中文系统的 cmd/PowerShell 管道输出是 CP936，修复前 scanner 按 UTF-8
// 解码会把整行变成 U+FFFD 替换符（issue #5 Bug 1）。
func TestScanOutputDecodesGBK(t *testing.T) {
	t.Parallel()

	// 「系统找不到指定的路径。」的 GBK 编码（cmd 的典型报错文案）。
	const want = "系统找不到指定的路径。"
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(want + "\r\n"))
	if err != nil {
		t.Fatalf("构造 GBK 样本: %v", err)
	}

	events := collectScan(t, simplifiedchinese.GB18030.NewDecoder(), gbkBytes)
	if len(events) != 1 {
		t.Fatalf("scanOutput 产出 %d 个事件 = %+v, want 1", len(events), events)
	}
	if events[0].Stream != "stdout" || events[0].Data != want {
		t.Errorf("解码结果 = %q (stream %q), want %q", events[0].Data, events[0].Stream, want)
	}
}

// TestScanOutputPassthroughWhenNoDecoder 确认没有解码器（非 Windows，或
// 代码页未知/UTF-8）时输出原样透传，不被二次破坏。
func TestScanOutputPassthroughWhenNoDecoder(t *testing.T) {
	t.Parallel()

	input := []byte("go build ./... ok 中文 ok\nexit 1\r\n")
	events := collectScan(t, nil, input)
	want := []string{"go build ./... ok 中文 ok", "exit 1"}
	if len(events) != len(want) {
		t.Fatalf("scanOutput 产出 %d 个事件 = %+v, want %d", len(events), events, len(want))
	}
	for i, w := range want {
		if events[i].Data != w {
			t.Errorf("行 %d = %q, want %q", i, events[i].Data, w)
		}
	}
}

// TestScanOutputRoundTripsASCII 用 GB18030 解码器处理纯 ASCII 内容，
// 确认管道解码不改变可打印 ASCII。
func TestScanOutputRoundTripsASCII(t *testing.T) {
	t.Parallel()

	events := collectScan(t, simplifiedchinese.GB18030.NewDecoder(), []byte("go build ./... ok\n"))
	if len(events) != 1 || events[0].Data != "go build ./... ok" {
		t.Fatalf("ASCII 经解码 = %+v, want [go build ./... ok]", events)
	}
}

// TestDecoderForCodePage 校验代码页映射：UTF-8/未知直通为 nil，已知代码页
// 返回对应编码。
func TestDecoderForCodePage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cp    uint
		isNil bool
	}{
		{0, true},
		{65001, true}, // UTF-8 直通
		{936, false},  // 简体中文
		{950, false},  // 繁体中文
		{932, false},  // 日语
		{949, false},  // 韩语
		{437, false},  // 美式 OEM
		{850, false},  // 拉丁-1 OEM
		{852, false},  // 中欧 OEM
		{866, false},  // 西里尔 OEM
		{1252, true},  // 未知代码页：直通，不猜
		{99999, true}, // 未知代码页：直通，不猜
	}
	for _, tc := range cases {
		dec := decoderForCodePage(tc.cp)
		if gotNil := dec == nil; gotNil != tc.isNil {
			t.Errorf("decoderForCodePage(%d) = %v, want nil=%v", tc.cp, dec, tc.isNil)
		}
	}
}
