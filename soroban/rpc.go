// Package soroban is a thin Soroban JSON-RPC client plus ScVal builders. It
// covers exactly what a keeper needs: simulate, send, await, events, ledger
// entries, and account sequence lookup — with transaction-safety semantics
// (a transaction whose fate is unknown is never silently retried).
package soroban

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stellar/go/xdr"
)

// ErrTxStatusUnknown marks a transaction that was accepted by the network but
// whose result could not be confirmed before the await deadline. It may still
// be included in a ledger. Callers must NOT re-broadcast state-changing calls
// on this error — a second submission could double-execute (double draw,
// double fill). Recovery happens on the next keeper cycle instead.
var ErrTxStatusUnknown = errors.New("transaction status unknown (sent but unconfirmed)")

// txPollInterval is how often AwaitTx polls getTransaction (~ledger close time).
const txPollInterval = 3 * time.Second

// Client is a minimal Soroban JSON-RPC client. It is safe for concurrent use.
type Client struct {
	url  string
	http *http.Client
	seq  atomic.Int64
}

// NewClient builds a Client for the given Soroban RPC URL with a 30s HTTP timeout.
func NewClient(url string) *Client {
	return &Client{url: url, http: &http.Client{Timeout: 30 * time.Second}}
}

// SimulateResult is the decoded simulateTransaction response.
type SimulateResult struct {
	Results         []SimEntry `json:"results,omitempty"`
	Error           string     `json:"error,omitempty"`
	TransactionData string     `json:"transactionData,omitempty"`
	MinResourceFee  string     `json:"minResourceFee,omitempty"`
	LatestLedger    int64      `json:"latestLedger"`
}

// SimEntry is one simulation result entry (return value XDR plus auth).
type SimEntry struct {
	XDR  string   `json:"xdr"`
	Auth []string `json:"auth,omitempty"`
}

// TxResult is the decoded getTransaction response for a confirmed transaction.
type TxResult struct {
	Status         string `json:"status"`
	Hash           string
	ResultXDR      string `json:"resultXdr,omitempty"`
	ErrorResultXDR string `json:"errorResultXdr,omitempty"`
}

// Event is one contract event from getEvents.
type Event struct {
	Type        string   `json:"type"`
	ContractID  string   `json:"contractId"`
	Topic       []string `json:"topic"`
	Value       string   `json:"value"`
	Ledger      int64    `json:"ledger"`
	PagingToken string   `json:"pagingToken,omitempty"`
}

// Simulate runs simulateTransaction for the given base64 envelope XDR.
func (c *Client) Simulate(txXDR string) (*SimulateResult, error) {
	var r SimulateResult
	return &r, c.call("simulateTransaction", map[string]string{"transaction": txXDR}, &r)
}

// Send submits a signed transaction. It returns the hash when the network
// accepted it (PENDING or DUPLICATE — a duplicate is already in flight, so
// awaiting the same hash is correct). ERROR and TRY_AGAIN_LATER mean the
// transaction was NOT accepted; both return an error and are safe to retry.
func (c *Client) Send(txXDR string) (string, error) {
	var r struct {
		Hash           string `json:"hash"`
		Status         string `json:"status"`
		ErrorResultXDR string `json:"errorResultXdr"`
	}
	if err := c.call("sendTransaction", map[string]string{"transaction": txXDR}, &r); err != nil {
		return "", err
	}
	switch r.Status {
	case "ERROR":
		return "", fmt.Errorf("send tx rejected: %s", decodeTxResultCode(r.ErrorResultXDR))
	case "TRY_AGAIN_LATER":
		return "", errors.New("send tx: try_again_later (queue full, not accepted)")
	}
	return r.Hash, nil
}

// AwaitTx polls getTransaction until the transaction succeeds, fails, or the
// timeout elapses. A FAILED transaction is definitive (it was included and did
// not apply). A timeout returns ErrTxStatusUnknown — the transaction may still
// land, so callers must not re-broadcast. Transient poll errors are retried
// until the deadline rather than abandoning a transaction already in flight.
func (c *Client) AwaitTx(hash string, timeout time.Duration) (*TxResult, error) {
	deadline := time.Now().Add(timeout)
	var lastPollErr error
	for {
		var r TxResult
		if err := c.call("getTransaction", map[string]string{"hash": hash}, &r); err != nil {
			lastPollErr = err
		} else {
			lastPollErr = nil
			r.Hash = hash
			switch r.Status {
			case "SUCCESS":
				return &r, nil
			case "FAILED":
				return nil, fmt.Errorf("tx %s failed in ledger: %s", shortHash(hash), decodeTxResultCode(r.ResultXDR))
			}
		}
		if time.Now().After(deadline) {
			if lastPollErr != nil {
				return nil, fmt.Errorf("tx %s: %w (last poll error: %v)", shortHash(hash), ErrTxStatusUnknown, lastPollErr)
			}
			return nil, fmt.Errorf("tx %s: %w after %s", shortHash(hash), ErrTxStatusUnknown, timeout)
		}
		time.Sleep(txPollInterval)
	}
}

// Event pagination bounds: pages are fetched until a short page, the cursor
// stops advancing, or maxEventPages is hit (so a noisy contract cannot stall a
// keeper cycle indefinitely).
const (
	eventPageLimit = 200
	maxEventPages  = 10
)

// GetEvents fetches contract events from startLedger onward, following
// pagination cursors so busy contracts do not silently lose events past the
// first page. Already-fetched events are returned alongside any mid-pagination
// error.
func (c *Client) GetEvents(startLedger int64, contractID string) ([]Event, error) {
	var all []Event
	cursor := ""
	for page := 0; page < maxEventPages; page++ {
		var r struct {
			Events []Event `json:"events"`
			Cursor string  `json:"cursor"`
		}
		params := map[string]any{
			"filters": []map[string]any{
				{"type": "contract", "contractIds": []string{contractID}},
			},
		}
		pagination := map[string]any{"limit": eventPageLimit}
		if cursor == "" {
			params["startLedger"] = startLedger
		} else {
			// Per the RPC spec, a cursor replaces startLedger.
			pagination["cursor"] = cursor
		}
		params["pagination"] = pagination
		if err := c.call("getEvents", params, &r); err != nil {
			return all, err
		}
		all = append(all, r.Events...)
		if len(r.Events) < eventPageLimit {
			return all, nil
		}
		next := r.Cursor
		if next == "" {
			next = r.Events[len(r.Events)-1].PagingToken
		}
		if next == "" || next == cursor {
			return all, nil // cannot advance — stop rather than spin
		}
		cursor = next
	}
	return all, nil
}

// LatestLedger returns the most recent ledger sequence known to the RPC node.
func (c *Client) LatestLedger() (int64, error) {
	var r struct {
		Sequence int64 `json:"sequence"`
	}
	return r.Sequence, c.call("getLatestLedger", nil, &r)
}

// LedgerEntry is a single getLedgerEntries result entry.
type LedgerEntry struct {
	Key                string `json:"key"`
	XDR                string `json:"xdr"`
	LastModifiedLedger int64  `json:"lastModifiedLedgerSeq"`
	LiveUntilLedgerSeq int64  `json:"liveUntilLedgerSeq,omitempty"`
}

// GetLedgerEntries fetches raw ledger entries for the given base64-XDR keys.
// Useful for direct contract-storage lookups when SimulateRead is overkill.
func (c *Client) GetLedgerEntries(keys []string) ([]LedgerEntry, error) {
	var r struct {
		Entries []LedgerEntry `json:"entries"`
	}
	params := map[string]any{"keys": keys}
	return r.Entries, c.call("getLedgerEntries", params, &r)
}

// GetAccount returns the current sequence number of address via Horizon. A 404
// (unfunded/missing account) and a malformed sequence are explicit errors
// rather than a silent zero, which would otherwise surface later as a baffling
// tx_bad_seq.
func (c *Client) GetAccount(horizonURL, address string) (int64, error) {
	url := fmt.Sprintf("%s/accounts/%s", strings.TrimSuffix(horizonURL, "/"), address)
	resp, err := c.http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("horizon: account %s not found — create and fund it first", address)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("horizon: get account %s: http %d: %s", address, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var r struct {
		Sequence string `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0, fmt.Errorf("horizon: decode account %s: %w", address, err)
	}
	seq, err := strconv.ParseInt(r.Sequence, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("horizon: account %s returned bad sequence %q", address, r.Sequence)
	}
	return seq, nil
}

func (c *Client) call(method string, params any, out any) error {
	id := c.seq.Add(1)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	if err != nil {
		return fmt.Errorf("rpc %s: marshal params: %w", method, err)
	}
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("rpc %s: http %d: %s", method, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var rr struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return fmt.Errorf("rpc %s: decode response: %w", method, err)
	}
	if rr.Error != nil {
		return fmt.Errorf("rpc %s: %s", method, rr.Error.Message)
	}
	if out == nil {
		return nil
	}
	if len(rr.Result) == 0 || string(rr.Result) == "null" {
		return fmt.Errorf("rpc %s: empty result", method)
	}
	return json.Unmarshal(rr.Result, out)
}

// decodeTxResultCode renders a base64 TransactionResult XDR as its result-code
// name (plus per-operation codes for txFAILED) so errors are matchable by the
// retry classifier instead of opaque base64 — which could even false-positive
// substring checks like "eof".
func decodeTxResultCode(b64 string) string {
	if b64 == "" {
		return "no error result returned"
	}
	var res xdr.TransactionResult
	if err := xdr.SafeUnmarshalBase64(b64, &res); err != nil {
		return b64 // undecodable — raw XDR is better than nothing
	}
	code := res.Result.Code.String()
	if res.Result.Code == xdr.TransactionResultCodeTxFailed && res.Result.Results != nil {
		for i, op := range *res.Result.Results {
			if op.Tr == nil {
				continue
			}
			if ih, ok := op.Tr.GetInvokeHostFunctionResult(); ok {
				code += fmt.Sprintf(" (op %d: %s)", i, ih.Code.String())
			}
		}
	}
	return code
}

// shortHash trims a tx hash for log/error lines without panicking on short input.
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	if h == "" {
		return "<no hash>"
	}
	return h
}
