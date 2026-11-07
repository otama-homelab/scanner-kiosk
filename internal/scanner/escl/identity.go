package escl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"
)

// IdentityObservation is an advertised UUID at a registered endpoint, not an
// authenticated hardware credential, exclusive writer fence or idle permission.
// Raw UUIDs/serials/URLs are absent and implicit output is redacted.
type IdentityObservation struct {
	DeviceID, PhysicalID string
	ObservedAtMS         int64
}

func (IdentityObservation) String() string               { return "[redacted scanner identity observation]" }
func (IdentityObservation) GoString() string             { return "[redacted scanner identity observation]" }
func (IdentityObservation) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

// ObserveIdentity performs one bounded noncached GET of ScannerCapabilities.
// No discovery, scan creation, response URI or serial fallback is used.
func (o *Observer) ObserveIdentity(ctx context.Context, deviceID string) (IdentityObservation, error) {
	endpoint, ok := o.lookup(deviceID)
	if !ok {
		return IdentityObservation{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return IdentityObservation{}, err
	}
	began := time.Now().UnixMilli()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/ScannerStatus")+"/ScannerCapabilities", nil)
	if err != nil {
		return IdentityObservation{}, ErrUnavailable
	}
	request.Header.Set("Accept", "text/xml, application/xml")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache, no-store")
	request.Header.Set("Pragma", "no-cache")
	response, err := o.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return IdentityObservation{}, ctx.Err()
		}
		return IdentityObservation{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > o.maxBytes || (response.Header.Get("Age") != "" && response.Header.Get("Age") != "0") || (response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return IdentityObservation{}, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, o.maxBytes+1))
	if ctx.Err() != nil {
		return IdentityObservation{}, ctx.Err()
	}
	if err != nil || int64(len(data)) > o.maxBytes {
		return IdentityObservation{}, ErrUnavailable
	}
	id, ok := capabilityIdentity(data)
	observed := time.Now().UnixMilli()
	if !ok || observed < began {
		return IdentityObservation{}, ErrUnavailable
	}
	if ctx.Err() != nil {
		return IdentityObservation{}, ctx.Err()
	}
	return IdentityObservation{DeviceID: deviceID, PhysicalID: id, ObservedAtMS: observed}, nil
}

func capabilityIdentity(data []byte) (string, bool) {
	root := boundedTree(data)
	if root == nil || root.name != (xml.Name{Space: scanNS, Local: "ScannerCapabilities"}) {
		return "", false
	}
	version, ok := singleText(root, pwgNS, "Version")
	if !ok || version == "" {
		return "", false
	}
	value, ok := singleText(root, scanNS, "UUID")
	if !ok {
		return "", false
	}
	if len(value) == 45 && strings.EqualFold(value[:9], "urn:uuid:") {
		value = value[9:]
	}
	if len(value) != 36 {
		return "", false
	}
	for _, index := range []int{8, 13, 18, 23} {
		if value[index] != '-' {
			return "", false
		}
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return "", false
	}
	var uuid [16]byte
	if _, err := hex.Decode(uuid[:], []byte(compact)); err != nil {
		return "", false
	}
	zero, max := true, true
	for _, b := range uuid {
		zero = zero && b == 0
		max = max && b == 255
	}
	if zero || max {
		return "", false
	}
	hash := sha256.New()
	hash.Write([]byte("scanner-kiosk/device-uuid/v1\x00"))
	hash.Write(uuid[:])
	return "physical_" + hex.EncodeToString(hash.Sum(nil)), true
}
