package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	webview "github.com/webview/webview_go"
)

//go:embed index.html
var indexHTML string

// Person 代表一個人
type Person struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Bill 代表一筆帳單
type Bill struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Amount       float64 `json:"amount"`
	Category     string  `json:"category,omitempty"` // 帳單分類（交通、飲食、住宿等）
	Currency     string  `json:"currency,omitempty"` // 帳單幣別（例如 TWD, USD）
	AmountBase   float64 `json:"amountBase,omitempty"`
	PaidBy       int     `json:"paidBy"`       // 誰先付的（Person ID）
	Participants []int   `json:"participants"` // 參與者的 ID 列表
}

// Settlement 代表結算結果
type Settlement struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
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
	Bills        []Bill       `json:"bills,omitempty"` // 換算後的帳單
	BaseCurrency string       `json:"baseCurrency,omitempty"`
	RateDate     string       `json:"rateDate,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type rateEntry struct {
	Rates     map[string]float64
	Date      string
	FetchedAt time.Time
}

var (
	rateCache       = make(map[string]rateEntry) // key: lower-case base
	rateCacheTTL    = 30 * time.Minute
	exchangeAPIBase = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json"
	defaultBase     = "TWD"
)

func main() {
	runtime.LockOSThread()

	w := webview.New(true)
	defer w.Destroy()

	w.SetTitle("分帳器 - Bill Splitter")
	w.SetSize(900, 700, webview.HintNone)

	// 綁定計算分帳的函式
	w.Bind("calculateSplit", func(requestJSON string) string {
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

		// 執行分帳計算
		settlements := calculate(req.People, convertedBills)

		response := CalculateResponse{
			Settlements:  settlements,
			Bills:        convertedBills,
			BaseCurrency: base,
			RateDate:     rateDate,
		}
		result, _ := json.Marshal(response)
		return string(result)
	})

	dataURI := "data:text/html;charset=utf-8," + url.PathEscape(indexHTML)
	w.Navigate(dataURI)

	w.Run()
}

// convertBillsToBase 將各筆帳單換算為基準幣別
func convertBillsToBase(base string, bills []Bill) ([]Bill, string, error) {
	baseLower := strings.ToLower(base)
	rates, err := getRates(baseLower)
	if err != nil {
		return nil, rates.Date, err
	}

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
				return nil, rates.Date, fmt.Errorf("缺少幣別 %s 的匯率，無法換算為 %s", strings.ToUpper(cur), strings.ToUpper(base))
			}
			amountBase = bill.Amount / rate
		}

		bill.AmountBase = amountBase
		converted = append(converted, bill)
	}

	return converted, rates.Date, nil
}

// getRates 取得以 base 為基準的匯率，帶有快取與過期 fallback
func getRates(base string) (rateEntry, error) {
	now := time.Now()
	if entry, ok := rateCache[base]; ok {
		if now.Sub(entry.FetchedAt) < rateCacheTTL {
			return entry, nil
		}
	}

	entry, err := fetchRates(base)
	if err != nil {
		if cached, ok := rateCache[base]; ok {
			// 使用舊資料，但回傳日期提示可能過期
			return cached, nil
		}
		return rateEntry{}, err
	}

	rateCache[base] = entry
	return entry, nil
}

// fetchRates 從遠端抓取匯率，最多重試 2 次
func fetchRates(base string) (rateEntry, error) {
	var lastErr error
	for i := 0; i < 2; i++ {
		entry, err := fetchRatesOnce(base)
		if err == nil {
			return entry, nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	return rateEntry{}, lastErr
}

func fetchRatesOnce(base string) (rateEntry, error) {
	url := fmt.Sprintf(exchangeAPIBase, base)
	resp, err := http.Get(url)
	if err != nil {
		return rateEntry{}, fmt.Errorf("匯率讀取失敗：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rateEntry{}, fmt.Errorf("匯率讀取失敗：HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rateEntry{}, fmt.Errorf("匯率讀取失敗：%w", err)
	}

	entry, err := parseRateResponse(base, body)
	if err != nil {
		return rateEntry{}, fmt.Errorf("匯率解析失敗：%w", err)
	}

	entry.FetchedAt = time.Now()
	return entry, nil
}

func parseRateResponse(base string, data []byte) (rateEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return rateEntry{}, err
	}

	var date string
	if v, ok := raw["date"]; ok {
		if err := json.Unmarshal(v, &date); err != nil {
			return rateEntry{}, err
		}
	}

	baseKey := strings.ToLower(base)
	rateRaw, ok := raw[baseKey]
	if !ok {
		return rateEntry{}, errors.New("找不到基準幣別的匯率資料")
	}

	rates := make(map[string]float64)
	if err := json.Unmarshal(rateRaw, &rates); err != nil {
		return rateEntry{}, err
	}

	// 確保基準幣別本身的匯率為 1
	rates[baseKey] = 1
	return rateEntry{
		Rates: rates,
		Date:  date,
	}, nil
}

// calculate 計算分帳邏輯
func calculate(people []Person, bills []Bill) []Settlement {
	// 建立每個人的餘額映射
	balance := make(map[int]float64)
	nameMap := make(map[int]string)

	// 初始化
	for _, person := range people {
		balance[person.ID] = 0
		nameMap[person.ID] = person.Name
	}

	// 計算每個人的應付金額
	for _, bill := range bills {
		if len(bill.Participants) == 0 {
			continue
		}

		amount := bill.AmountBase
		if amount == 0 {
			amount = bill.Amount
		}

		// 每人應付的金額
		perPerson := amount / float64(len(bill.Participants))

		// 付款者增加餘額（他先付了這筆錢）
		balance[bill.PaidBy] += amount

		// 每個參與者減少餘額（欠這筆錢）
		for _, participantID := range bill.Participants {
			balance[participantID] -= perPerson
		}
	}

	// 分離債權人和債務人
	var creditors []struct {
		id     int
		amount float64
	}
	var debtors []struct {
		id     int
		amount float64
	}

	for id, amt := range balance {
		if amt > 0.01 { // 債權人（別人欠他錢）
			creditors = append(creditors, struct {
				id     int
				amount float64
			}{id, amt})
		} else if amt < -0.01 { // 債務人（他欠別人錢）
			debtors = append(debtors, struct {
				id     int
				amount float64
			}{id, -amt})
		}
	}

	// 使用貪心算法配對債權人和債務人
	var settlements []Settlement
	i, j := 0, 0

	for i < len(creditors) && j < len(debtors) {
		creditor := creditors[i]
		debtor := debtors[j]

		// 取較小的金額
		amount := creditor.amount
		if debtor.amount < amount {
			amount = debtor.amount
		}

		// 添加結算記錄
		settlements = append(settlements, Settlement{
			From:   nameMap[debtor.id],
			To:     nameMap[creditor.id],
			Amount: amount,
		})

		// 更新餘額
		creditors[i].amount -= amount
		debtors[j].amount -= amount

		// 如果債權人收完了，移到下一個
		if creditors[i].amount < 0.01 {
			i++
		}
		// 如果債務人付完了，移到下一個
		if debtors[j].amount < 0.01 {
			j++
		}
	}

	return settlements
}
