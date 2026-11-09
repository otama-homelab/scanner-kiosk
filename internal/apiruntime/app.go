// Package apiruntime composes the HTTP API from deployment configuration. The
// frontend, document routes and scanner readiness are still being integrated.
package apiruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/capabilityhttp"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/diskspace"
	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/importupload"
	"scanner-kiosk/internal/oidcattempt"
	"scanner-kiosk/internal/oidcflow"
	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/oidcprovider"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanadmission"
	"scanner-kiosk/internal/simpleauth"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
	"scanner-kiosk/internal/webui"
	"scanner-kiosk/internal/workercatalog"
)

var (
	ErrConfig  = errors.New("invalid API runtime configuration")
	ErrRoot    = errors.New("API storage roots must be existing private directories")
	ErrStorage = errors.New("API storage unavailable")
	ErrOIDC    = errors.New("configured OIDC provider unavailable")
	ErrListen  = errors.New("API listener unavailable")
)

type App struct {
	state, docs    *sql.DB
	handler        http.Handler
	closed         atomic.Bool
	once           sync.Once
	meter          *diskspace.Meter
	files          *artifactread.Root
	imports        *importupload.Service
	closePaperless func()
}

func New(ctx context.Context, c config.Config) (*App, error) {
	if c.Role != config.API || c.AuthenticateType != "guest" && c.AuthenticateType != "oidc" && c.AuthenticateType != "simple" && c.AuthenticateType != "none" {
		return nil, ErrConfig
	}
	for _, key := range []string{"SCANNER_API_MAX_INFLIGHT", "SCANNER_API_REQUESTS_PER_MINUTE", "SCANNER_API_BURST"} {
		if c.Limits[key] < 1 {
			return nil, ErrConfig
		}
	}
	transferTimeout := c.Durations["SCANNER_TRANSFER_TIMEOUT_SECONDS"]
	if transferTimeout < time.Second || transferTimeout > 15*time.Minute {
		return nil, ErrConfig
	}
	for _, root := range []string{c.StateDir, c.DocumentDir} {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, ErrRoot
		}
	}
	state, err := storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State)
	if err != nil {
		return nil, ErrStorage
	}
	docs, err := storage.Open(filepath.Join(c.DocumentDir, "documents.sqlite3"), storage.Documents)
	if err != nil {
		state.Close()
		return nil, ErrStorage
	}
	app := &App{state: state, docs: docs}
	success := false
	defer func() {
		if !success {
			app.Close()
		}
	}()
	var flow authport.OIDC
	if c.AuthenticateType == "oidc" {
		provider, err := oidcprovider.New(ctx, oidcprovider.Options{Issuer: c.OIDC.Issuer, ClientID: c.OIDC.ClientID, ClientAuth: c.OIDC.ClientAuth, ClientSecret: c.OIDC.ClientSecret, Scopes: c.OIDC.Scopes, AdminGroups: c.OIDC.AdminGroups, RedirectURL: strings.TrimSuffix(c.PublicURL, "/") + "/auth/oidc/callback", Timeout: 15 * time.Second, MaxResponseBytes: 1 << 20})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrOIDC
		}
		attemptRecords, err := oidcattempt.NewSQLiteRecords(state)
		if err != nil {
			return nil, ErrStorage
		}
		attempts, err := oidcattempt.NewWithRecords(attemptRecords, oidcattempt.Options{Lifetime: 5 * time.Minute, MaxPending: 4, Now: time.Now})
		if err != nil {
			return nil, ErrConfig
		}
		identityRecords, err := oidcidentity.NewSQLiteRecords(state)
		if err != nil {
			return nil, ErrStorage
		}
		identities, err := oidcidentity.NewWithRecords(identityRecords, time.Now)
		if err != nil {
			return nil, ErrStorage
		}
		flow, err = oidcflow.New(attempts, provider, identities)
		if err != nil {
			return nil, ErrOIDC
		}
	}
	var password authport.Password
	if c.AuthenticateType == "simple" {
		records, err := simpleauth.NewSQLiteRecords(state, time.Now)
		if err != nil {
			return nil, ErrStorage
		}
		password, err = simpleauth.New(records)
		if err != nil {
			return nil, ErrConfig
		}
	}
	sessionRecords, err := websession.NewSQLiteRecords(state)
	if err != nil {
		return nil, ErrStorage
	}
	sessions, err := websession.NewWithRecords(sessionRecords, websession.Options{PublicURL: c.PublicURL, AuthenticateType: c.AuthenticateType, Lifetime: c.Durations["SCANNER_SESSION_LIFETIME_SECONDS"], IdleTimeout: c.Durations["SCANNER_SESSION_IDLE_SECONDS"], ReceiptLifetime: c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"], OIDC: flow, Password: password, LoginRequestsPerMinute: c.Limits["SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE"], LoginBurst: c.Limits["SCANNER_AUTH_LOGIN_BURST"]})
	if err != nil {
		return nil, ErrConfig
	}
	app.meter, err = diskspace.Open(c.DocumentDir, c.Limits["SCANNER_MIN_FREE_BYTES"])
	if err != nil {
		return nil, ErrStorage
	}
	documentStore, err := documents.New(state, docs, time.Now, app.meter.Available)
	if err != nil {
		return nil, ErrStorage
	}
	app.files, err = artifactread.OpenRoot(c.DocumentDir, c.Limits["SCANNER_JOB_MAX_BYTES"])
	if err != nil {
		return nil, ErrStorage
	}
	catalogReader, err := workercatalog.NewReader(docs, time.Now)
	if err != nil {
		return nil, ErrStorage
	}
	renderService := &renderadmission.Service{Records: documentStore, Resolver: &renderadmission.CatalogResolver{Source: catalogReader, Now: time.Now}, Limits: documents.AdmissionLimits{OCRMaxPagePixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"], UserMaxBytes: c.Limits["SCANNER_USER_MAX_BYTES"], GlobalMaxBytes: c.Limits["SCANNER_GLOBAL_MAX_BYTES"], JobMaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPages: int(c.Limits["SCANNER_JOB_MAX_PAGES"]), JobTimeout: c.Durations["SCANNER_OCR_TIMEOUT_SECONDS"], ReceiptRetention: c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]}}
	scannerIDs := make([]string, 0, len(c.Scanners))
	for _, device := range c.Scanners {
		scannerIDs = append(scannerIDs, device.ID)
	}
	scanService, err := scanadmission.New(documentStore, scannerIDs, documents.ScanLimits{DiscoverScanners: c.ScannerDiscovery, AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: c.Limits["SCANNER_USER_MAX_BYTES"], GlobalMaxBytes: c.Limits["SCANNER_GLOBAL_MAX_BYTES"], JobMaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPages: int(c.Limits["SCANNER_JOB_MAX_PAGES"]), JobTimeout: c.Durations["SCANNER_SCAN_TIMEOUT_SECONDS"], ReceiptRetention: c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]}, MaxDocuments: int(c.Limits["SCANNER_USER_MAX_DOCUMENTS"]), MaxPagePixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"], DocumentLifetime: c.Durations["SCANNER_RETENTION_SECONDS"]})
	if err != nil {
		return nil, ErrConfig
	}
	app.imports, err = importupload.New(c.DocumentDir, documentStore, documents.ImportLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: c.Limits["SCANNER_USER_MAX_BYTES"], GlobalMaxBytes: c.Limits["SCANNER_GLOBAL_MAX_BYTES"], JobMaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPages: int(c.Limits["SCANNER_JOB_MAX_PAGES"]), JobTimeout: c.Durations["SCANNER_OCR_TIMEOUT_SECONDS"], ReceiptRetention: c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"]}, MaxDocuments: int(c.Limits["SCANNER_USER_MAX_DOCUMENTS"]), DocumentLifetime: c.Durations["SCANNER_RETENTION_SECONDS"]})
	if err != nil {
		return nil, ErrStorage
	}
	paperlessState, deliveryService, closePaperless, err := paperlessCapability(c.Paperless, documentStore)
	if err != nil {
		return nil, ErrConfig
	}
	app.closePaperless = closePaperless
	documentRoutes := documenthttp.New(documentStore, sessions, c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"], app.files, transferTimeout, renderService, scanService, app.imports, deliveryService)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, "ok") })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if app.closed.Load() {
			respond(w, 503, "unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if app.state.PingContext(ctx) != nil || app.docs.PingContext(ctx) != nil {
			respond(w, 503, "unavailable")
			return
		}
		respond(w, 200, "ok")
	})
	scanners := make([]capabilityhttp.Scanner, 0, len(c.Scanners))
	for _, device := range c.Scanners {
		scanners = append(scanners, capabilityhttp.Scanner{ID: device.ID, Name: device.Name})
	}
	capabilityRoutes := capabilityhttp.New(catalogReader, catalogReader, documentStore, sessions, scanners, capabilityhttp.Limits{DiscoverScanners: c.ScannerDiscovery, RetentionSeconds: int64(c.Durations["SCANNER_RETENTION_SECONDS"] / time.Second), MaxDocuments: c.Limits["SCANNER_USER_MAX_DOCUMENTS"], UserMaxBytes: c.Limits["SCANNER_USER_MAX_BYTES"], JobMaxPages: int(c.Limits["SCANNER_JOB_MAX_PAGES"]), JobMaxBytes: c.Limits["SCANNER_JOB_MAX_BYTES"], MaxPagePixels: c.Limits["SCANNER_MAX_PAGE_PIXELS"]}, time.Now, paperlessState)
	mux.Handle("/api/capabilities", capabilityRoutes)
	mux.Handle("/api/capabilities/", capabilityRoutes)
	mux.Handle("/api/scanners", capabilityRoutes)
	mux.Handle("/api/session", sessions)
	mux.Handle("/api/documents", documentRoutes)
	mux.Handle("/api/documents/", documentRoutes)
	mux.Handle("/api/jobs/", documentRoutes)
	mux.Handle("/api/deliveries/", documentRoutes)
	mux.Handle("/auth/", sessions)
	frontend, frontendErr := webui.New(c.PublicURL)
	if frontendErr != nil && !errors.Is(frontendErr, fs.ErrNotExist) {
		return nil, ErrConfig
	}
	if frontendErr == nil {
		mux.Handle("/", frontend)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"code": "not_found", "message": "Not found"})
		})
	}
	app.handler = bounded(mux, int(c.Limits["SCANNER_API_MAX_INFLIGHT"]), float64(c.Limits["SCANNER_API_REQUESTS_PER_MINUTE"])/60, float64(c.Limits["SCANNER_API_BURST"]), transferTimeout, min(c.Limits["SCANNER_JOB_MAX_BYTES"], 1<<30))
	success = true
	return app, nil
}

func paperlessCapability(p config.Paperless, records documenthttp.DeliveryRecords) (capabilityhttp.Paperless, *documenthttp.DeliveryService, func(), error) {
	state := capabilityhttp.Paperless{Enabled: p.Enabled, Configured: p.Configured, ProcessingDestination: p.DestinationLabel}
	client, err := p.NewClient()
	if err != nil {
		return capabilityhttp.Paperless{}, nil, nil, err
	}
	if client != nil {
		state.Health = client.Health
		return state, &documenthttp.DeliveryService{Records: records, DestinationID: p.DestinationID, ConfigFingerprint: p.ConfigFingerprint, Task: client.Task, Metadata: client.Metadata}, client.Close, nil
	}
	return state, nil, nil, nil
}
func (a *App) Handler() http.Handler { return a.handler }
func (a *App) Close() error {
	var err error
	a.once.Do(func() {
		a.closed.Store(true)
		if a.closePaperless != nil {
			a.closePaperless()
		}
		if a.meter != nil {
			err = a.meter.Close()
		}
		if a.imports != nil {
			err = errors.Join(err, a.imports.Close())
		}
		if a.files != nil {
			err = errors.Join(err, a.files.Close())
		}
		err = errors.Join(err, a.docs.Close(), a.state.Close())
	})
	return err
}
func respond(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"status": value})
}
