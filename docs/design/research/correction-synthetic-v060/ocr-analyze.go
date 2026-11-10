package main
import("encoding/json";"os";"scanner-kiosk/internal/evaluation";"scanner-kiosk/pkg/ocrplugin")
type Input struct{Oracle evaluation.OCRPageOracle `json:"oracle"`;Text string `json:"text"`}
func main(){var in []Input;if e:=json.NewDecoder(os.Stdin).Decode(&in);e!=nil{panic(e)};out:=[]evaluation.OCRPageQuality{};for _,v:=range in{q,e:=evaluation.AnalyzeOCRPage(v.Oracle,v.Text,ocrplugin.PositionedPage{PageID:v.Oracle.PageID,Width:v.Oracle.Width,Height:v.Oracle.Height});if e!=nil{panic(e)};out=append(out,q)};if e:=json.NewEncoder(os.Stdout).Encode(out);e!=nil{panic(e)}}
