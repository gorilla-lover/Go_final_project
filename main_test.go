package main

import (
	"testing"
)

// TestCalculate 使用 Table-Driven Tests 策略來測試分帳邏輯
// 這是 Go 語言社群最推薦的測試寫法，易於擴充與閱讀。
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
				// Bob 欠 Alice 100, Charlie 欠 Alice 100
				// 注意：你的演算法輸出順序可能會變，這裡我們只檢查是否包含正確的轉帳邏輯
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
				// Alice 付 300 (每人應付 100) -> Alice +200
				{ID: 1, AmountBase: 300, PaidBy: 1, Participants: []int{1, 2, 3}},
				// Bob 付 150 (每人應付 50) -> Bob +100
				{ID: 2, AmountBase: 150, PaidBy: 2, Participants: []int{1, 2, 3}},
				// 總結：
				// Alice 淨支付: 300, 應付: 150, 餘額: +150 (應收)
				// Bob   淨支付: 150, 應付: 150, 餘額: 0   (平)
				// Charlie 淨支付: 0, 應付: 150, 餘額: -150 (應付)
			},
			expected: []Settlement{
				{From: "Charlie", To: "Alice", Amount: 150},
			},
		},
		{
			name:     "Case 3: 沒有帳單",
			people:   []Person{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
			bills:    []Bill{},
			expected: nil, // 預期沒有結果
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

			// 驗證內容 (這裡做簡單的包含檢查)
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

// BenchmarkCalculate 效能測試
// 模擬大量數據來測試 calculate 函數的效能
// 執行指令: go test -bench=.
func BenchmarkCalculate(b *testing.B) {
	// 1. 準備測試資料 (100人, 1000筆帳單)
	peopleCount := 100
	billCount := 1000

	var people []Person
	for i := 1; i <= peopleCount; i++ {
		people = append(people, Person{ID: i, Name: "User"})
	}

	var bills []Bill
	for i := 0; i < billCount; i++ {
		// 建立參與者列表 (所有人參與)
		participants := make([]int, peopleCount)
		for j := 0; j < peopleCount; j++ {
			participants[j] = j + 1
		}

		bills = append(bills, Bill{
			ID:           i,
			Title:        "Bench Bill",
			AmountBase:   1000,
			PaidBy:       (i % peopleCount) + 1, // 輪流付款
			Participants: participants,
		})
	}

	b.ResetTimer() // 重置計時器，排除資料準備時間
	for i := 0; i < b.N; i++ {
		calculate(people, bills)
	}
}
