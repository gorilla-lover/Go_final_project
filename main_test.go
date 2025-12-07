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
	tests := []struct {
		name     string
		people   []Person
		bills    []Bill
		expected []Settlement
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
					AmountBase:   300,
					PaidBy:       1,
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

			if len(got) != len(tt.expected) {
				t.Errorf("結算筆數錯誤, got %d, want %d", len(got), len(tt.expected))
				return
			}

			for _, wantItem := range tt.expected {
				found := false
				for _, gotItem := range got {
					if gotItem.From == wantItem.From && gotItem.To == wantItem.To {
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
// ==========================================
func TestParseRateResponse(t *testing.T) {
	mockJSON := []byte(`{
		"date": "2025-12-06",
		"twd": {
			"usd": 0.03125,
			"jpy": 4.5
		}
	}`)

	entry, err := parseRateResponse("TWD", mockJSON)
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}

	if entry.Date != "2025-12-06" {
		t.Errorf("日期解析錯誤, got: %s", entry.Date)
	}

	if rate, ok := entry.Rates["usd"]; !ok || rate != 0.03125 {
		t.Errorf("USD 匯率解析錯誤, got: %v", rate)
	}
}

// ==========================================
// 3. 匯率轉換邏輯測試 (Mocking Strategy)
// 透過注入假資料來測試幣別換算，不需連網
// ==========================================
func TestConvertBillsToBase_WithMock(t *testing.T) {
	mockBase := "twd"
	rateCache.Set(mockBase, rateEntry{
		Date:      "2025-01-01",
		FetchedAt: time.Now(),
		Rates: map[string]float64{
			"usd": 0.1, // 假設 1 TWD = 0.1 USD (即 1 USD = 10 TWD)
		},
	})

	inputBills := []Bill{
		{ID: 1, Title: "US Snack", Amount: 10, Currency: "USD"},
	}

	converted, _, err := convertBillsToBase(mockBase, inputBills)
	if err != nil {
		t.Fatalf("轉換過程報錯: %v", err)
	}

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
	peopleCount := 100
	billCount := 1000

	var people []Person
	for i := 1; i <= peopleCount; i++ {
		people = append(people, Person{ID: i, Name: fmt.Sprintf("User%d", i)})
	}

	var bills []Bill
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < billCount; i++ {
		amt := rng.Float64() * 1000
		payer := rng.Intn(10) + 1
		var participants []int
		for p := 1; p <= peopleCount; p++ {
			if rng.Intn(2) == 0 {
				participants = append(participants, p)
			}
		}
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculate(people, bills)
	}
}
