//go:build linux

package renderjob

import (
 "context"
 "testing"
 "time"
 "scanner-kiosk/internal/documents"
 "scanner-kiosk/internal/pdfassemble"
 "scanner-kiosk/pkg/ocrplugin"
)

func pdfCandidateFixture(t *testing.T) (*candidateRecords,string,string,pdfassemble.Options,documents.Export) {
 t.Helper()
 r,db,root,job,o:=fixtureWithPDF(t,true)
 p,e:=Prepare(context.Background(),r,root,owner,job,runner,o); if e!=nil {t.Fatal(e)}
 old,e:=p.Publish(context.Background()); if e!=nil {t.Fatal(e)}
 a,e:=r.AdmitRender(context.Background(),owner,documents.RenderAdmission{DocumentID:document,Revision:2,IdempotencyKey:"request_pdfocr_0001",ReOCR:true,ResolvedOutput:ocrplugin.PositionedText,Settings:documents.RenderSettings{Format:"pdf",PDFEncoding:"lossless",OCR:documents.RenderOCR{Enabled:true,ProviderID:"testocr",ModelID:"installed",ModelVersion:"test-version",Languages:[]string{"en"},QualityPreset:"auto"}},ReserveBytes:1<<20},documents.AdmissionLimits{UserMaxBytes:4<<20,GlobalMaxBytes:8<<20,JobMaxBytes:1<<20,MaxPages:50,JobTimeout:time.Minute,ReceiptRetention:72*time.Hour}); if e!=nil {t.Fatal(e)}
 if _,e=r.ClaimExport(context.Background(),owner,a.JobID,runner);e!=nil {t.Fatal(e)}
 o.MaxPagePixels=1000000
 return &candidateRecords{faultRecords:r,provider:&candidateProvider{kind:ocrplugin.PositionedText},db:db},root,a.JobID,o,old
}

func TestReOCRPDFRaster(t *testing.T) {
 r,root,job,o,_:=pdfCandidateFixture(t)
 p,e:=Prepare(context.Background(),r,root,owner,job,runner,o)
 if e!=nil {t.Fatalf("imported PDF recognition raster must prepare: %v",e)}
 defer p.Close()
 if _,e=p.Publish(context.Background());e!=nil {t.Fatal(e)}
 b:=readCandidateBundle(t,r,job)
 if len(b.Pages)!=2 || b.Pages[1].SourceType!="pdf" || b.Pages[1].PixelWidth!=334 || b.Pages[1].PixelHeight!=417 {t.Fatalf("300dpi source mapping: %+v",b.Pages)}
}

func TestReOCRPDFRasterLimits(t *testing.T) {
 r,root,job,o,old:=pdfCandidateFixture(t)
 // A successful real raster establishes that rejection below is a pixel policy,
 // rather than an unavailable executable or an unsupported PDF route.
 p,e:=Prepare(context.Background(),r,root,owner,job,runner,o);if e!=nil {t.Fatalf("real raster baseline: %v",e)}
 if e=p.Close();e!=nil {t.Fatal(e)}
 o.MaxPagePixels=100000
 p,e=Prepare(context.Background(),r,root,owner,job,runner,o)
 if e==nil {p.Close();t.Fatal("over-policy 300dpi page accepted")}
 current,e:=r.GetExport(context.Background(),owner,document);if e!=nil || current.ID!=old.ID {t.Fatal("failed raster replaced old output",e)}
}
