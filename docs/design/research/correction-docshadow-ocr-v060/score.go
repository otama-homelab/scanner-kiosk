package main

import (
 "encoding/json"
 "fmt"
 "os"
 "scanner-kiosk/internal/evaluation"
 "scanner-kiosk/pkg/ocrplugin"
)

type Item struct {ID string `json:"id"`; Oracle evaluation.OCRPageOracle `json:"oracle"`; Text string `json:"text"`; Positions ocrplugin.PositionedPage `json:"positions"`}
func main(){
 var items []Item
 if err:=json.NewDecoder(os.Stdin).Decode(&items);err!=nil{panic(err)}
 results:=make([]map[string]any,0,len(items))
 for _, item:=range items {
  q,err:=evaluation.AnalyzeOCRPage(item.Oracle,item.Text,item.Positions)
  if err!=nil {panic(fmt.Errorf("%s: %w",item.ID,err))}
  results=append(results,map[string]any{"id":item.ID,"quality":q})
 }
 if err:=json.NewEncoder(os.Stdout).Encode(results);err!=nil{panic(err)}
}
