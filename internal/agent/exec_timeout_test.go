package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// TestExecTimeoutDoesNotHangOnGrandchild：超时必须真的能收场。
//
// exec.CommandContext 超时只杀直接子进程，孙子进程还活着，而它继承了
// 同一份 stdout/stderr 管道 —— 管道不 EOF，scanOutput 就不返回，
// `for event := range events` 永远不结束，整个 handler 卡死在那里，
// 后面那句 terminateProcessTree 根本走不到。
//
// 现场表现：一条 `powershell.exe -File build.ps1` 只要脚本里起了后台
// 进程，这次 exec 就再也不返回，连接一直挂着。
func TestExecTimeoutDoesNotHangOnGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("这条用 sh 造孙子进程，Windows 上另说")
	}
	s := testServer(t)
	body, _ := json.Marshal(ExecRequest{
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 60 & echo started; sleep 60"},
		TimeoutMS: 800,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/exec", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(res, req)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("超时之后 exec 没有收场：孙子进程还攥着管道，handler 卡死了")
	}

	if !bytes.Contains(res.Body.Bytes(), []byte(`"stream":"exit"`)) {
		t.Fatalf("没有收到 exit 事件: %s", res.Body.String())
	}
}
