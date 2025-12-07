package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
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

type GlobalState struct {
	People       []Person `json:"people"`
	Bills        []Bill   `json:"bills"`
	BaseCurrency string   `json:"baseCurrency"`
	LastUpdated  int64    `json:"lastUpdated"`
}

type CalculateRequest struct {
	BaseCurrency string   `json:"baseCurrency,omitempty"`
	People       []Person `json:"people"`
	Bills        []Bill   `json:"bills"`
}

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

// ================= 全域變數（保留原有功能） =================

var (
	projectState = GlobalState{
		People:       []Person{},
		Bills:        []Bill{},
		BaseCurrency: "TWD",
		LastUpdated:  time.Now().UnixMilli(),
	}
	stateMutex sync.Mutex

	// exchange API template (unchanged)
	exchangeAPIBase = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json"
	defaultBase     = "TWD"
	rateCacheTTL    = 30 * time.Minute
)

// ================= RateFetcher interface & HTTP implementation =================

// RateFetcher 抽象化外部匯率來源
type RateFetcher interface {
	Fetch(base string) (rateEntry, error)
}

type HTTPRateFetcher struct {
	baseURL string
	client  *http.Client
}

func NewHTTPRateFetcher(baseURL string) *HTTPRateFetcher {
	return &HTTPRateFetcher{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (h *HTTPRateFetcher) Fetch(base string) (rateEntry, error) {
	urlStr := fmt.Sprintf(h.baseURL, base)
	resp, err := h.client.Get(urlStr)
	if err != nil {
		return rateEntry{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rateEntry{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rateEntry{}, err
	}

	return parseRateResponse(base, body)
}

// ================= RateCache (thread-safe) =================

type RateCache struct {
	mu    sync.RWMutex
	cache map[string]rateEntry
}

func NewRateCache() *RateCache {
	return &RateCache{
		cache: make(map[string]rateEntry),
	}
}

func (rc *RateCache) Get(base string) (rateEntry, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	e, ok := rc.cache[base]
	return e, ok
}

func (rc *RateCache) Set(base string, e rateEntry) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cache[base] = e
}

var rateCache = NewRateCache()

// 供測試或特殊情境外部替換 fetcher
var rateFetcher RateFetcher = NewHTTPRateFetcher(exchangeAPIBase)

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

	// 綁定計算函數（desktop 版本仍保持原有行為）
	// webview 的 Bind 需要函數型態符合條件；我們保持 processCalculate 的簽名
	w.Bind("calculateSplit", processCalculate)

	dataURI := "data:text/html;charset=utf-8," + url.PathEscape(indexHTML)
	w.Navigate(dataURI)
	w.Run()
}

func runServer(port string) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(indexHTML)); err != nil {
			log.Printf("write index failed: %v", err)
		}
	})

	http.HandleFunc("/api/calculate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		resultJSON := processCalculate(string(body))

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(resultJSON)); err != nil {
			log.Printf("/api/calculate write failed: %v", err)
		}
	})

	http.HandleFunc("/api/sync", handleSync)

	ip := getLocalIP()
	fmt.Println("========================================")
	fmt.Printf("分帳器伺服器已啟動 (同步模式)！\n")
	fmt.Printf("電腦本機請開： http://localhost:%s\n", port)
	if ip != "" {
		fmt.Printf("手機請連線至： http://%s:%s\n", ip, port)
	} else {
		fmt.Println("警告：無法偵測到可用的實體網路介面")
	}
	fmt.Println("現在所有連線裝置將會看到相同的帳單資料。")
	fmt.Println("========================================")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server exit: %v", err)
	}
}

// handleSync 處理狀態同步（保留行為，但修正錯誤處理）
func handleSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stateMutex.Lock()
	defer stateMutex.Unlock()

	if r.Method == http.MethodPost {
		var newState GlobalState
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if err := json.Unmarshal(body, &newState); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		projectState = newState
		projectState.LastUpdated = time.Now().UnixMilli()
	}

	enc := json.NewEncoder(w)
	if err := enc.Encode(projectState); err != nil {
		log.Printf("encode projectState failed: %v", err)
	}
}

// processCalculate：保持外部介面不變，但內部更嚴謹處理錯誤
func processCalculate(requestJSON string) string {
	var req CalculateRequest
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		response := CalculateResponse{Error: "解析資料錯誤"}
		if result, err := json.Marshal(response); err == nil {
			return string(result)
		}
		// 若真的 marshal 也失敗，回傳簡單字串
		return `{"error":"解析資料錯誤"}`
	}

	base := strings.ToUpper(strings.TrimSpace(req.BaseCurrency))
	if base == "" {
		base = defaultBase
	}

	convertedBills, rateDate, err := convertBillsToBase(base, req.Bills)
	if err != nil {
		response := CalculateResponse{Error: err.Error(), BaseCurrency: base, RateDate: rateDate}
		if result, err := json.Marshal(response); err == nil {
			return string(result)
		}
		return `{"error":"internal"}` // fallback
	}

	settlements := calculate(req.People, convertedBills)

	response := CalculateResponse{
		Settlements:  settlements,
		Bills:        convertedBills,
		BaseCurrency: base,
		RateDate:     rateDate,
	}
	if result, err := json.Marshal(response); err == nil {
		return string(result)
	}
	return `{"error":"internal"}` // fallback
}

// getLocalIP 與原版行為相同（僅作小格式整理）
func getLocalIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	virtualPrefixes := []string{
		"docker", "veth", "br-", "virbr", "vmnet", "vbox",
		"utun", "tap", "tun", "ppp", "awdl", "llw", "bridge",
		"vEthernet", "hyper-v", "virtualbox", "vmware",
	}

	var candidates []string

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		name := strings.ToLower(iface.Name)
		isVirtual := false
		for _, prefix := range virtualPrefixes {
			if strings.HasPrefix(name, strings.ToLower(prefix)) {
				isVirtual = true
				break
			}
		}
		if isVirtual {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ipv4 := ipNet.IP.To4()
			if ipv4 != nil && !ipNet.IP.IsLoopback() {
				s := ipv4.String()
				if strings.HasPrefix(s, "192.168.") || strings.HasPrefix(s, "10.") {
					candidates = append([]string{s}, candidates...)
				} else {
					candidates = append(candidates, s)
				}
			}
		}
	}

	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// ================= 匯率轉換與 fetch（改用 RateCache 與 RateFetcher） =================

func convertBillsToBase(base string, bills []Bill) ([]Bill, string, error) {
	baseLower := strings.ToLower(base)
	entry, ok := rateCache.Get(baseLower)
	now := time.Now()

	if ok {
		if now.Sub(entry.FetchedAt) < rateCacheTTL {
			// fresh cache
		} else {
			// stale -> attempt refresh asynchronously (best-effort)
			// but keep using stale until we get fresh
			if fetched, err := fetchRates(baseLower); err == nil {
				entry = fetched
				rateCache.Set(baseLower, fetched)
			}
		}
	} else {
		// no cache -> fetch synchronously
		fetched, err := fetchRates(baseLower)
		if err != nil {
			// if nothing cached, surface error
			return nil, "", err
		}
		entry = fetched
		rateCache.Set(baseLower, fetched)
	}

	rates := entry

	var converted []Bill
	for _, bill := range bills {
		cur := strings.ToLower(strings.TrimSpace(bill.Currency))
		if cur == "" {
			cur = baseLower
		}
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
	// legacy helper kept for compatibility (calls the unified path)
	if e, ok := rateCache.Get(base); ok {
		if time.Since(e.FetchedAt) < rateCacheTTL {
			return e, nil
		}
	}
	e, err := fetchRates(base)
	if err != nil {
		if cached, ok := rateCache.Get(base); ok {
			return cached, nil
		}
		return rateEntry{}, err
	}
	rateCache.Set(base, e)
	return e, nil
}

func fetchRates(base string) (rateEntry, error) {
	var lastErr error
	for i := 0; i < 2; i++ {
		entry, err := rateFetcher.Fetch(base)
		if err == nil {
			// ensure FetchedAt is set
			entry.FetchedAt = time.Now()
			rateCache.Set(base, entry)
			return entry, nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	return rateEntry{}, lastErr
}

func parseRateResponse(base string, data []byte) (rateEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return rateEntry{}, err
	}
	var date string
	if v, ok := raw["date"]; ok {
		_ = json.Unmarshal(v, &date)
	}
	baseKey := strings.ToLower(base)
	rateRaw, ok := raw[baseKey]
	if !ok {
		return rateEntry{}, errors.New("無匯率資料")
	}
	rates := make(map[string]float64)
	if err := json.Unmarshal(rateRaw, &rates); err != nil {
		return rateEntry{}, err
	}
	rates[baseKey] = 1
	return rateEntry{Rates: rates, Date: date, FetchedAt: time.Now()}, nil
}

// ================= 核心結算演算法（保留原邏輯） =================

func calculate(people []Person, bills []Bill) []Settlement {
	balance := make(map[int]float64)
	nameMap := make(map[int]string)
	for _, p := range people {
		balance[p.ID] = 0
		nameMap[p.ID] = p.Name
	}
	for _, bill := range bills {
		if len(bill.Participants) == 0 {
			continue
		}
		amt := bill.AmountBase
		if amt == 0 {
			amt = bill.Amount
		}
		perPerson := amt / float64(len(bill.Participants))
		balance[bill.PaidBy] += amt
		for _, pid := range bill.Participants {
			balance[pid] -= perPerson
		}
	}
	var creditors, debtors []struct {
		id     int
		amount float64
	}
	for id, amt := range balance {
		if amt > 0.01 {
			creditors = append(creditors, struct {
				id     int
				amount float64
			}{id, amt})
		}
		if amt < -0.01 {
			debtors = append(debtors, struct {
				id     int
				amount float64
			}{id, -amt})
		}
	}
	var settlements []Settlement
	i, j := 0, 0
	for i < len(creditors) && j < len(debtors) {
		cred := creditors[i]
		debt := debtors[j]
		amt := cred.amount
		if debt.amount < amt {
			amt = debt.amount
		}
		settlements = append(settlements, Settlement{From: nameMap[debt.id], To: nameMap[cred.id], Amount: amt})
		creditors[i].amount -= amt
		debtors[j].amount -= amt
		if creditors[i].amount < 0.01 {
			i++
		}
		if debtors[j].amount < 0.01 {
			j++
		}
	}
	return settlements
}
