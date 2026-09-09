package adkvercel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	adkmodels "github.com/Alcova-AI/adk-models-go"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestRetryTransport(t *testing.T) {
	for _, status := range []int{408, 409, 429, 500, 503, 529, 400, 401, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			var bodies []*trackedBody
			tr := &retryTransport{sleep: func(context.Context, time.Duration) error { return nil }, base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				payload, _ := io.ReadAll(req.Body)
				_ = req.Body.Close()
				if string(payload) != "payload" {
					t.Fatalf("body = %q", payload)
				}
				body := &trackedBody{Reader: strings.NewReader("response")}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}, nil
			})}
			req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", strings.NewReader("payload"))
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if status == 408 || status == 409 || status == 429 || status >= 500 {
				want = 3
			}
			if calls != want {
				t.Fatalf("calls %d want %d", calls, want)
			}
			for _, b := range bodies[:len(bodies)-1] {
				if !b.closed {
					t.Fatal("discarded response not closed")
				}
			}
			if bodies[len(bodies)-1].closed {
				t.Fatal("final response closed")
			}
			_ = resp.Body.Close()
		})
	}
}

func TestRetryConnectionAndHeaders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header string
		want   int
	}{{"connection", 0, "", 3}, {"deny", 503, "false", 1}, {"allow", 400, "true", 3}, {"successful stream", 200, "true", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			tr := &retryTransport{sleep: func(context.Context, time.Duration) error { return nil }, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.status == 0 {
					return nil, errors.New("connection failed")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-Should-Retry": []string{tc.header}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
			resp, _ := tr.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if calls != tc.want {
				t.Fatalf("calls %d", calls)
			}
		})
	}
}

func TestRetryCancellationAndNonReplayableBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	tr := &retryTransport{sleep: func(ctx context.Context, d time.Duration) error { cancel(); return retrySleep(ctx, time.Hour) }, base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
	_, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	calls = 0
	req, _ = http.NewRequest(http.MethodPost, "https://example.invalid", io.NopCloser(strings.NewReader("body")))
	resp, err := tr.RoundTrip(req)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	_ = resp.Body.Close()
}

func TestRetryDelay(t *testing.T) {
	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	for _, value := range []string{"2", "0", now.Add(10 * time.Second).UTC().Format(http.TimeFormat)} {
		d := retryDelay(&http.Response{Header: http.Header{"Retry-After": []string{value}}}, 0, now)
		if value == "2" && d != 2*time.Second {
			t.Fatal(d)
		}
		if value == "0" && d != 0 {
			t.Fatal(d)
		}
		if strings.Contains(value, "GMT") && d != 10*time.Second {
			t.Fatal(d)
		}
	}
	for _, value := range []string{"invalid", "-1", "999999999999999999"} {
		d := retryDelay(&http.Response{Header: http.Header{"Retry-After": []string{value}}}, 1, now)
		if d < 750*time.Millisecond || d > time.Second {
			t.Fatal(d)
		}
	}
}

func TestDefaultRetryClientAndSuppliedClient(t *testing.T) {
	cfg := Config{APIKey: "test", Model: adkmodels.ModelConfig{CanonicalModel: "gpt-test"}}
	llm, err := NewModel(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client := llm.(*gatewayModel).httpClient
	if client == http.DefaultClient {
		t.Fatal("shared default client")
	}
	if _, ok := client.Transport.(*retryTransport); !ok {
		t.Fatal("missing retries")
	}
	supplied := &http.Client{}
	cfg.HTTPClient = supplied
	llm, err = NewModel(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if llm.(*gatewayModel).httpClient != supplied {
		t.Fatal("supplied client replaced")
	}
}
