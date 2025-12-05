package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	webview "github.com/webview/webview_go"
)

//go:embed index.html
var indexHTML string

// ================= 資料結構 =================

type Person struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Bill struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Amount       float64 `json:"amount"`
	Category     string  `json:"category,omitempty"`
	Currency     string  `json:"currency,omitempty"`
	AmountBase   float64 `json:"amountBase,omitempty"`
	PaidBy       int     `json:"paidBy"`
	Participants []int   `json:"participants"`
}

type Settlement struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
}

// GlobalState 用於儲存伺服器端的共享狀態
type GlobalState struct {
	People       []Person `json:"people"`
	Bills        []Bill   `json:"bills"`
	BaseCurrency string   `json:"baseCurrency"`
	LastUpdated  int64    `json:"lastUpdated"` // 用於版本控制 (Timestamp)
}

// CalculateRequest 計算請求
type CalculateRequest struct {
	BaseCurrency string   `json:"baseCurrency,omitempty"`
	People       []Person `json:"people"`
	Bills        []Bill   `json:"bills"`
}

// CalculateResponse 計算結果
type CalculateResponse struct {
	Settlements  []Settlement `json:"settlements"`
	Bills        []Bill       `json:"bills,omitempty"`
	BaseCurrency string       `json:"baseCurrency,omitempty"`
	RateDate     string       `json:"rateDate,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type rateEntry struct {
	Rates     map[string]float64
	Date      string
	FetchedAt time.Time
}

// ================= 全域變數 =================

var (
	// 專案共享狀態 (加上 Mutex 鎖以確保執行緒安全)
	projectState = GlobalState{
		People:       []Person{},
		Bills:        []Bill{},
		BaseCurrency: "TWD",
		LastUpdated:  time.Now().UnixMilli(),
	}
	stateMutex sync.Mutex

	// 匯率快取
	rateCache       = make(map[string]rateEntry)
	rateCacheTTL    = 30 * time.Minute
	exchangeAPIBase = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json"
	defaultBase     = "TWD"
)

// ================= 主程式 =================

func main() {
	serverMode := flag.Bool("server", false, "啟動 HTTP 伺服器模式")
	port := flag.String("port", "8080", "HTTP 伺服器連接埠")
	flag.Parse()

	if *serverMode {
		runServer(*port)
	} else {
		runDesktop()
	}
}

func runDesktop() {
	runtime.LockOSThread()
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("分帳器 - Bill Splitter")
	w.SetSize(900, 700, webview.HintNone)

	// 桌面版也可以綁定同步函數，讓邏輯統一
	w.Bind("calculateSplit", processCalculate)
	
	// 桌面版目前維持原本行為，若要支援同步需改用 API 呼叫模式
	// 這裡為了相容性，傳入 HTML
	dataURI := "data:text/html;charset=utf-8," + url.PathEscape(indexHTML)
	w.Navigate(dataURI)
	w.Run()
}

func runServer(port string) {
	// 1. 首頁
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})

	// 2. 計算 API
	http.HandleFunc("/api/calculate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		resultJSON := processCalculate(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resultJSON))
	})

	// 3. 同步 API (核心修改：讀取與寫入共享狀態)
	http.HandleFunc("/api/sync", handleSync)

	// 顯示連線資訊
	ip := getLocalIP()
	fmt.Println("========================================")
	fmt.Printf("分帳器伺服器已啟動 (同步模式)！\n")
	fmt.Printf("電腦本機請開： http://localhost:%s\n", port)
	if ip != "" {
		fmt.Printf("手機請連線至： http://%s:%s\n", ip, port)
	}
	fmt.Println("現在所有連線裝置將會看到相同的帳單資料。")
	fmt.Println("========================================")

	http.ListenAndServe(":"+port, nil)
}

// handleSync 處理狀態同步
func handleSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stateMutex.Lock()
	defer stateMutex.Unlock()

	// 若是 POST，代表前端要更新資料
	if r.Method == http.MethodPost {
		var newState GlobalState
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &newState); err == nil {
			// 更新伺服器狀態
			projectState = newState
			projectState.LastUpdated = time.Now().UnixMilli()
		}
	}

	// 無論 GET 或 POST，最後都回傳最新的伺服器狀態
	json.NewEncoder(w).Encode(projectState)
}

// processCalculate 保持不變，負責運算
func processCalculate(requestJSON string) string {
	var req CalculateRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		response := CalculateResponse{Error: "解析資料錯誤"}
		result, _ := json.Marshal(response)
		return string(result)
	}

	base := strings.ToUpper(strings.TrimSpace(req.BaseCurrency))
	if base == "" {
		base = defaultBase
	}

	convertedBills, rateDate, err := convertBillsToBase(base, req.Bills)
	if err != nil {
		response := CalculateResponse{Error: err.Error(), BaseCurrency: base, RateDate: rateDate}
		result, _ := json.Marshal(response)
		return string(result)
	}

	settlements := calculate(req.People, convertedBills)

	response := CalculateResponse{
		Settlements:  settlements,
		Bills:        convertedBills,
		BaseCurrency: base,
		RateDate:     rateDate,
	}
	result, _ := json.Marshal(response)
	return string(result)
}

// 其餘輔助函式 (IP, Rate, Parse) 保持不變
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil { return "" }
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil { return ipnet.IP.String() }
		}
	}
	return ""
}

func convertBillsToBase(base string, bills []Bill) ([]Bill, string, error) {
	baseLower := strings.ToLower(base)
	rates, err := getRates(baseLower)
	if err != nil { return nil, rates.Date, err }

	var converted []Bill
	for _, bill := range bills {
		cur := strings.ToLower(strings.TrimSpace(bill.Currency))
		if cur == "" { cur = baseLower }
		var amountBase float64
		if cur == baseLower {
			amountBase = bill.Amount
		} else {
			rate, ok := rates.Rates[cur]
			if !ok || rate == 0 {
				return nil, rates.Date, fmt.Errorf("缺少幣別 %s", strings.ToUpper(cur))
			}
			amountBase = bill.Amount / rate
		}
		bill.AmountBase = amountBase
		converted = append(converted, bill)
	}
	return converted, rates.Date, nil
}

func getRates(base string) (rateEntry, error) {
	now := time.Now()
	if entry, ok := rateCache[base]; ok {
		if now.Sub(entry.FetchedAt) < rateCacheTTL { return entry, nil }
	}
	entry, err := fetchRates(base)
	if err != nil {
		if cached, ok := rateCache[base]; ok { return cached, nil }
		return rateEntry{}, err
	}
	rateCache[base] = entry
	return entry, nil
}

func fetchRates(base string) (rateEntry, error) {
	var lastErr error
	for i := 0; i < 2; i++ {
		entry, err := fetchRatesOnce(base)
		if err == nil { return entry, nil }
		lastErr = err
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	return rateEntry{}, lastErr
}

func fetchRatesOnce(base string) (rateEntry, error) {
	url := fmt.Sprintf(exchangeAPIBase, base)
	resp, err := http.Get(url)
	if err != nil { return rateEntry{}, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return rateEntry{}, fmt.Errorf("HTTP %d", resp.StatusCode) }
	body, err := io.ReadAll(resp.Body)
	if err != nil { return rateEntry{}, err }
	return parseRateResponse(base, body)
}

func parseRateResponse(base string, data []byte) (rateEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil { return rateEntry{}, err }
	var date string
	if v, ok := raw["date"]; ok { json.Unmarshal(v, &date) }
	baseKey := strings.ToLower(base)
	rateRaw, ok := raw[baseKey]
	if !ok { return rateEntry{}, errors.New("無匯率資料") }
	rates := make(map[string]float64)
	if err := json.Unmarshal(rateRaw, &rates); err != nil { return rateEntry{}, err }
	rates[baseKey] = 1
	return rateEntry{Rates: rates, Date: date}, nil
}

func calculate(people []Person, bills []Bill) []Settlement {
	balance := make(map[int]float64)
	nameMap := make(map[int]string)
	for _, p := range people {
		balance[p.ID] = 0
		nameMap[p.ID] = p.Name
	}
	for _, bill := range bills {
		if len(bill.Participants) == 0 { continue }
		amt := bill.AmountBase
		if amt == 0 { amt = bill.Amount }
		perPerson := amt / float64(len(bill.Participants))
		balance[bill.PaidBy] += amt
		for _, pid := range bill.Participants { balance[pid] -= perPerson }
	}
	var creditors, debtors []struct{ id int; amount float64 }
	for id, amt := range balance {
		if amt > 0.01 { creditors = append(creditors, struct{ id int; amount float64 }{id, amt}) }
		if amt < -0.01 { debtors = append(debtors, struct{ id int; amount float64 }{id, -amt}) }
	}
	var settlements []Settlement
	i, j := 0, 0
	for i < len(creditors) && j < len(debtors) {
		cred := creditors[i]
		debt := debtors[j]
		amt := cred.amount
		if debt.amount < amt { amt = debt.amount }
		settlements = append(settlements, Settlement{From: nameMap[debt.id], To: nameMap[cred.id], Amount: amt})
		creditors[i].amount -= amt
		debtors[j].amount -= amt
		if creditors[i].amount < 0.01 { i++ }
		if debtors[j].amount < 0.01 { j++ }
	}
	return settlements
}