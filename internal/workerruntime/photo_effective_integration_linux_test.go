//go:build linux

package workerruntime

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/json"
 "image"
 "image/color"
 "image/jpeg"
 "os"
 "path/filepath"
 "testing"
 "time"

 "scanner-kiosk/internal/documents"
 "scanner-kiosk/internal/importupload"
)

func photoJPEG(t *testing.T) []byte {
 t.Helper()
 im := image.NewNRGBA(image.Rect(0,0,80,48))
 colors := []color.NRGBA{{240,20,20,255},{20,230,20,255},{20,20,240,255},{240,220,20,255}}
 for y:=0;y<48;y++ { for x:=0;x<80;x++ { im.SetNRGBA(x,y,colors[(y/24)*2+x/40]) } }
 var b bytes.Buffer
 if err:=jpeg.Encode(&b,im,&jpeg.Options{Quality:100});err!=nil {t.Fatal(err)}
 // Little-endian EXIF orientation 6, plus no private fixture data.
 exif:=[]byte{'E','x','i','f',0,0,'I','I',42,0,8,0,0,0,1,0,0x12,1,3,0,1,0,0,0,6,0,0,0,0,0,0,0}
 out:=append([]byte{0xff,0xd8,0xff,0xe1,0,byte(len(exif)+2)},exif...)
 return append(out,b.Bytes()[2:]...)
}
func photoLimits() documents.ImportLimits { return documents.ImportLimits{AdmissionLimits:documents.AdmissionLimits{UserMaxBytes:16<<20,GlobalMaxBytes:32<<20,JobMaxBytes:4<<20,MaxPages:50,JobTimeout:time.Minute,ReceiptRetention:72*time.Hour},MaxDocuments:20,DocumentLifetime:time.Hour} }
func photoRequest(t *testing.T, raw []byte) documents.ImportRequest {
 t.Helper()
 request:=documents.ImportRequest{IdempotencyKey:"request_photo_trace001",InputBytes:int64(len(raw)),SHA256:sha256.Sum256(raw)}
 if err:=json.Unmarshal([]byte(`{"media_type":"image/jpeg"}`),&request);err!=nil {t.Fatal(err)}
 return request
}
func TestPhotoIntakeTracer(t *testing.T) {
 c,store,db,root:=fixture(t)
 raw:=photoJPEG(t)
 upload,err:=importupload.New(root,store,photoLimits());if err!=nil {t.Fatal(err)};defer upload.Close()
 request:=photoRequest(t,raw)
 a,err:=upload.Receive(context.Background(),owner,request,bytes.NewReader(raw));if err!=nil {t.Fatal(err)}
 var media,path string
 if err=db.QueryRow("SELECT a.media_type,a.relative_path FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id WHERE i.job_id=?",a.JobID).Scan(&media,&path);err!=nil {t.Fatal(err)}
 if media!="image/jpeg" {t.Fatalf("photo upload must retain canonical JPEG media, got %s",media)}
 if err=Run(context.Background(),c,true);err!=nil {t.Fatal(err)}
 p,err:=store.ReadDocument(context.Background(),owner,a.DocumentID);if err!=nil {t.Fatal(err)}
 if p.Purpose!="document" || len(p.Pages)!=1 || p.Pages[0].SourceType!="photo" || p.Pages[0].Capture!=nil || p.Pages[0].PDF!=nil {t.Fatalf("not a document photo: %+v",p)}
 saved,err:=os.ReadFile(filepath.Join(root,path));if err!=nil||!bytes.Equal(saved,raw) {t.Fatal("immutable upload changed",err)}
 replay,err:=upload.Receive(context.Background(),owner,request,bytes.NewReader(nil));if err!=nil||!replay.Replay||replay.JobID!=a.JobID {t.Fatal("duplicate admission",replay,err)}
 var count int
 if err=db.QueryRow("SELECT count(*) FROM photo_sources WHERE document_id=?",a.DocumentID).Scan(&count);err!=nil||count!=1 {t.Fatal("missing normalized provenance",count,err)}
}
