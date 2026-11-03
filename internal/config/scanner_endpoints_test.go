package config

import (
	"encoding/json"
	"testing"
)

func TestDuplicatePhysicalEndpointRegistrationsRejected(t *testing.T) {
	for _, pair := range [][2]string{
		{"http://scanner.local/eSCL", "http://scanner.local/eSCL"},
		{"http://scanner.local/eSCL", "http://SCANNER.local.:80/eSCL/"},
		{"https://scanner.local/eSCL", "https://scanner.local:0443/eSCL"},
		{"http://192.0.2.10/eSCL", "http://192.0.2.10:80/%65SCL"},
		{"http://[2001:db8::1]/eSCL", "http://[2001:0db8:0:0:0:0:0:1]:80/eSCL/"},
	} {
		scanners := []Scanner{{ID: "scanner_example_0001", Name: "A", Driver: "sane-airscan", Endpoint: pair[0]}, {ID: "scanner_example_0002", Name: "B", Driver: "sane-airscan", Endpoint: pair[1]}}
		encoded, _ := json.Marshal(scanners)
		values := base()
		values["SCANNER_SCANNERS_JSON"] = string(encoded)
		if _, err := Load(env(values)); err == nil {
			t.Fatal("same endpoint registered under two device IDs")
		}
	}
	values := base()
	values["SCANNER_SCANNERS_JSON"] = `[{"id":"scanner_example_0001","name":"A","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"},{"id":"scanner_example_0002","name":"B","driver":"sane-airscan","endpoint":"http://192.0.2.11/eSCL"}]`
	c, err := Load(env(values))
	if err != nil || len(c.Scanners) != 2 {
		t.Fatal("distinct scanners rejected", err)
	}
}
