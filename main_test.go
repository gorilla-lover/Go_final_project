package main

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// ==========================================
// 1. 核心演算法測試
// ==========================================
func TestCalculate(t *testing.T) {
	// 定義測試案例結構
	tests := []struct {
		name     string       // 測試案例名稱
		people   []Person     // 輸入：人員
		bills    []Bill       // 輸入：帳單 (假設已經轉換好匯率 AmountBase)
		expected []Settlement // 預期輸出：結算結果
	}{
		{
			name: "Case 1: 簡單三人平分 (A先付錢)",
			people: []Person{
				{ID: 1, Name: "Alice"},
				{ID: 2, Name: "Bob"},
				{ID: 3, Name: "Charlie"},
			},
			bills: []Bill{
				{
					ID:           1,
					Title:        "Lunch",
					Amount:       300,
					AmountBase:   300, // 假設匯率 1:1 或已轉換
					PaidBy:       1,   // Alice 付款
					Participants: []int{1, 2, 3},
				},
			},
			expected: []Settlement{
				{From: "Bob", To: "Alice", Amount: 100},
				{From: "Charlie", To: "Alice", Amount: 100},
			},
		},
		{
			name: "Case 2: 互相抵銷 (A付300, B付150, 三人分)",
			people: []Person{
				{ID: 1, Name: "Alice"},
				{ID: 2, Name: "Bob"},
				{ID: 3, Name: "Charlie"},
			},
			bills: []Bill{
				{ID: 1, AmountBase: 300, PaidBy: 1, Participants: []int{1, 2, 3}},
				{ID: 2, AmountBase: 150, PaidBy: 2, Participants: []int{1, 2, 3}},
			},
			expected: []Settlement{
				{From: "Charlie", To: "Alice", Amount: 150},
			},
		},
		{
			name:     "Case 3: 沒有帳單",
			people:   []Person{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
			bills:    []Bill{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculate(tt.people, tt.bills)

			// 驗證長度
			if len(got) != len(tt.expected) {
				t.Errorf("結算筆數錯誤, got %d, want %d", len(got), len(tt.expected))
				return
			}

			// 驗證內容
			for _, wantItem := range tt.expected {
				found := false
				for _, gotItem := range got {
					if gotItem.From == wantItem.From && gotItem.To == wantItem.To {
						// 檢查金額是否接近 (浮點數比較)
						if diff := gotItem.Amount - wantItem.Amount; diff < 0.01 && diff > -0.01 {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("缺少預期的結算: %+v, 實際結果: %+v", wantItem, got)
				}
			}
		})
	}
}

// ==========================================
// 2. JSON 資料解析測試 (Data Parsing)
// 驗證能否正確解析外部 API 的複雜 JSON 格式
// ==========================================
func TestParseRateResponse(t *testing.T) {
	// 模擬外部 API 回傳的 JSON (TWD 為基準)
	mockJSON := []byte(`{
		"date": "2025-12-06",
		"twd": {
			"usd": 0.03125, 
			"jpy": 4.5
		}
	}`)

	// 測試目標：以 "TWD" 為基準進行解析
	entry, err := parseRateResponse("TWD", mockJSON)

	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}

	// 驗證日期
	if entry.Date != "2025-12-06" {
		t.Errorf("日期解析錯誤, got: %s", entry.Date)
	}

	// 驗證匯率數值 (假設 1 TWD = 0.03125 USD)
	if rate, ok := entry.Rates["usd"]; !ok || rate != 0.03125 {
		t.Errorf("USD 匯率解析錯誤, got: %v", rate)
	}
}

// ==========================================
// 3. 匯率轉換邏輯測試 (Mocking Strategy)
// 透過「注入假資料」來測試幣別換算，不需連網
// ==========================================
func TestConvertBillsToBase_WithMock(t *testing.T) {
	// 1. 準備 Mock (假) 的匯率快取
	mockBase := "twd"
	rateCache[mockBase] = rateEntry{
		Date:      "2025-01-01",
		FetchedAt: time.Now(),
		Rates: map[string]float64{
			"usd": 0.1, // 假設 1 TWD = 0.1 USD (即 1 USD = 10 TWD)
		},
	}

	// 2. 準備測試帳單 (10 USD)
	inputBills := []Bill{
		{ID: 1, Title: "US Snack", Amount: 10, Currency: "USD"},
	}

	// 3. 執行轉換
	converted, _, err := convertBillsToBase(mockBase, inputBills)

	if err != nil {
		t.Fatalf("轉換過程報錯: %v", err)
	}

	// 4. 驗證結果 (10 USD / 0.1 = 100 TWD)
	expectedAmount := 100.0
	if math.Abs(converted[0].AmountBase-expectedAmount) > 0.01 {
		t.Errorf("匯率換算錯誤: 10 USD (Rate 0.1) 應為 %.2f TWD, 但得到 %.2f",
			expectedAmount, converted[0].AmountBase)
	}
}

// ==========================================
// 4. 效能測試
// ==========================================
func BenchmarkCalculate(b *testing.B) {
	// 1. 準備測試資料 (100人, 1000筆帳單)
	peopleCount := 100
	billCount := 1000

	var people []Person
	for i := 1; i <= peopleCount; i++ {
		people = append(people, Person{ID: i, Name: fmt.Sprintf("User%d", i)})
	}

	var bills []Bill
	// 使用固定的種子 (42)，確保每次跑 Benchmark 的數據都是一樣的 (Deterministic)
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < billCount; i++ {
		// 場景 A: 隨機金額
		amt := rng.Float64() * 1000

		// 場景 B: 貧富差距 (只有前 10% 的人會付錢，其他人只吃飯)
		payer := rng.Intn(10) + 1

		// 場景 C: 隨機參與 (每筆帳單約 50% 人參與，而非全部)
		var participants []int
		for p := 1; p <= peopleCount; p++ {
			if rng.Intn(2) == 0 { // 50% 機率
				participants = append(participants, p)
			}
		}
		// 防呆
		if len(participants) == 0 {
			participants = append(participants, payer)
		}

		bills = append(bills, Bill{
			ID:           i,
			Title:        "Bench Bill",
			AmountBase:   amt,
			PaidBy:       payer,
			Participants: participants,
		})
	}

	b.ResetTimer() // 重置計時器，只計算 calculate 的時間
	for i := 0; i < b.N; i++ {
		calculate(people, bills)
	}
}
