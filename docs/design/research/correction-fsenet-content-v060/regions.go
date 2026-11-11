//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"scanner-kiosk/internal/evaluation"
)

func main() {
	var in struct {
		Paths   []string                        `json:"catalog_paths"`
		Oracles evaluation.CorrectionOracleFile `json:"oracles"`
	}
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		panic(e)
	}
	x, e := evaluation.AnalyzeCorrectionCatalogs(in.Paths, in.Oracles)
	if e != nil {
		panic(e)
	}
	if e = json.NewEncoder(os.Stdout).Encode(x); e != nil {
		panic(e)
	}
}
