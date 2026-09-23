package services

import "testing"

// rateLimitBurstBytes 决定限速的突发额度：过小会让限速口吃、实际吞吐远低于设定值，
// 过大则失去限速意义，极端速率下还可能溢出。这里把上下限钉住。
func TestRateLimitBurstBytes(t *testing.T) {
	const (
		minBurst = 16000
		maxBurst = 128 * 1024 * 1024
	)

	cases := []struct {
		name     string
		rateMbps int
		want     int
	}{
		{name: "低速时取保底值", rateMbps: 1, want: minBurst},
		{name: "极低速同样保底", rateMbps: 4, want: minBurst},
		{name: "保底与计算值相等处", rateMbps: 5, want: minBurst},
		{name: "常规速率按 25ms 额度计算", rateMbps: 10, want: 32000},
		{name: "百兆", rateMbps: 100, want: 320000},
		{name: "千兆", rateMbps: 1000, want: 3200000},
		{name: "万兆", rateMbps: 10000, want: 32000000},
		{name: "上限被截断", rateMbps: 100000, want: maxBurst},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rateLimitBurstBytes(tc.rateMbps)
			if got != tc.want {
				t.Fatalf("rateLimitBurstBytes(%d) = %d, want %d", tc.rateMbps, got, tc.want)
			}
			if got < minBurst || got > maxBurst {
				t.Fatalf("rateLimitBurstBytes(%d) = %d 超出 [%d, %d] 区间", tc.rateMbps, got, minBurst, maxBurst)
			}
		})
	}
}
