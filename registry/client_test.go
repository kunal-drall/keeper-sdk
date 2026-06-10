package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/Nectar-Network/keeper-sdk/soroban"
)

const testRegistry = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
const testAddr = "GCCYJT7KHQZ235LCND5DKNRBNGZ4DRDPP24R3M5TMWUPJRRQDRVZDEMF"

// simServer answers every simulateTransaction with the given result payload.
func simServer(t *testing.T, resultJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + resultJSON + `}`))
	}))
}

func scValB64(t *testing.T, v xdr.ScVal) string {
	t.Helper()
	b64, err := xdr.MarshalBase64(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b64
}

// get_keeper returning Option::None (void) means "not registered" — this used
// to incorrectly report true.
func TestIsRegistered_VoidResultIsFalse(t *testing.T) {
	srv := simServer(t, `{"latestLedger":1,"results":[{"xdr":"`+scValB64(t, soroban.ScvVoid())+`"}]}`)
	defer srv.Close()

	ok, err := IsRegistered(soroban.NewClient(srv.URL), "Test SDF Network ; September 2015", testRegistry, testAddr)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if ok {
		t.Fatal("a void get_keeper result must report not-registered")
	}
}

func TestIsRegistered_StructResultIsTrue(t *testing.T) {
	srv := simServer(t, `{"latestLedger":1,"results":[{"xdr":"`+scValB64(t, soroban.ScvString("keeper-alpha"))+`"}]}`)
	defer srv.Close()

	ok, err := IsRegistered(soroban.NewClient(srv.URL), "Test SDF Network ; September 2015", testRegistry, testAddr)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !ok {
		t.Fatal("a non-void get_keeper result must report registered")
	}
}

func TestIsRegistered_NotRegisteredErrorIsFalse(t *testing.T) {
	srv := simServer(t, `{"latestLedger":1,"error":"HostError: Error(Contract, NotRegistered)"}`)
	defer srv.Close()

	ok, err := IsRegistered(soroban.NewClient(srv.URL), "Test SDF Network ; September 2015", testRegistry, testAddr)
	if err != nil {
		t.Fatalf("NotRegistered should be a clean false, got error: %v", err)
	}
	if ok {
		t.Fatal("expected false")
	}
}

func TestIsRegistered_NoResultsIsFalse(t *testing.T) {
	srv := simServer(t, `{"latestLedger":1}`)
	defer srv.Close()

	ok, err := IsRegistered(soroban.NewClient(srv.URL), "Test SDF Network ; September 2015", testRegistry, testAddr)
	if err != nil || ok {
		t.Fatalf("no results should be (false, nil), got (%v, %v)", ok, err)
	}
}

func TestErrorMatchers(t *testing.T) {
	if !isAlreadyRegistered("Error(Contract, AlreadyRegistered)") {
		t.Error("camel-case AlreadyRegistered should match")
	}
	if !isAlreadyRegistered("keeper already registered") {
		t.Error("spaced lowercase should match")
	}
	if !isNotRegistered("HostError: NotRegistered") {
		t.Error("NotRegistered should match")
	}
	if isNotRegistered("AlreadyRegistered") {
		t.Error("AlreadyRegistered must not match NotRegistered")
	}
}
