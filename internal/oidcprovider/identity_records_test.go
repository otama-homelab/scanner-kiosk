package oidcprovider_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/oidcprovider"
)

type identityRecords struct {
	command oidcidentity.ResolveRecord
	id      string
	failure error
	after   func()
}

func (r *identityRecords) Resolve(_ context.Context, c oidcidentity.ResolveRecord) (string, error) {
	r.command = c
	if r.id == "" {
		r.id = c.ProposedPrincipalID
	}
	if r.after != nil {
		r.after()
	}
	return r.id, r.failure
}
func TestIdentityRecordsInjectionWithVerifiedTLSClaims(t *testing.T) {
	db, browser, _, material := browser(t)
	fixture := newIDP(t, key(t), material, "valid", "client_secret_basic")
	provider, err := oidcprovider.New(context.Background(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := provider.Exchange(context.Background(), "fixture-code", material)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"stable", "uncertain", "invalid-id", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			records := &identityRecords{}
			service, err := oidcidentity.NewWithRecords(records, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "uncertain":
				records.failure = oidcidentity.ErrUncertain
			case "invalid-id":
				records.id = "invalid"
			case "cancel":
				records.after = cancel
			}
			result, err := service.Resolve(ctx, verified, browser)
			if mode != "stable" {
				if err == nil || result.ID != "" || result.CanAdminister {
					t.Fatal("unconfirmed identity exposed")
				}
				if mode == "uncertain" && !errors.Is(err, oidcidentity.ErrUncertain) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || result.ID == "" || !result.CanAdminister {
				t.Fatal("verified identity lost", err)
			}
			again, err := service.Resolve(ctx, verified, browser)
			if err != nil || again.ID != result.ID {
				t.Fatal("identity remapped", err)
			}
			c := records.command
			if c.Issuer != verified.Issuer() || c.Subject != verified.Subject() || c.BrowserHash != sha256.Sum256([]byte(browser)) {
				t.Fatal("identity binding lost")
			}
			encoded, err := json.Marshal(c)
			if err != nil || string(encoded) != `"[redacted]"` {
				t.Fatal("identity command JSON leaked")
			}
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if fmt.Sprintf(format, c) != "[redacted]" {
					t.Fatal("identity command formatting leaked")
				}
			}
		})
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM oidc_identities").Scan(&count); err != nil || count != 0 {
		t.Fatal("injected identity service wrote SQLite", err)
	}
}
