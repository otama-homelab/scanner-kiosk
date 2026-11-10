package main
import("os";"encoding/json";"crypto/sha256";"fmt";"scanner-kiosk/internal/evaluation")
func main(){if len(os.Args)!=4{panic("catalog id output required")};_,b,e:=evaluation.ResolveFixture(os.Args[1],"","",os.Args[2]);if e!=nil{panic(e)};if e=os.WriteFile(os.Args[3],b,0600);e!=nil{panic(e)};if e=json.NewEncoder(os.Stdout).Encode(map[string]any{"id":os.Args[2],"sha256":fmt.Sprintf("%x",sha256.Sum256(b)),"bytes":len(b)});e!=nil{panic(e)}}
