package soroban

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rpcServer answers JSON-RPC calls using the supplied handler, which returns
// the raw `result` JSON for each request body.
func rpcServer(t *testing.T, handle func(method string, params json.RawMessage) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, handle(req.Method, req.Params))
	}))
}

func TestSend_TryAgainLaterIsError(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"TRY_AGAIN_LATER","hash":"abc123"}`
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).Send("AAAA")
	if err == nil {
		t.Fatal("TRY_AGAIN_LATER must be an error — the tx was not accepted")
	}
	if !IsRetryable(err) {
		t.Fatalf("TRY_AGAIN_LATER should be retryable (pre-broadcast), got %v", err)
	}
}

func TestSend_DuplicateReturnsHash(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"DUPLICATE","hash":"abc123"}`
	})
	defer srv.Close()

	hash, err := NewClient(srv.URL).Send("AAAA")
	if err != nil {
		t.Fatalf("DUPLICATE means already in flight; awaiting it is correct: %v", err)
	}
	if hash != "abc123" {
		t.Fatalf("expected hash abc123, got %q", hash)
	}
}

func TestSend_ErrorStatus(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"ERROR","errorResultXdr":""}`
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).Send("AAAA")
	if err == nil {
		t.Fatal("expected error for ERROR status")
	}
}

func TestAwaitTx_TimeoutIsStatusUnknown_NotRetryable(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"NOT_FOUND"}`
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).AwaitTx("cafebabe12345678", 10*time.Millisecond)
	if !errors.Is(err, ErrTxStatusUnknown) {
		t.Fatalf("expected ErrTxStatusUnknown, got %v", err)
	}
	if IsRetryable(err) {
		t.Fatal("a tx with unknown status must NEVER be retryable — re-broadcast could double-execute")
	}
}

func TestAwaitTx_ShortHashDoesNotPanic(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"NOT_FOUND"}`
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).AwaitTx("ab", 10*time.Millisecond) // would panic on hash[:8] before
	if !errors.Is(err, ErrTxStatusUnknown) {
		t.Fatalf("expected ErrTxStatusUnknown, got %v", err)
	}
}

func TestAwaitTx_Success(t *testing.T) {
	srv := rpcServer(t, func(string, json.RawMessage) string {
		return `{"status":"SUCCESS","resultXdr":"AAAA"}`
	})
	defer srv.Close()

	res, err := NewClient(srv.URL).AwaitTx("cafebabe12345678", time.Second)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Hash != "cafebabe12345678" || res.Status != "SUCCESS" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestCall_HTTPErrorStatusSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).LatestLedger()
	if err == nil || !strings.Contains(err.Error(), "http 502") {
		t.Fatalf("expected http 502 error, got %v", err)
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":404}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).GetAccount(srv.URL, "GABC")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("404 must be an explicit error (was silently seq=0 before), got %v", err)
	}
}

func TestGetAccount_BadSequence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sequence":"not-a-number"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).GetAccount(srv.URL, "GABC")
	if err == nil || !strings.Contains(err.Error(), "bad sequence") {
		t.Fatalf("expected bad-sequence error, got %v", err)
	}
}

func TestGetAccount_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sequence":"123456789"}`))
	}))
	defer srv.Close()

	seq, err := NewClient(srv.URL).GetAccount(srv.URL, "GABC")
	if err != nil || seq != 123456789 {
		t.Fatalf("expected 123456789, got %d (%v)", seq, err)
	}
}

func TestGetEvents_FollowsPagination(t *testing.T) {
	page := 0
	srv := rpcServer(t, func(method string, params json.RawMessage) string {
		if method != "getEvents" {
			t.Errorf("unexpected method %s", method)
		}
		page++
		if page == 1 {
			// Full first page → client must fetch the next one via the cursor.
			events := make([]string, eventPageLimit)
			for i := range events {
				events[i] = fmt.Sprintf(`{"type":"contract","ledger":%d,"pagingToken":"tok-%d"}`, i+1, i+1)
			}
			return `{"events":[` + strings.Join(events, ",") + `],"cursor":"cursor-1"}`
		}
		// Second page: cursor must be present, startLedger must not.
		var p struct {
			StartLedger *int64 `json:"startLedger"`
			Pagination  struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("params: %v", err)
		}
		if p.StartLedger != nil {
			t.Error("startLedger must be omitted when paginating with a cursor")
		}
		if p.Pagination.Cursor != "cursor-1" {
			t.Errorf("expected cursor-1, got %q", p.Pagination.Cursor)
		}
		return `{"events":[{"type":"contract","ledger":999}]}`
	})
	defer srv.Close()

	events, err := NewClient(srv.URL).GetEvents(100, "CCONTRACT")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(events) != eventPageLimit+1 {
		t.Fatalf("expected %d events across 2 pages, got %d", eventPageLimit+1, len(events))
	}
	if page != 2 {
		t.Fatalf("expected 2 pages fetched, got %d", page)
	}
}

func TestGetEvents_ShortPageStops(t *testing.T) {
	calls := 0
	srv := rpcServer(t, func(string, json.RawMessage) string {
		calls++
		return `{"events":[{"type":"contract","ledger":1}]}`
	})
	defer srv.Close()

	events, err := NewClient(srv.URL).GetEvents(100, "CCONTRACT")
	if err != nil || len(events) != 1 || calls != 1 {
		t.Fatalf("short page should stop after 1 call: events=%d calls=%d err=%v", len(events), calls, err)
	}
}

func TestShortHash(t *testing.T) {
	cases := map[string]string{
		"":                 "<no hash>",
		"ab":               "ab",
		"cafebabe":         "cafebabe",
		"cafebabe12345678": "cafebabe",
	}
	for in, want := range cases {
		if got := shortHash(in); got != want {
			t.Errorf("shortHash(%q)=%q want %q", in, got, want)
		}
	}
}
