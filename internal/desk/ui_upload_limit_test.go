package desk

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// 浏览器上传路径的大小上限：超限 413；限内但对端不存在走业务错误 400。
func TestUIUploadOverLimit(t *testing.T) {
	cfg, _ := DefaultConfig()
	cfg.ControlPort = 0
	cfg.DiscoveryPort = 0
	cfg.MaxFileBytes = 1024
	h := NewHub(cfg, "unused", Platform{}, nil)
	addr, err := StartBrowserUI(h, 0)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr
	mkBody := func(n int) string {
		return "-----X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n" +
			"Content-Type: application/octet-stream\r\n\r\n" + strings.Repeat("A", n) + "\r\n-----X--\r\n"
	}
	post := func(n int) int {
		req, _ := http.NewRequest("POST", base+"/api/upload?peer=p1&t="+h.Token(),
			strings.NewReader(mkBody(n)))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=---X")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(4096); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限上传应 413，实际 %d", got)
	}
	if got := post(100); got != http.StatusBadRequest {
		t.Fatalf("限内上传（对端不存在）应 400 业务错误，实际 %d", got)
	}
	fmt.Println("upload limit ok")
}
