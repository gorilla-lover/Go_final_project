package main

import (
	"fmt"
	"math/rand"
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
// 模擬「不均勻」的真實場景，強迫演算法執行最複雜的配對邏輯
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
		// 這樣會製造出大量的債務人，強迫結算邏輯運作到極限
		payer := rng.Intn(10) + 1

		// 場景 C: 隨機參與 (每筆帳單約 50% 人參與，而非全部)
		var participants []int
		for p := 1; p <= peopleCount; p++ {
			if rng.Intn(2) == 0 { // 50% 機率
				participants = append(participants, p)
			}
		}
		// 防呆：如果隨機到沒人參與，強制付款人自己參與
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
