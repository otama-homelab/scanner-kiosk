//go:build linux

package tesseract

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"scanner-kiosk/pkg/ocrplugin"
)

const positionedTSV = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
	"1\t1\t0\t0\t0\t0\t0\t0\t80\t80\t-1\t\n" +
	"2\t1\t1\t0\t0\t0\t4\t8\t70\t60\t-1\t\n" +
	"3\t1\t1\t1\t0\t0\t4\t8\t70\t60\t-1\t\n" +
	"4\t1\t1\t1\t1\t0\t4\t8\t70\t20\t-1\t\n" +
	"5\t1\t1\t1\t1\t1\t4\t8\t30\t20\t95.5\t日本語\n" +
	"5\t1\t1\t1\t1\t2\t40\t8\t25\t20\t0\tEnglish\n"

func positionedProvider(t *testing.T, payload string, limit int64) (*Provider, Options) {
	t.Helper()
	opts := options(t)
	if limit > 0 {
		opts.MaxResultBytes = limit
	}
	// This executable fixture also rejects a second invocation for the page.
	script := "#!/bin/sh\n[ ! -e \"$2.called\" ] || exit 17\n: > \"$2.called\"\ncase \" $* \" in\n*' tessedit_create_tsv=1 '*) ;;\n*) exit 18;;\nesac\nprintf '%s' '" + strings.ReplaceAll(payload, "'", "'\"'\"'") + "' > \"$2.tsv\"\n"
	if err := os.WriteFile(opts.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return p, opts
}

func runPositioned(t *testing.T, p *Provider) (*memoryAccess, ocrplugin.Result, error) {
	t.Helper()
	req := request(p)
	req.Output = ocrplugin.PositionedText
	access := &memoryAccess{input: blankPNG(t)}
	result, err := p.Run(context.Background(), req, access)
	return access, result, err
}

func TestTesseractPositionedOutput(t *testing.T) {
	p, opts := positionedProvider(t, positionedTSV, 0)
	access, result, err := runPositioned(t, p)
	if err != nil {
		t.Fatalf("positioned recognition must succeed: %v", err)
	}
	if len(result.Pages) != 1 || result.Pages[0].Kind != ocrplugin.PositionedText || result.Pages[0].PageID != request(p).Pages[0].ID || len(access.outputs) != 1 || !access.outputs[0].closed {
		t.Fatal("positioned artifact lost page/output identity")
	}
	var page ocrplugin.PositionedPage
	if err := json.Unmarshal(access.outputs[0].Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if err := ocrplugin.ValidatePositionedPage(page, request(p).Pages[0]); err != nil {
		t.Fatal(err)
	}
	if len(page.Spans) != 2 || page.Spans[0].Text != "日本語" || page.Spans[1].Text != "English" || page.Spans[0].Box != (ocrplugin.Box{X: 4, Y: 8, Width: 30, Height: 20}) || page.Spans[0].Confidence == nil || *page.Spans[0].Confidence != .955 {
		t.Fatalf("word text/order/geometry/confidence not preserved: %+v", page)
	}
	if strings.Contains(access.outputs[0].String(), opts.WorkRoot) {
		t.Fatal("private path leaked into positioned artifact")
	}
}

func TestTesseractPositionedSingleInvocation(t *testing.T) {
	p, opts := positionedProvider(t, positionedTSV, 0)
	access, result, err := runPositioned(t, p)
	if err != nil || len(result.Pages) != 1 || access.opens != 1 {
		t.Fatalf("one recognition invocation must supply text and positions: %v", err)
	}
	files, err := os.ReadDir(opts.WorkRoot)
	if err != nil || len(files) != 0 {
		t.Fatal("positioned staging not cleaned")
	}
}

func TestTesseractPositionedParser(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		limit         int64
		valid         bool
	}{
		{"blank", strings.Join(strings.Split(positionedTSV, "\n")[:2], "\n") + "\n", 0, true},
		{"tabs-in-text", strings.Replace(positionedTSV, "日本語", "日本語\t資料", 1), 0, true},
		{"missing", "", 0, false},
		{"missing-page", strings.Split(positionedTSV, "\n")[0] + "\n", 0, false},
		{"wrong-header", strings.Replace(positionedTSV, "level", "path", 1), 0, false},
		{"newline-in-text", strings.Replace(positionedTSV, "日本語", "日本語\n資料", 1), 0, false},
		{"invalid-utf8", strings.Replace(positionedTSV, "日本語", "\xff", 1), 0, false},
		{"negative", strings.Replace(positionedTSV, "\t4\t8\t30\t20\t95.5", "\t-1\t8\t30\t20\t95.5", 1), 0, false},
		{"outside", strings.Replace(positionedTSV, "\t4\t8\t30\t20\t95.5", "\t79\t8\t30\t20\t95.5", 1), 0, false},
		{"zero-width", strings.Replace(positionedTSV, "\t4\t8\t30\t20\t95.5", "\t4\t8\t0\t20\t95.5", 1), 0, false},
		{"overflow", strings.Replace(positionedTSV, "\t4\t8\t30\t20\t95.5", "\t9223372036854775808\t8\t30\t20\t95.5", 1), 0, false},
		{"nan", strings.Replace(positionedTSV, "95.5", "NaN", 1), 0, false},
		{"infinity", strings.Replace(positionedTSV, "95.5", "+Inf", 1), 0, false},
		{"negative-confidence", strings.Replace(positionedTSV, "95.5", "-1", 1), 0, false},
		{"excess-confidence", strings.Replace(positionedTSV, "95.5", "101", 1), 0, false},
		{"wrong-page", strings.ReplaceAll(positionedTSV, "\t1\t", "\t2\t"), 0, false},
		{"duplicate-word", positionedTSV + strings.Split(positionedTSV, "\n")[5] + "\n", 0, false},
		{"unknown-level", positionedTSV + "6\t1\t1\t1\t1\t3\t4\t8\t30\t20\t95\tx\n", 0, false},
		{"over-budget", positionedTSV, 128, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, opts := positionedProvider(t, test.payload, test.limit)
			access, _, err := runPositioned(t, p)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t, got %v", test.valid, err)
			}
			if err != nil && len(access.outputs) != 0 {
				t.Fatal("malformed positioned output escaped validation")
			}
			if files, err := os.ReadDir(opts.WorkRoot); err != nil || len(files) != 0 {
				t.Fatal("failed parsing leaked staging")
			}
		})
	}
}

