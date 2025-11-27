package main

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"runtime"

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
	People []Person `json:"people"`
	Bills  []Bill   `json:"bills"`
}

// CalculateResponse 計算結果
type CalculateResponse struct {
	Settlements []Settlement `json:"settlements"`
	Error       string       `json:"error,omitempty"`
}

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

		// 執行分帳計算
		settlements := calculate(req.People, req.Bills)

		response := CalculateResponse{Settlements: settlements}
		result, _ := json.Marshal(response)
		return string(result)
	})

	dataURI := "data:text/html;charset=utf-8," + url.PathEscape(indexHTML)
	w.Navigate(dataURI)

	w.Run()
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

		// 每人應付的金額
		perPerson := bill.Amount / float64(len(bill.Participants))

		// 付款者增加餘額（他先付了這筆錢）
		balance[bill.PaidBy] += bill.Amount

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
