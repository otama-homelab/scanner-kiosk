package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ValidateScannerEndpoints rejects registrations that point at the same known
// endpoint. It performs no discovery or DNS lookup. DNS aliases and HTTP/HTTPS
// endpoints can still refer to one physical device; deployment must register
// each physical scanner once and establish exclusive ownership separately.
func ValidateScannerEndpoints(scanners []Scanner) error {
	seen := map[string]bool{}
	for _, scanner := range scanners {
		if !scannerURL(scanner.Endpoint) {
			return fmt.Errorf("invalid scanner endpoints")
		}
		u, err := url.Parse(scanner.Endpoint)
		if err != nil || u.ForceQuery || u.Opaque != "" {
			return fmt.Errorf("invalid scanner endpoints")
		}
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		}
		port := u.Port()
		if port != "" {
			number, _ := strconv.Atoi(port)
			port = strconv.Itoa(number)
		} else if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
		key := u.Scheme + "://" + net.JoinHostPort(host, port) + strings.TrimRight(u.Path, "/")
		if seen[key] {
			return fmt.Errorf("duplicate scanner endpoints")
		}
		seen[key] = true
	}
	return nil
}
