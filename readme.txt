需要安裝 webview，在終端機使用 go get github.com/webview/webview_go
然後應該就直接 go run main.go 就可以了

如果 run 之後出現在終端機出現一大串錯誤，並且錯誤中包含 mingw，有可能是安裝到的 GO 版本是 32 位元的
可以重新安裝 64 位元的版本，並用 go version 指令檢查當前版本，386 是 32 位元版本，amd64 是 64 位元版本
如果安裝 64 位元後使用 go version 後出現的版本仍是 32 位元，應該是因為 32 位元版本的路徑沒有從環境變數刪掉
64 位元的會放在 Program Files，32 位元的會放在 Program Files (x86)，把系統環境變數 Path 裡面 Program Files (x86) 下面的那個 GO 刪掉應該就可以了
