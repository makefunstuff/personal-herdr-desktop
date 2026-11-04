package search

import "testing"

func TestMostlyCJK(t *testing.T) {
	if !mostlyCJK("英为财情 Investing.com: 比特币 - Bitcoin 比特币(Bitcoin)是一种数字加密货币") {
		t.Error("a Chinese snippet kept")
	}
	if mostlyCJK("Bitcoin is trading at about 78,000 USD today, CoinDesk says.") {
		t.Error("an English snippet dropped")
	}
}
