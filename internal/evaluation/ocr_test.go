package evaluation

import (
	"testing"
	"math"
	"crypto/sha256"
	"fmt"
	"scanner-kiosk/pkg/ocrplugin"
)

func hashBytes(b []byte) string { return fmt.Sprintf("%x",sha256.Sum256(b)) }

func ocrPageTest() (OCRPageOracle,ocrplugin.PositionedPage) {
	o:=OCRPageOracle{PageID:"page_ocr_test_001",SourceSHA256:hashBytes([]byte("source")),Width:100,Height:100,Spans:[]OCROracleSpan{{Text:"日本",Order:0,Box:[4]float64{10,10,30,10},Cell:"r1c1"},{Text:"123",Order:1,Box:[4]float64{10,30,30,10},Cell:"r2c1"}}}
	p:=ocrplugin.PositionedPage{PageID:o.PageID,Width:100,Height:100,Spans:[]ocrplugin.Span{{Text:"日本",Box:ocrplugin.Box{X:10,Y:10,Width:30,Height:10}},{Text:"123",Box:ocrplugin.Box{X:10,Y:30,Width:30,Height:10}}}}
	return o,p
}
func TestOCRAnalysis(t *testing.T) {
	o,p:=ocrPageTest();q,err:=AnalyzeOCRPage(o,"日本123",p)
	if err!=nil||q.Status!="passed"||q.ReferenceCodePoints!=5||q.TextErrors!=0||q.MatchedCells!=2||q.MatchedRegions!=2 {t.Fatalf("expected independent complete quality, got %+v %v",q,err)}
	p.Spans[0],p.Spans[1]=p.Spans[1],p.Spans[0]
	q,err=AnalyzeOCRPage(o,"123日本",p);if err!=nil||q.ReadingOrderInversions!=1||q.Status!="failed" {t.Fatalf("order inversion not measured: %+v %v",q,err)}
}
func TestOCRMissingOutput(t *testing.T) {
	o,p:=ocrPageTest();if _,err:=AnalyzeOCRPage(o,"",ocrplugin.PositionedPage{});err==nil {t.Fatal("missing recognition accepted")}
	for _,v:=range []float64{math.NaN(),-1,101} {p.Spans[0].Box.X=v;if _,err:=AnalyzeOCRPage(o,"日本123",p);err==nil {t.Fatal("bad source box accepted")}}
	o.Spans=nil;o.Blank=true;p.Spans=nil;p.Width=100;p.Height=100
	q,err:=AnalyzeOCRPage(o,"",p);if err!=nil||q.Status!="passed" {t.Fatalf("explicit blank artifact rejected: %+v %v",q,err)}
}
func TestOCRUnicodeUnits(t *testing.T) {
	o,p:=ocrPageTest();o.Spans=o.Spans[:1];o.Spans[0].Text="が１";p.Spans=p.Spans[:1];p.Spans[0].Text="か\u3099１"
	q,err:=AnalyzeOCRPage(o,"か\u3099１",p);if err!=nil||q.TextErrors!=0||q.ReferenceCodePoints!=2 {t.Fatalf("NFC code points: %+v %v",q,err)}
	q,err=AnalyzeOCRPage(o,"が1",p);if err!=nil||q.TextErrors!=1 {t.Fatalf("fullwidth digit was folded: %+v %v",q,err)}
}
