package upscaler

import "testing"

func TestParseImgLargerTaskResponse(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"root camel", `{"taskId":"abc"}`, "abc"},
		{"root snake", `{"task_id":"def"}`, "def"},
		{"nested data", `{"code":200,"data":{"taskId":"ghi"}}`, "ghi"},
		{"nested result pid", `{"result":{"pid":"jkl"}}`, "jkl"},
		{"deep snake numeric", `{"raw":{"data":{"result":{"task_id":9876543210123456789}}}}`, "9876543210123456789"},
		{"legacy raw code", `{"taskId":"","raw":{"code":200,"data":{"code":"raw-code-123"}}}`, "raw-code-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := parseImgLargerTaskResponse([]byte(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("task ID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseImgLargerTaskResponseMessage(t *testing.T) {
	got, message, err := parseImgLargerTaskResponse([]byte(`{"code":403,"message":"image too large"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || message != "image too large" {
		t.Fatalf("got task=%q message=%q", got, message)
	}
}

func TestParseImgLargerStatusResponse(t *testing.T) {
	state, urls, message := parseImgLargerStatusResponse([]byte(`{"raw":{"data":{"status":"success","downloadUrls":["https://cdn.test/result.jpg"],"result":{"output_url":"https://cdn.test/result.jpg"}}}}`))
	if state != "success" || message != "" {
		t.Fatalf("state=%q message=%q", state, message)
	}
	if len(urls) != 1 || urls[0] != "https://cdn.test/result.jpg" {
		t.Fatalf("urls=%v", urls)
	}
}
