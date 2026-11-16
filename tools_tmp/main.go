package main
import (
  "context"
  "fmt"
  _ "pet-grooming/internal/boot"
  "github.com/gogf/gf/v2/frame/g"
)
func main() {
  ctx := context.Background()
  keys, _ := g.Redis().Do(ctx, "KEYS", "slot:*")
  fmt.Println(keys)
  ks := keys.Strings()
  for _, k := range ks {
    v, _ := g.Redis().Do(ctx, "TYPE", k)
    if v.String() == "set" {
      m, _ := g.Redis().Do(ctx, "SMEMBERS", k)
      cnt, _ := g.Redis().Do(ctx, "SCARD", k)
      fmt.Println(k, "scard=", cnt, m.Strings())
    } else {
      v2, _ := g.Redis().Do(ctx, "GET", k)
      fmt.Println(k, "get=", v2)
    }
  }
}
