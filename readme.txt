需要安裝 webview，在終端機使用 go get github.com/webview/webview_go
然後應該就直接 go run main.go 就可以了

如果 run 之後出現在終端機出現一大串錯誤，並且錯誤中包含 mingw，有可能是安裝到的 GO 版本是 32 位元的
可以重新安裝 64 位元的版本，並用 go version 指令檢查當前版本，386 是 32 位元版本，amd64 是 64 位元版本
如果安裝 64 位元後使用 go version 後出現的版本仍是 32 位元，應該是因為 32 位元版本的路徑沒有從環境變數刪掉
64 位元的會放在 Program Files，32 位元的會放在 Program Files (x86)，把系統環境變數 Path 裡面 Program Files (x86) 下面的那個 GO 刪掉應該就可以了

1. 只在本地端使用webviewer：go run main.go
2. 啟動伺服器（同個wifi下可同步使用，手機也可操作）：go run main.go -server
使用方法：
========================================
分帳器伺服器已啟動！
電腦本機請開： http://localhost:8080
手機請連線至： http://192.168.1.105:8080
(請確保手機與電腦連線到同一個 Wi-Fi)
按 Ctrl+C 結束程式
========================================


------------main_test.go------------
1. TestCalculate(): 測試核心函數Calculate()進行分帳結算是否正確
2. TestParseRateResponse()：測試函數ParseRateResponse()能否正確解析匯率API回傳的JSON資料
3. TestConvertBillsToBase_WithMock()：測試函數ConvertBillsToBase()是否能正確轉換匯率
4. BenchmarkCalculate()：測時函數Calculate()在高資料量（100位使用者和1000筆帳單）下的表現

如何使用：
1. 2. 3. -> 終端機輸入：$go test -v
            結果判斷：若正確顯示「PASS」
4. -> 終端機輸入：$ go test -bench=. -benchmem
      結果判斷：ns/op (每次操作奈秒數) 越低越好

------------go.yml------------
GitHub 的自動化工作流程 (Workflow)：
每次push到GitHub時，會自動執行main_test.go測試